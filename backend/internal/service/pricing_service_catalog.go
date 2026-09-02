package service

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// catalogFileCheckInterval 是渠道模型目录文件的热加载轮询间隔。
// 文件只有几 KB，stat 轮询开销可忽略，且无 inotify 依赖，所有卷类型都可用。
const catalogFileCheckInterval = 60 * time.Second

// catalogAutoDiscoverPaths 在 pricing.catalog_file 留空时按顺序探测。
// 官方镜像构建时已把仓库内目录文件 COPY 到 /app/data/catalog.json，
// 因此默认无需额外配置即可命中。
var catalogAutoDiscoverPaths = []string{"./data/catalog.json", "./catalog.json"}

// catalogRuntime 记录渠道模型目录的运行态（面向运维的状态）。
// 真正生效的 *modelcatalog.Catalog 在 modelcatalog 包的原子指针后面，
// 本结构只负责记录「当前目录来自哪里、何时加载、最近一次错误」。
type catalogRuntime struct {
	mu       sync.RWMutex
	path     string // 已解析的目录文件路径；"" 表示只使用内嵌目录
	source   string // "embedded" 或生效的文件路径
	loadedAt time.Time
	models   int
	lastErr  string
	// 上次成功加载文件的指纹，用于热加载变更检测。
	lastMod  time.Time
	lastSize int64
}

func newCatalogRuntime() *catalogRuntime {
	rt := &catalogRuntime{source: "embedded", loadedAt: time.Now()}
	rt.models = catalogModelCount(modelcatalog.Current())
	return rt
}

func catalogModelCount(cat *modelcatalog.Catalog) int {
	if cat == nil {
		return 0
	}
	return len(cat.Entries())
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
		if _, err := os.Stat(cfgPath); err == nil {
			return cfgPath
		}
		return cfgPath
	}
	for _, p := range catalogAutoDiscoverPaths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// loadRuntimeCatalogFile 在启动时加载运维目录文件。
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
	cat, err := modelcatalog.LoadFile(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("stat catalog file %s: %w", path, err)
	}
	modelcatalog.Replace(cat)
	s.catalogRuntime.mu.Lock()
	s.catalogRuntime.lastMod = info.ModTime()
	s.catalogRuntime.lastSize = info.Size()
	s.catalogRuntime.mu.Unlock()
	s.setCatalogActive(path, path, "")
	return nil
}

// startCatalogFileWatcher 轮询目录文件并在磁盘内容变化时热换入新目录。
// 生命周期与定价更新调度器一致：每进程启动一次，Stop 时随 stopCh 退出。
func (s *PricingService) startCatalogFileWatcher() {
	path := s.catalogPathLocked()
	if path == "" {
		return
	}
	s.lifecycleMu.Lock()
	if s.catalogWatcherStarted || s.stopped {
		s.lifecycleMu.Unlock()
		return
	}
	s.catalogWatcherStarted = true
	s.lifecycleMu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ticker := time.NewTicker(catalogFileCheckInterval)
		defer ticker.Stop()
		for {
			select {
			case <-s.stopCh:
				return
			case <-ticker.C:
				s.pollCatalogFile(path)
			}
		}
	}()
}

// pollCatalogFile 执行一次变更检测 + 热加载。
// 文件消失或内容非法时保留上一份有效目录，下一轮继续重试。
func (s *PricingService) pollCatalogFile(path string) {
	info, err := os.Stat(path)
	if err != nil {
		s.setCatalogError(fmt.Errorf("stat catalog file: %w", err))
		return
	}
	s.catalogRuntime.mu.RLock()
	unchanged := info.ModTime().Equal(s.catalogRuntime.lastMod) && info.Size() == s.catalogRuntime.lastSize
	s.catalogRuntime.mu.RUnlock()
	if unchanged {
		return
	}
	if err := s.applyCatalogFile(path); err != nil {
		logger.L().Warn("pricing catalog hot reload failed, keeping previous catalog",
			zap.String("path", path), zap.Error(err))
		s.setCatalogError(err)
		return
	}
	logger.L().Info("pricing catalog hot reload applied",
		zap.String("path", path), zap.Int("models", s.catalogModelCountLocked()))
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

// setCatalogError 记录目录文件问题；相同错误只告警一次，避免刷屏。
func (s *PricingService) setCatalogError(err error) {
	msg := err.Error()
	s.catalogRuntime.mu.Lock()
	if s.catalogRuntime.lastErr == msg {
		s.catalogRuntime.mu.Unlock()
		return
	}
	s.catalogRuntime.lastErr = msg
	path := s.catalogRuntime.path
	s.catalogRuntime.mu.Unlock()
	logger.L().Warn("pricing catalog file problem", zap.String("path", path), zap.Error(err))
}

// catalogStatus 返回 GetStatus 使用的目录运行态快照。
func (s *PricingService) catalogStatus() map[string]any {
	s.catalogRuntime.mu.RLock()
	defer s.catalogRuntime.mu.RUnlock()
	out := map[string]any{
		"source":    s.catalogRuntime.source,
		"models":    s.catalogRuntime.models,
		"loaded_at": s.catalogRuntime.loadedAt,
	}
	if s.catalogRuntime.path != "" {
		out["path"] = s.catalogRuntime.path
	}
	if s.catalogRuntime.lastErr != "" {
		out["last_error"] = s.catalogRuntime.lastErr
	}
	return out
}
