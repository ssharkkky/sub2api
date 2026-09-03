package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// catalogFileCheckInterval 是显式目录文件（pricing.catalog_file）的热加载
// 轮询间隔。文件只有几 KB，stat 轮询开销可忽略，且无 inotify 依赖，
// 所有卷类型都可用。
const catalogFileCheckInterval = 60 * time.Second


// catalogRuntime 记录渠道模型目录的运行态（面向运维的状态）。
// 真正生效的 *modelcatalog.Catalog 在 modelcatalog 包的原子指针后面，
// 本结构只负责记录「当前目录来自哪里、何时加载、内容指纹、最近一次错误」。
type catalogRuntime struct {
	mu       sync.RWMutex
	path     string // 已解析的目录文件路径；"" 表示只使用内嵌目录
	source   string // "embedded" | "remote" | 生效的本地文件路径
	loadedAt time.Time
	models   int
	lastErr  string
	// hash 是生效目录内容的 sha256（hex）。远程同步用它做「无变化」短路：
	// 仓库 main 没改 → 锚点相等 → 连正文都不下载。
	hash string
	// remoteHash 是最近一次成功取到的远程锚点（catalog_hash_url 内容），
	// 仅用于状态可观测。
	remoteHash string
	// conflictWarnedHash 记录已告警过的「显式文件 vs 远程」分歧组合，去重告警。
	conflictWarnedHash string
}

// 初始态 = 空目录（无模型）+ 未知 hash：目录数据不再编译进二进制，
// 首份目录来自启动期本地加载（镜像种子 ./data/models.json）或首次远程同步。
// hash 为空时远程同步的「锚点相等」短路不成立 → 首轮必然下载一次并校验，
// 成功后写入本地锚点，此后仓库未变即短路（连正文都不下载）。
func newCatalogRuntime() *catalogRuntime {
	rt := &catalogRuntime{
		source:   "none",
		loadedAt: time.Now(),
	}
	rt.models = catalogModelCount(modelcatalog.Current())
	return rt
}

