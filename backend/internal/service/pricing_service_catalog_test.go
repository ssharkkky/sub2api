package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
	"github.com/stretchr/testify/require"
)

const runtimeCatalogWithTestModel = `{
  "version": 1,
  "models": [
    {"id": "catalog-file-model", "platforms": ["openai"], "kind": "chat", "billing_mode": "token",
     "price": {"input_per_mtok": 1, "output_per_mtok": 2}}
  ]
}`

const runtimeCatalogWithOtherModel = `{
  "version": 1,
  "models": [
    {"id": "catalog-file-model-v2", "platforms": ["openai"], "kind": "chat", "billing_mode": "token",
     "price": {"input_per_mtok": 3, "output_per_mtok": 4}}
  ]
}`

func writeCatalogFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// restoreEmbeddedCatalog 保证测试替换全局目录后恢复播种目录，
// 避免影响同包其它依赖仓库目录的测试。
func restoreEmbeddedCatalog(t *testing.T) {
	t.Helper()
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })
}

// 注意：目录数据不再编译进二进制。测试包 TestMain 已用仓库合并文档
// （deploy/data/models.json）播种全局目录（含 gemini-3.6-flash 等真实模型），
// 下面的「当前目录」均指这份播种目录。

func TestLoadRuntimeCatalogFileExplicitWins(t *testing.T) {
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })

	dir := t.TempDir()
	explicit := writeCatalogFile(t, dir, "custom.json", runtimeCatalogWithTestModel)

	svc := NewPricingService(&config.Config{}, nil)
	svc.cfg.Pricing.CatalogFile = explicit
	svc.loadRuntimeCatalogFile()

	require.NotNil(t, modelcatalog.Lookup("catalog-file-model"), "显式文件里的模型必须可见")
	require.Nil(t, modelcatalog.Lookup("gemini-3.6-flash"), "文件整体替换当前目录，而不是合并")

	status := svc.catalogStatus()
	require.Equal(t, explicit, status["path"])
	require.Equal(t, explicit, status["source"], "source 必须是显式路径（本地赢的判定依据）")
	require.NotEmpty(t, status["models"])
}

func TestLoadRuntimeCatalogFileExplicitMissingPending(t *testing.T) {
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })

	dir := t.TempDir()
	missing := filepath.Join(dir, "not-deployed-yet.json")
	svc := NewPricingService(&config.Config{}, nil)
	svc.cfg.Pricing.CatalogFile = missing
	svc.loadRuntimeCatalogFile()

	// 显式配置但文件尚未就位（部署稍后放进容器）：记录 pending 状态，
	// 60s 轮询会持续重试；当前目录（播种目录）保持可用。
	require.NotNil(t, modelcatalog.Lookup("gemini-3.6-flash"), "文件未就位时当前目录必须保持")
	status := svc.catalogStatus()
	require.Equal(t, "explicit(pending)", status["source"])
	require.NotEmpty(t, status["last_error"])
}

func TestLoadRuntimeCatalogFileInvalidKeepsCurrent(t *testing.T) {
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })

	dir := t.TempDir()
	bad := writeCatalogFile(t, dir, "custom.json", `{"version": 77, "models": []}`)
	svc := NewPricingService(&config.Config{}, nil)
	svc.cfg.Pricing.CatalogFile = bad
	svc.loadRuntimeCatalogFile()

	require.NotNil(t, modelcatalog.Lookup("gemini-3.6-flash"), "坏文件必须保留当前目录")
	require.Nil(t, modelcatalog.Lookup("catalog-file-model"))
	status := svc.catalogStatus()
	require.Equal(t, "explicit(pending)", status["source"])
	require.NotEmpty(t, status["last_error"])
}

func TestLoadRuntimeCatalogFileNotConfiguredNoop(t *testing.T) {
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })

	svc := NewPricingService(&config.Config{}, nil)
	svc.loadRuntimeCatalogFile()

	// 未配置显式文件：直接 no-op（本地缓存/镜像种子由 loadLocalModelData 负责）。
	require.NotNil(t, modelcatalog.Lookup("gemini-3.6-flash"), "当前目录不得被触碰")
	status := svc.catalogStatus()
	require.Equal(t, "none", status["source"])
	require.Empty(t, status["last_error"])
}

