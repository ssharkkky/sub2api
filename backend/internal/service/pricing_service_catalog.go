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
	"github.com/Wei-Shaw/sub2api/internal/modelcatalog/data"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// catalogFileCheckInterval 是显式目录文件（pricing.catalog_file）的热加载
// 轮询间隔。文件只有几 KB，stat 轮询开销可忽略，且无 inotify 依赖，
// 所有卷类型都可用。
const catalogFileCheckInterval = 60 * time.Second

// catalogAutoDiscoverPaths 在 pricing.catalog_file 留空时按顺序探测。
// 官方镜像构建时已把仓库内目录文件 COPY 到 /app/data/catalog.json，
// 因此默认无需额外配置即可命中。该路径同时是远程目录同步的本地缓存位置
// （四级优先级中的「缓存层」，不赢过远程）。
var catalogAutoDiscoverPaths = []string{"./data/catalog.json", "./catalog.json"}

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

func newCatalogRuntime() *catalogRuntime {
	rt := &catalogRuntime{
		source:   "embedded",
		loadedAt: time.Now(),
		hash:     sha256Hex(data.CatalogJSON),
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

// resolveCatalogFilePath 返回要跟踪的目录文件路径。
// 显式配置优先；否则自动发现。找不到文件时：显式配置返回配置路径
// （部署可能稍后才把文件放进容器，watcher 会持续重试），自动发现返回 ""。
func (s *PricingService) resolveCatalogFilePath() string {
	cfgPath := ""
	if s.cfg != nil {
		cfgPath = strings.TrimSpace(s.cfg.Pricing.CatalogFile)
	}
	if cfgPath != "" {
		return cfgPath
	}
	for _, p := range catalogAutoDiscoverPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// explicitCatalogFileLocked 返回「显式配置且当前在磁盘上存在」的目录文件路径。
// 四级优先级里它是最高层（运维意图/air-gap），存在时远程同步不得覆盖它。
func (s *PricingService) explicitCatalogFileLocked() string {
	cfgPath := ""
	if s.cfg != nil {
		cfgPath = strings.TrimSpace(s.cfg.Pricing.CatalogFile)
	}
	if cfgPath == "" {
		return ""
	}
	if _, err := os.Stat(cfgPath); err != nil {
		return ""
	}
	return cfgPath
}

// getCatalogCachePath 返回远程目录同步的本地缓存路径（= 自动发现层）。
func (s *PricingService) getCatalogCachePath() string {
	return filepath.Join(s.cfg.Pricing.DataDir, "catalog.json")
}

// loadRuntimeCatalogFile 在启动时加载本地目录文件（显式配置或自动发现）。
// 文件不存在（未配置）是正常情况；文件不可读/非法时保留内嵌目录，
// 保证一个坏文件永远不会让定价链路带病启动或变空。
func (s *PricingService) loadRuntimeCatalogFile() {
	path := s.resolveCatalogFilePath()
	if path == "" {
		s.setCatalogActive("", "embedded", "")
		return
	}
	if err := s.applyCatalogFile(path); err != nil {
		logger.L().Warn("pricing catalog file not applied, keeping embedded catalog",
			zap.String("path", path), zap.Error(err))
		s.setCatalogActive(path, "embedded", err.Error())
		return
	}
	logger.L().Info("pricing catalog loaded from runtime file",
		zap.String("path", path), zap.Int("models", s.catalogModelCountLocked()))
}

// applyCatalogFile 读取、完整校验并原子换入指定路径的目录文件。
// 只有校验通过并完成换入才返回 nil。
func (s *PricingService) applyCatalogFile(path string) error {
	body, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read catalog file %s: %w", path, err)
	}
	return s.applyCatalogFileContent(path, body)
}

// applyCatalogFileContent 对已读出的目录内容做完整校验并原子换入。
func (s *PricingService) applyCatalogFileContent(path string, body []byte) error {
	cat, err := modelcatalog.Load(body)
	if err != nil {
		return fmt.Errorf("validate catalog file %s: %w", path, err)
	}
	if err := s.swapInCatalog(cat, nil, sha256Hex(body), path, path); err != nil {
		return err
	}
	return nil
}

// syncCatalogRemote 按「哈希锚点 → 变化才下载 → 完整校验 → 原子换入」的
// 节奏同步本仓库 main 分支的目录文件（仓库即权威源）。
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

	// 3) 显式本地文件在场时它赢过远程（运维意图）：只记录分歧，不覆盖。
	if explicit := s.explicitCatalogFileLocked(); explicit != "" {
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

	// 5) 完整校验（版本号/重复 id/price_ref 闭包…），失败保留上一份。
	cat, err := modelcatalog.Load(body)
	if err != nil {
		s.setCatalogError(fmt.Errorf("validate remote catalog: %w", err))
		return fmt.Errorf("validate remote catalog: %w", err)
	}

	if err := s.swapInCatalog(cat, body, bodyHash, "", "remote"); err != nil {
		s.setCatalogError(err)
		return err
	}
	s.setCatalogError(nil)
	logger.L().Info("pricing catalog synced from repo",
		zap.String("url", catalogURL),
		zap.String("cache", s.getCatalogCachePath()),
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
		s.setCatalogError(fmt.Errorf("read catalog file: %w", err))
		return
	}
	if strings.EqualFold(sha256Hex(body), s.catalogHashLocked()) {
		// 文件与生效目录一致 = 健康态：顺手清掉陈旧错误记录（例如文件曾坏、
		// 现已修回当前生效内容）；无错误时是零成本 no-op。
		s.setCatalogError(nil)
		return
	}
	if err := s.applyCatalogFileContent(path, body); err != nil {
		logger.L().Warn("pricing catalog hot reload failed, keeping previous catalog",
			zap.String("path", path), zap.Error(err))
		s.setCatalogError(err)
		return
	}
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

// swapInCatalog 完成一次目录换入（同一事务内）：
//  1. modelcatalog.Replace 原子换入，所有读者透明切换；
//  2. 对当前价表重放 lock_price 覆盖（不原地改动已在共享的条目）；
//  3. 远程来源时把正文原子落盘到本地缓存（tmp+fsync+rename，离线种子）；
//  4. 更新运行态（source/path/hash/时间戳）。
//
// 本地文件来源（显式/自动发现）不重写源文件，避免触碰运维文件。
// 缓存写失败只告警不回滚：内存态优先，缓存只是离线种子。
func (s *PricingService) swapInCatalog(cat *modelcatalog.Catalog, body []byte, bodyHash, path, source string) error {
	modelcatalog.Replace(cat)
	s.replayCatalogLockOverlay()
	if source == "remote" && len(body) > 0 {
		path = s.getCatalogCachePath()
		if err := writeAtomic(path, body, 0644); err != nil {
			logger.L().Warn("pricing catalog cache write failed (in-memory catalog stays active)",
				zap.String("path", path), zap.Error(err))
		}
	}
	s.catalogRuntime.mu.Lock()
	s.catalogRuntime.path = path
	s.catalogRuntime.source = source
	s.catalogRuntime.hash = bodyHash
	s.catalogRuntime.loadedAt = time.Now()
	s.catalogRuntime.lastErr = ""
	s.catalogRuntime.models = catalogModelCount(cat)
	s.catalogRuntime.mu.Unlock()
	return nil
}

// replayCatalogLockOverlay 在目录热换入后，把生效目录的 lock_price 条目
// 重新覆盖到当前价表上。不原地修改已共享给在读请求的 *LiteLLMModelPricing：
// lock 行用克隆覆盖，其余行保持原指针，整个 map 在 s.mu 下原子换引用。
func (s *PricingService) replayCatalogLockOverlay() {
	s.mu.Lock()
	defer s.mu.Unlock()
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

// setCatalogActive 记录一次成功（或启动期失败后回落内嵌）的目录状态。
func (s *PricingService) setCatalogActive(path, source, lastErr string) {
	s.catalogRuntime.mu.Lock()
	s.catalogRuntime.path = path
	s.catalogRuntime.source = source
	s.catalogRuntime.loadedAt = time.Now()
	s.catalogRuntime.lastErr = lastErr
	s.catalogRuntime.models = catalogModelCount(modelcatalog.Current())
	s.catalogRuntime.mu.Unlock()
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