func catalogModelCount(cat *modelcatalog.Catalog) int {
	if cat == nil {
		return 0
	}
	return len(cat.Entries())
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// getCatalogCachePath 返回独立目录文档（pricing.catalog_url 兼容路径）的本地
// 缓存路径。默认配置下目录段来自合并文档（缓存为 models.json），此路径仅在
// 显式配置 catalog_url 时作为其离线种子使用。
func (s *PricingService) getCatalogCachePath() string {
	return filepath.Join(s.cfg.Pricing.DataDir, "catalog.json")
}

// loadRuntimeCatalogFile 在启动时加载显式目录文件（pricing.catalog_file，
// 运维意图/air-gap 场景，四级优先级中的最高层）。未配置时直接返回——
// 本地缓存/镜像种子的加载由 loadLocalModelData 负责。文件稍后才放入容器是
// 合法部署时序：记录 pending 状态，60s 轮询会持续重试；文件损坏/非法时同样
// 记录错误并保留当前目录（远程目录段可在此期间正常接管，见
// catalogSourceIsExplicit），保证坏文件永远不锁死更新、不产生半更新。
func (s *PricingService) loadRuntimeCatalogFile() {
	path := ""
	if s.cfg != nil {
		path = strings.TrimSpace(s.cfg.Pricing.CatalogFile)
	}
	if path == "" {
		return
	}
	body, err := os.ReadFile(path)
	if err != nil {
		logger.L().Warn("pricing explicit catalog file not readable yet (will keep retrying via poll), remote catalog may serve in the meantime",
			zap.String("path", path), zap.Error(err))
		s.setCatalogState(path, "explicit(pending)", err.Error(), "")
		return
	}
	doc, err := decodeModelData(body)
	if err != nil || !doc.hasCatalog {
		errMsg := "explicit catalog_file has no models section"
		if err != nil {
			errMsg = err.Error()
		}
		logger.L().Warn("pricing explicit catalog file not applied, keeping current catalog",
			zap.String("path", path), zap.Error(fmt.Errorf("%s", errMsg)))
		s.setCatalogState(path, "explicit(pending)", errMsg, "")
		return
	}
	if err := s.applyModelData(doc, path, path, nil); err != nil {
		logger.L().Warn("pricing explicit catalog file not applied, keeping current catalog",
			zap.String("path", path), zap.Error(err))
		s.setCatalogState(path, "explicit(pending)", err.Error(), "")
		return
	}
	logger.L().Info("pricing catalog loaded from explicit file",
		zap.String("path", path), zap.Int("models", s.catalogModelCountLocked()))
}

// syncCatalogRemote 按「哈希锚点 → 变化才下载 → 完整校验 → 原子换入」的
// 节奏同步独立的远程目录文档（pricing.catalog_url，兼容路径）。
// 默认配置下目录段来自合并文档（remote_url），此路径不启用。
// 任何一步失败都保留上一份有效目录并返回错误，由调用方记录退避。
func (s *PricingService) syncCatalogRemote() error {
	return s.syncCatalogRemoteCtx(context.Background())
}

// syncCatalogRemoteCtx 同上，但允许调用方通过 parent 限制总预算
// （启动期用较短超时避免拖慢 boot；失败后调度器会按退避重试）。
func (s *PricingService) syncCatalogRemoteCtx(parent context.Context) error {
	if s == nil || s.cfg == nil {
		return nil
	}
	if strings.TrimSpace(s.cfg.Pricing.CatalogURL) == "" {
		return nil
	}
	catalogURL, err := s.validatePricingURL(s.cfg.Pricing.CatalogURL)
	if err != nil {
		return err
	}

	// 1) 远程哈希锚点（可缺省；缺省时靠正文哈希兜底比较）。
	var remoteHash string
	if strings.TrimSpace(s.cfg.Pricing.CatalogHashURL) != "" {
		remoteHash, err = s.fetchCatalogRemoteHash(parent)
		if err != nil {
			return fmt.Errorf("fetch catalog remote hash: %w", err)
		}
	}
	s.setCatalogRemoteHash(remoteHash)

	activeHash := s.catalogHashLocked()

	// 2) 锚点与生效内容一致 → 无变化，连正文都不下载。
	if remoteHash != "" && activeHash != "" && strings.EqualFold(remoteHash, activeHash) {
		return nil
	}

	// 3) 显式本地文件正在生效时它赢过远程（运维意图）：只记录分歧，不覆盖。
	//    判定是「目录确实来自该文件」（source-based）：文件损坏/未放入不锁死远程。
	if s.catalogSourceIsExplicit() {
		if remoteHash != "" && activeHash != "" && !strings.EqualFold(remoteHash, activeHash) {
			s.warnCatalogConflict(remoteHash, activeHash)
		}
		return nil
	}

	// 4) 下载正文并做二次指纹比较（覆盖哈希文件缺失/CDN 缓存延迟）。
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	body, err := s.remoteClient.FetchPricingJSON(ctx, catalogURL)
	if err != nil {
		return fmt.Errorf("download catalog: %w", err)
	}
	bodyHash := sha256Hex(body)
	if remoteHash != "" && !strings.EqualFold(remoteHash, bodyHash) {
		logger.L().Warn("pricing catalog hash mismatch: remote anchor differs from body hash (hash file may be out of sync), validating body",
			zap.String("remote", remoteHash[:min(8, len(remoteHash))]),
			zap.String("body", bodyHash[:8]))
	}
	if activeHash != "" && strings.EqualFold(bodyHash, activeHash) {
		return nil
	}

	// 5) 形态识别 + 完整校验（目录/价表段都必须通过），失败保留上一份。
	doc, err := decodeModelData(body)
	if err != nil {
		s.setCatalogError(err)
		return err
	}
	if err := s.applyModelData(doc, "remote", "", body); err != nil {
		s.setCatalogError(err)
		return err
	}
	s.setCatalogError(nil)
	logger.L().Info("pricing catalog synced from remote",
		zap.String("url", catalogURL),
		zap.String("shape", doc.shape.String()),
		zap.Int("models", s.catalogModelCountLocked()))
	return nil
}

// pollCatalogFile 执行一次显式目录文件的变更检测 + 热加载。
// 变更指纹 = 生效目录内容的 sha256（所有来源同一口径，swapInCatalog 维护）：
// 文件未变 → 跳过；变了 → 校验 + 热换入。相比 mtime/size 比对，
// 不受文件系统时间粒度（FAT 2s）与「读文件/stat」交错的影响。
// 文件只有几 KB，每 60s 读一次 + 哈希开销可忽略。
// 文件消失或内容非法时保留上一份有效目录，下一轮继续重试。
// 由统一调度器每 60s 调用一次（见 startUpdateScheduler）。
func (s *PricingService) pollCatalogFile(path string) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// 运维移除了显式文件：解除「本地赢」锁（source 回退），远程目录段
			// 在下一轮同步可重新接管；当前目录保持 last-good。
			if s.catalogSourceIsExplicit() {
				s.setCatalogState("", "released", "", "")
			}
			return
		}
		s.setCatalogError(fmt.Errorf("read catalog file: %w", err))
		return
	}
	if strings.EqualFold(sha256Hex(body), s.catalogHashLocked()) {
		// 文件与生效目录一致 = 健康态：顺手清掉陈旧错误记录（例如文件曾坏、
		// 现已修回当前生效内容）；无错误时是零成本 no-op。
		s.setCatalogError(nil)
		return
	}
	doc, err := decodeModelData(body)
	if err != nil || !doc.hasCatalog {
		msg := "explicit catalog_file has no models section"
		if err != nil {
			msg = err.Error()
		}
		s.setCatalogError(fmt.Errorf("%s", msg))
		return
	}
	if err := s.applyModelData(doc, path, path, nil); err != nil {
		logger.L().Warn("pricing catalog hot reload failed, keeping previous catalog",
			zap.String("path", path), zap.Error(err))
		s.setCatalogError(err)
		return
	}
	s.setCatalogError(nil)
	logger.L().Info("pricing catalog hot reload applied",
		zap.String("path", path), zap.Int("models", s.catalogModelCountLocked()))
}

