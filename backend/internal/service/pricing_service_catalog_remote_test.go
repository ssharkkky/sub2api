package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
	"github.com/stretchr/testify/require"
)

const (
	remoteTestCatalogURL    = "https://raw.example.invalid/repo/catalog.json"
	remoteTestCatalogHashUR = "https://raw.example.invalid/repo/catalog.sha256"
)

// fakeCatalogRemote 实现 PricingRemoteClient，只为目录同步提供可编排的响应。
type fakeCatalogRemote struct {
	catalogBody   []byte
	catalogHash   string
	failBody      error
	failHash      error
	bodyFetches   int
	hashFetches   int
	lastBodyURL   string
	lastHashURL   string
}

func (f *fakeCatalogRemote) FetchPricingJSON(ctx context.Context, url string) ([]byte, error) {
	f.bodyFetches++
	f.lastBodyURL = url
	if f.failBody != nil {
		return nil, f.failBody
	}
	return f.catalogBody, nil
}

func (f *fakeCatalogRemote) FetchHashText(ctx context.Context, url string) (string, error) {
	f.hashFetches++
	f.lastHashURL = url
	if f.failHash != nil {
		return "", f.failHash
	}
	return f.catalogHash, nil
}

func newCatalogSyncTestService(t *testing.T, remote PricingRemoteClient, cfg *config.Config) *PricingService {
	t.Helper()
	if cfg == nil {
		cfg = &config.Config{}
	}
	if cfg.Pricing.DataDir == "" {
		cfg.Pricing.DataDir = t.TempDir()
	}
	if cfg.Pricing.CatalogURL == "" {
		cfg.Pricing.CatalogURL = remoteTestCatalogURL
	}
	if cfg.Pricing.CatalogHashURL == "" {
		cfg.Pricing.CatalogHashURL = remoteTestCatalogHashUR
	}
	svc := NewPricingService(cfg, remote)
	return svc
}

func loadTestCatalog(t *testing.T, body string) *modelcatalog.Catalog {
	t.Helper()
	cat, err := modelcatalog.Load([]byte(body))
	require.NoError(t, err)
	return cat
}

func TestSyncCatalogRemoteNoopWhenAnchorMatchesActive(t *testing.T) {
	restoreEmbeddedCatalog(t)
	body := []byte(runtimeCatalogWithTestModel)
	sum := sha256.Sum256(body)
	remote := &fakeCatalogRemote{catalogBody: body, catalogHash: hex.EncodeToString(sum[:])}

	svc := newCatalogSyncTestService(t, remote, nil)
	// 启动期已把远程内容换入（模拟首次部署完成）
	require.NoError(t, svc.syncCatalogRemote())
	svc2Fetches := remote.bodyFetches

	// 锚点与生效内容一致：无变化，连正文都不下载
	require.NoError(t, svc.syncCatalogRemote())
	require.Equal(t, svc2Fetches, remote.bodyFetches, "anchor match must short-circuit before download")
	require.Equal(t, hex.EncodeToString(sum[:]), svc.catalogHashLocked())
}

func TestSyncCatalogRemoteSwapsWhenAnchorDiffers(t *testing.T) {
	restoreEmbeddedCatalog(t)
	remote := &fakeCatalogRemote{
		catalogBody:   []byte(runtimeCatalogWithTestModel),
		catalogHash:   "deadbeef", // 与内嵌目录 hash 必然不同
	}
	svc := newCatalogSyncTestService(t, remote, nil)

	require.NoError(t, svc.syncCatalogRemote())

	cur := modelcatalog.Current()
	require.NotNil(t, cur.Lookup("catalog-file-model"), "remote catalog must be swapped in")
	require.Equal(t, "remote", svc.catalogRuntime.source)
	require.FileExists(t, filepath.Join(svc.cfg.Pricing.DataDir, "catalog.json"), "remote body must be cached atomically")
	cached, err := os.ReadFile(filepath.Join(svc.cfg.Pricing.DataDir, "catalog.json"))
	require.NoError(t, err)
	require.Equal(t, remote.catalogBody, cached)

	status := svc.GetStatus()
	catalog := status["catalog"].(map[string]any)
	require.True(t, catalog["remote_enabled"].(bool))
	require.Equal(t, "deadbeef", catalog["remote_hash"])
	require.NotEmpty(t, catalog["hash"])
}

func TestSyncCatalogRemoteInvalidBodyKeepsLastGood(t *testing.T) {
	restoreEmbeddedCatalog(t)
	before := modelcatalog.Current()
	remote := &fakeCatalogRemote{
		catalogBody:   []byte(`{"version": 99, "models": []}`), // 非法版本
		catalogHash:   "deadbeef",
	}
	svc := newCatalogSyncTestService(t, remote, nil)

	err := svc.syncCatalogRemote()
	require.Error(t, err)
	require.Same(t, before, modelcatalog.Current(), "invalid remote body must keep the previous catalog")
	require.Equal(t, "none", svc.catalogRuntime.source, "无内嵌基线：失败时保持初始空目录状态")
}