func TestCatalogPollHotReloadAndInvalidRetry(t *testing.T) {
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })

	dir := t.TempDir()
	path := writeCatalogFile(t, dir, "custom.json", runtimeCatalogWithTestModel)
	svc := NewPricingService(&config.Config{}, nil)
	svc.cfg.Pricing.CatalogFile = path
	svc.loadRuntimeCatalogFile()
	require.Equal(t, path, svc.catalogPathLocked())
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model"))

	// 未变化（内容哈希 = 生效目录哈希）：poll 必须短路，不重复换入。
	svc.pollCatalogFile(path)
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model"))
	require.Empty(t, svc.catalogStatus()["last_error"])

	// 文件更新：换入新目录，旧模型消失
	require.NoError(t, os.WriteFile(path, []byte(runtimeCatalogWithOtherModel), 0o644))
	svc.pollCatalogFile(path)
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model-v2"))
	require.Nil(t, modelcatalog.Lookup("catalog-file-model"))
	require.Equal(t, path, svc.catalogStatus()["source"])

	// 坏版本：保留上一份有效目录，不崩溃不清空
	require.NoError(t, os.WriteFile(path, []byte(`{"version": 2, "models": []}`), 0o644))
	svc.pollCatalogFile(path)
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model-v2"), "坏版本不得覆盖上一份有效目录")
	require.NotEmpty(t, svc.catalogStatus()["last_error"])

	// 修复后自动恢复
	require.NoError(t, os.WriteFile(path, []byte(runtimeCatalogWithOtherModel), 0o644))
	svc.pollCatalogFile(path)
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model-v2"))
	require.Empty(t, svc.catalogStatus()["last_error"])
}

func TestCatalogPollFileMissingReleasesExplicitLock(t *testing.T) {
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })

	dir := t.TempDir()
	path := writeCatalogFile(t, dir, "custom.json", runtimeCatalogWithTestModel)
	svc := NewPricingService(&config.Config{}, nil)
	svc.cfg.Pricing.CatalogFile = path
	svc.loadRuntimeCatalogFile()
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model"))
	require.Equal(t, path, svc.catalogStatus()["source"])

	// 运维移除显式文件：保留 last-good，并解除「本地赢」锁，
	// 让远程目录段在下一轮同步可以重新接管（source 回退，不算错误）。
	require.NoError(t, os.Remove(path))
	svc.pollCatalogFile(path)
	require.NotNil(t, modelcatalog.Lookup("catalog-file-model"), "文件消失时保留上一份有效目录")
	status := svc.catalogStatus()
	require.Equal(t, "released", status["source"])
	require.Empty(t, status["last_error"], "移除显式文件是运维动作，不是错误")
}

func TestCatalogExplicitYieldsToRemoteMergedDoc(t *testing.T) {
	orig := modelcatalog.Current()
	t.Cleanup(func() { modelcatalog.Replace(orig) })

	dir := t.TempDir()
	path := writeCatalogFile(t, dir, "custom.json", runtimeCatalogWithTestModel)
	svc := NewPricingService(&config.Config{Pricing: config.PricingConfig{DataDir: dir}}, nil)
	svc.cfg.Pricing.CatalogFile = path
	svc.loadRuntimeCatalogFile()
	require.Equal(t, path, svc.catalogStatus()["source"])

	// 远程合并文档到达（目录段 + 价表段）：显式文件正在生效 → 目录段让位，
	// 价表段照常生效。
	merged := `{
	  "version": 1,
	  "models": [{"id": "remote-doc-model", "platforms": ["openai"], "kind": "chat", "billing_mode": "token"}],
	  "prices": {"remote-doc-model": {"mode": "chat", "input_cost_per_token": 1e-06}}
	}`
	doc, err := decodeModelData([]byte(merged))
	require.NoError(t, err)
	require.NoError(t, svc.applyModelData(doc, "remote", "", []byte(merged)))

	require.NotNil(t, modelcatalog.Lookup("catalog-file-model"), "显式文件的目录必须保留（本地赢）")
	require.Nil(t, modelcatalog.Lookup("remote-doc-model"), "远程目录段不得覆盖显式目录")
	require.Equal(t, path, svc.catalogStatus()["source"])
	require.NotNil(t, svc.getModelPricingLocked("remote-doc-model"), "价表段照常生效")
}

func (s *PricingService) getModelPricingLocked(name string) *LiteLLMModelPricing {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pricingData[name]
}

func TestPricingServiceGetStatusIncludesCatalog(t *testing.T) {
	svc := NewPricingService(&config.Config{}, nil)
	svc.loadRuntimeCatalogFile()

	status := svc.GetStatus()
	catalog, ok := status["catalog"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "none", catalog["source"], "未配置显式文件且未加载任何数据 = none")
	require.NotEmpty(t, catalog["models"], "播种目录的模型数必须可见")
}