// fetchCatalogRemoteHash 拉取目录文件的远程哈希锚点（纯 hex 单行），
// parent 是调用方的总预算（其上再叠 10s 拉取超时）。
func (s *PricingService) fetchCatalogRemoteHash(parent context.Context) (string, error) {
	hashURL, err := s.validatePricingURL(s.cfg.Pricing.CatalogHashURL)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	hash, err := s.remoteClient.FetchHashText(ctx, hashURL)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(hash), nil
}

// replayCatalogLockOverlay 在目录热换入后，把生效目录的 lock_price 条目
// 重新覆盖到当前价表上（自持锁，供临界区外调用）。
func (s *PricingService) replayCatalogLockOverlay() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.replayCatalogLockOverlayLocked()
}

// replayCatalogLockOverlayLocked 同上，但要求调用方已持有 s.mu。
// 不原地修改已共享给在读请求的 *LiteLLMModelPricing：
// lock 行用克隆覆盖，其余行保持原指针，整个 map 在 s.mu 下原子换引用。
func (s *PricingService) replayCatalogLockOverlayLocked() {
	if s.pricingData == nil {
		s.pricingData = map[string]*LiteLLMModelPricing{}
	}
	next := make(map[string]*LiteLLMModelPricing, len(s.pricingData)+2)
	for k, v := range s.pricingData {
		next[k] = v
	}
	changed := false
	for _, entry := range modelcatalog.Current().Entries() {
		if entry == nil || entry.Price == nil || !entry.IsCanonical() || !entry.LockPrice {
			continue
		}
		existing, ok := next[entry.ID]
		if !ok {
			next[entry.ID] = tokenRatesToLiteLLMPricing(entry.Rates())
			changed = true
			continue
		}
		clone := *existing
		overlayLiteLLMFromCatalog(&clone, entry.Price)
		next[entry.ID] = &clone
		changed = true
	}
	if changed {
		s.pricingData = next
	}
}