func TestSyncCatalogRemoteDownloadFailureKeepsLastGood(t *testing.T) {
	restoreEmbeddedCatalog(t)
	before := modelcatalog.Current()
	remote := &fakeCatalogRemote{
		catalogBody:   []byte(runtimeCatalogWithTestModel),
		catalogHash:   "deadbeef",
		failBody:      errors.New("github 429"),
	}
	svc := newCatalogSyncTestService(t, remote, nil)

	require.Error(t, svc.syncCatalogRemote())
	require.Same(t, before, modelcatalog.Current())
}

func TestSyncCatalogRemoteExplicitFileWinsOverRemote(t *testing.T) {
	restoreEmbeddedCatalog(t)
	dir := t.TempDir()
	explicit := writeCatalogFile(t, dir, "explicit.json", runtimeCatalogWithOtherModel)

	remote := &fakeCatalogRemote{
		catalogBody:   []byte(runtimeCatalogWithTestModel),
		catalogHash:   "cafe0001", // 与显式文件 hash 不同
	}
	cfg := &config.Config{Pricing: config.PricingConfig{
		DataDir:     dir,
		CatalogFile: explicit,
	}}
	svc := newCatalogSyncTestService(t, remote, cfg)
	svc.loadRuntimeCatalogFile()
	require.NotNil(t, modelcatalog.Current().Lookup("catalog-file-model-v2"), "explicit file must be active first")

	err := svc.syncCatalogRemote()
	require.NoError(t, err, "remote divergence with explicit file in effect is a warning, not an error")
	require.NotNil(t, modelcatalog.Current().Lookup("catalog-file-model-v2"), "explicit catalog_file must win over remote")
	require.Nil(t, modelcatalog.Current().Lookup("catalog-file-model"))

	catalog := svc.GetStatus()["catalog"].(map[string]any)
	require.Equal(t, "cafe0001", catalog["remote_hash"], "remote anchor must stay observable even when local wins")
}

func TestSyncCatalogRemoteNoHashURLUsesBodyHash(t *testing.T) {
	restoreEmbeddedCatalog(t)
	body := []byte(runtimeCatalogWithTestModel)
	remote := &fakeCatalogRemote{catalogBody: body}
	cfg := &config.Config{Pricing: config.PricingConfig{
		DataDir:      t.TempDir(),
		CatalogURL:   remoteTestCatalogURL,
		CatalogHashURL: "", // 锚点缺失
	}}
	svc := newCatalogSyncTestService(t, remote, cfg)

	// 首次：正文 hash 与内嵌不同 → 换入
	require.NoError(t, svc.syncCatalogRemote())
	firstHash := svc.catalogHashLocked()
	require.NotEmpty(t, firstHash)
	fetchesAfterFirst := remote.bodyFetches

	// 再同步：正文 hash 与生效内容一致 → 短路（仍下载一次正文做比较，但不再换入）
	require.NoError(t, svc.syncCatalogRemote())
	require.Greater(t, remote.bodyFetches, fetchesAfterFirst, "without hash anchor the body must be fetched for comparison")
	require.Equal(t, firstHash, svc.catalogHashLocked(), "identical body must not re-swap")
}

func TestSyncCatalogRemoteHashFetchFailure(t *testing.T) {
	restoreEmbeddedCatalog(t)
	before := modelcatalog.Current()
	remote := &fakeCatalogRemote{
		catalogBody: []byte(runtimeCatalogWithTestModel),
		catalogHash: "deadbeef",
		failHash:    errors.New("dns"),
	}
	svc := newCatalogSyncTestService(t, remote, nil)

	require.Error(t, svc.syncCatalogRemote())
	require.Same(t, before, modelcatalog.Current())
	require.Zero(t, remote.bodyFetches, "hash fetch failure must not trigger a download")
}

func TestRemoteBackoffSchedule(t *testing.T) {
	var b remoteBackoff
	now := time.Now()
	require.True(t, b.ready(now))

	b.recordFailure(now)
	require.False(t, b.ready(now.Add(9*time.Second)))
	require.True(t, b.ready(now.Add(11*time.Second)))

	// 连续失败指数翻倍：10s → 20s
	b.recordFailure(now.Add(20 * time.Minute))
	require.False(t, b.ready(now.Add(20*time.Minute + 19*time.Second)))
	require.True(t, b.ready(now.Add(20*time.Minute + 21*time.Second)))

	b.recordSuccess()
	require.True(t, b.ready(time.Now()), "success must clear the backoff")
}