// warnCatalogConflict 对「显式文件生效但远程已变化」做一次性告警：
// 本地赢，但要让运维知道仓库 main 上的目录与容器内显式文件已经分叉。
func (s *PricingService) warnCatalogConflict(remoteHash, localHash string) {
	s.catalogRuntime.mu.Lock()
	key := remoteHash + "|" + localHash
	if s.catalogRuntime.conflictWarnedHash == key {
		s.catalogRuntime.mu.Unlock()
		return
	}
	s.catalogRuntime.conflictWarnedHash = key
	s.catalogRuntime.mu.Unlock()
	logger.L().Warn("pricing catalog: explicit catalog_file in effect while repo catalog changed (local wins)",
		zap.String("remote_hash", remoteHash[:min(8, len(remoteHash))]),
		zap.String("local_hash", localHash[:8]))
}

func (s *PricingService) catalogHashLocked() string {
	s.catalogRuntime.mu.RLock()
	defer s.catalogRuntime.mu.RUnlock()
	return s.catalogRuntime.hash
}

func (s *PricingService) setCatalogRemoteHash(h string) {
	if h == "" {
		return
	}
	s.catalogRuntime.mu.Lock()
	s.catalogRuntime.remoteHash = h
	s.catalogRuntime.mu.Unlock()
}

func (s *PricingService) catalogPathLocked() string {
	s.catalogRuntime.mu.RLock()
	defer s.catalogRuntime.mu.RUnlock()
	return s.catalogRuntime.path
}

func (s *PricingService) catalogModelCountLocked() int {
	s.catalogRuntime.mu.RLock()
	defer s.catalogRuntime.mu.RUnlock()
	return s.catalogRuntime.models
}


// setCatalogError 记录目录问题；相同错误只告警一次，避免刷屏。
// 传 nil 表示恢复正常。
func (s *PricingService) setCatalogError(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	s.catalogRuntime.mu.Lock()
	if s.catalogRuntime.lastErr == msg {
		s.catalogRuntime.mu.Unlock()
		return
	}
	s.catalogRuntime.lastErr = msg
	s.catalogRuntime.mu.Unlock()
	if err != nil {
		logger.L().Warn("pricing catalog problem", zap.Error(err))
	}
}

// catalogStatus 返回 GetStatus 使用的目录运行态快照。
func (s *PricingService) catalogStatus() map[string]any {
	s.catalogRuntime.mu.RLock()
	defer s.catalogRuntime.mu.RUnlock()
	out := map[string]any{
		"source":    s.catalogRuntime.source,
		"models":    s.catalogRuntime.models,
		"loaded_at": s.catalogRuntime.loadedAt,
		"hash":      s.catalogRuntime.hash,
	}
	if s.cfg != nil && strings.TrimSpace(s.cfg.Pricing.CatalogURL) != "" {
		out["remote_enabled"] = true
		out["remote_hash"] = s.catalogRuntime.remoteHash
	}
	if s.catalogRuntime.path != "" {
		out["path"] = s.catalogRuntime.path
	}
	if s.catalogRuntime.lastErr != "" {
		out["last_error"] = s.catalogRuntime.lastErr
	}
	return out
}

// remoteBackoff 对单个远程同步目标做指数退避：10s, 20s, 40s … 上限 10min。
// 持续失败（如 GitHub 429）时避免每个 tick 都去敲 GitHub。
type remoteBackoff struct {
	mu          sync.Mutex
	failures    int
	nextAttempt time.Time
}

func (b *remoteBackoff) ready(now time.Time) bool {
	if b == nil {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return now.After(b.nextAttempt)
}

func (b *remoteBackoff) recordFailure(now time.Time) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	d := 10 * time.Second
	for i := 1; i < b.failures && d < 10*time.Minute; i++ {
		d *= 2
	}
	if d > 10*time.Minute {
		d = 10 * time.Minute
	}
	b.nextAttempt = now.Add(d)
}

func (b *remoteBackoff) recordSuccess() {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.failures = 0
	b.nextAttempt = time.Time{}
	b.mu.Unlock()
}