func TestReplayCatalogLockOverlayClonesSharedEntries(t *testing.T) {
	// 内嵌目录自带 gemini-3.6-flash 的 lock_price 卡（$1.50/$7.50/$0.15）。
	// 价表里该模型是旧值：重放后必须是覆盖过的克隆，旧指针保持原值。
	old := &LiteLLMModelPricing{
		InputCostPerToken:  9e-6,
		OutputCostPerToken: 9e-6,
		Mode:               "chat",
	}
	svc := NewPricingService(&config.Config{}, nil)
	svc.pricingData = map[string]*LiteLLMModelPricing{"gemini-3.6-flash": old}

	svc.replayCatalogLockOverlay()

	got := svc.pricingData["gemini-3.6-flash"]
	require.NotSame(t, old, got, "lock replay must not mutate the shared entry in place")
	require.InDelta(t, 1.5e-6, got.InputCostPerToken, 1e-15)
	require.InDelta(t, 7.5e-6, got.OutputCostPerToken, 1e-15)
	require.InDelta(t, 0.15e-6, got.CacheReadInputTokenCost, 1e-15)
	require.InDelta(t, 9e-6, old.InputCostPerToken, 1e-15, "previous readers keep the old value")
}

func TestSyncCatalogRemoteUsesConfiguredURLs(t *testing.T) {
	restoreEmbeddedCatalog(t)
	remote := &fakeCatalogRemote{catalogBody: []byte(runtimeCatalogWithTestModel), catalogHash: "deadbeef"}
	svc := newCatalogSyncTestService(t, remote, nil)
	require.NoError(t, svc.syncCatalogRemote())
	require.Equal(t, remoteTestCatalogURL, remote.lastBodyURL)
	require.Equal(t, remoteTestCatalogHashUR, remote.lastHashURL)
}

// 价表远程同步与目录侧同构的回归测试。
const testPriceTableBody = `{"model-a": {"input_cost_per_token": 1e-6, "output_cost_per_token": 2e-6}}`

func pricingLocalHash(t *testing.T, svc *PricingService) string {
	t.Helper()
	svc.mu.RLock()
	defer svc.mu.RUnlock()
	return svc.localHash
}

// P1 回归：价表同步锚点 = 实际加载正文的哈希（不是远程锚点原样入库）。
// 锚点文件损坏（非 hex 垃圾）时只会每轮冗余下载 + 告警，不会把更新永久冻结；
// 锚点修复后恢复短路。
func TestSyncWithRemoteBadAnchorDoesNotFreezeUpdates(t *testing.T) {
	body := []byte(testPriceTableBody)
	bodyHash := sha256Hex(body)
	remote := &fakeCatalogRemote{catalogBody: body, catalogHash: "not-a-valid-hex-!!"}
	svc := newCatalogSyncTestService(t, remote, &config.Config{Pricing: config.PricingConfig{
		RemoteURL: "https://invalid.example/prices.json",
		HashURL:   "https://invalid.example/prices.sha256",
	}})

	// 首轮：localHash 为空 → 下载；本地锚点存正文哈希（而非垃圾锚点）。
	require.NoError(t, svc.syncWithRemote(context.Background()))
	require.Equal(t, 1, remote.bodyFetches)
	require.Equal(t, bodyHash, pricingLocalHash(t, svc), "锚点必须与实际加载的正文一致")

	// 次轮：垃圾锚点 ≠ 正文哈希 → 继续下载（不冻结）。
	require.NoError(t, svc.syncWithRemote(context.Background()))
	require.Equal(t, 2, remote.bodyFetches, "坏锚点不得把更新永久冻结")

	// 锚点修复后：锚点 == 正文哈希 → 短路，不再下载。
	remote.catalogHash = bodyHash
	require.NoError(t, svc.syncWithRemote(context.Background()))
	require.Equal(t, 2, remote.bodyFetches)
}

// P1 回归：哈希拉取失败必须返回错误（调度器据此计入指数退避，与目录侧一致），
// 而不是吞掉记为成功。
func TestSyncWithRemoteHashFetchFailureReturnsError(t *testing.T) {
	remote := &fakeCatalogRemote{
		catalogBody: []byte(testPriceTableBody),
		failHash:    errors.New("hash endpoint down"),
	}
	svc := newCatalogSyncTestService(t, remote, &config.Config{Pricing: config.PricingConfig{
		RemoteURL: "https://invalid.example/prices.json",
		HashURL:   "https://invalid.example/prices.sha256",
	}})

	require.Error(t, svc.syncWithRemote(context.Background()))
}
