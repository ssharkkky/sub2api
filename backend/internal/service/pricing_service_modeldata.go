package service

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// modelDataShape 描述本仓库模型数据文档的形态。
//
// 权威源是仓库里的单一文件 deploy/data/models.json（合并文档）：
//
//	{"version":1, "models":[...目录...], "prices":{...LiteLLM 价表...}}
//
// 目录（模型列表/alias/重写/lock 卡）与价表（计费数字）一次 fetch、一次校验、
// 一次原子换入，从机制上消除「两文件不同步」的漂移窗口。
type modelDataShape int

const (
	// shapeMerged: 同时带 "models"(数组) 与 "prices"(对象) 顶层键 — 合并文档（默认形态）。
	shapeMerged modelDataShape = iota
	// shapePricesOnly: 扁平 LiteLLM map — 旧 model_prices.json 形态（兼容期，价表段单独演进时仍可用）。
	shapePricesOnly
	// shapeCatalogOnly: 仅 "models" — 旧 catalog.json 形态（兼容期；独立 catalog_url 路径用）。
	shapeCatalogOnly
	// shapeUnknown: 无法识别 → 整体拒收，保留上一份有效数据。
	shapeUnknown
)

func (s modelDataShape) String() string {
	switch s {
	case shapeMerged:
		return "merged"
	case shapePricesOnly:
		return "prices-only"
	case shapeCatalogOnly:
		return "catalog-only"
	default:
		return "unknown"
	}
}

type modelDataProbe struct {
	Models json.RawMessage `json:"models"`
	Prices json.RawMessage `json:"prices"`
}

// classifyModelData 嗅探文档形态。顶层必须是 JSON 对象：
//   - "models"(数组) + "prices"(对象) → 合并文档（默认形态）；
//   - 仅 "models"(数组) → 独立目录文档（旧 catalog.json 形态）；
//   - 其它任意对象 → 扁平 LiteLLM 价表（键名任意；名为 "models" 的条目
//     值是价格对象而非数组，可借此区分撞名）。
func classifyModelData(body []byte) modelDataShape {
	var p modelDataProbe
	if err := json.Unmarshal(body, &p); err != nil {
		return shapeUnknown
	}
	isModelsArray := len(p.Models) > 0 && p.Models[0] == '['
	isPricesObject := len(p.Prices) > 0 && p.Prices[0] == '{'
	switch {
	case isModelsArray && isPricesObject:
		return shapeMerged
	case isModelsArray:
		// models 是 section 键（数组）：prices 键存在但非对象 = 畸形合并文档，
		// 整份拒收（不静默降级为 catalog-only 丢弃 prices 段）。
		if len(p.Prices) > 0 {
			return shapeUnknown
		}
		return shapeCatalogOnly
	case len(p.Prices) > 0 && !isPricesObject:
		// models 键缺席而 prices 键存在但非对象：扁平价表条目值必为对象，
		// 此处只能是畸形文档（名为 "prices" 的合法条目值是对象，不受影响）。
		return shapeUnknown
	default:
		return shapePricesOnly
	}
}

// modelDataDoc 是一份通过形态识别的数据文档，携带各段正文与整文档指纹。
type modelDataDoc struct {
	shape       modelDataShape
	body        []byte // 原始正文（落盘缓存用）
	hash        string // 整文档 sha256（远程锚点比较口径）
	catalogBody []byte
	hasCatalog  bool
	pricesBody  []byte
	hasPrices   bool
}

// decodeModelData 识别形态并切分各段正文。形态不可识别时返回错误（整体拒收）。
func decodeModelData(body []byte) (*modelDataDoc, error) {
	shape := classifyModelData(body)
	if shape == shapeUnknown {
		return nil, fmt.Errorf("unrecognized model data document: expected merged {models,prices}, catalog {models}, or flat LiteLLM price map")
	}
	doc := &modelDataDoc{shape: shape, body: body, hash: sha256Hex(body)}
	switch shape {
	case shapeMerged:
		var probe struct {
			Version int             `json:"version"`
			Models  json.RawMessage `json:"models"`
			Prices  json.RawMessage `json:"prices"`
		}
		if err := json.Unmarshal(body, &probe); err != nil {
			return nil, fmt.Errorf("decode merged model data: %w", err)
		}
		// modelcatalog.Load 期望 {"version":N,"models":[...]} 文档形态，
		// 用 RawMessage 原样重建（避免一次多余的解码+编码漂移）。
		catalogBody, err := json.Marshal(struct {
			Version int             `json:"version"`
			Models  json.RawMessage `json:"models"`
		}{orOne(probe.Version), probe.Models})
		if err != nil {
			return nil, fmt.Errorf("rebuild catalog section: %w", err)
		}
		doc.catalogBody, doc.hasCatalog = catalogBody, true
		doc.pricesBody, doc.hasPrices = []byte(probe.Prices), true
	case shapeCatalogOnly:
		doc.catalogBody, doc.hasCatalog = body, true
	case shapePricesOnly:
		doc.pricesBody, doc.hasPrices = body, true
	}
	return doc, nil
}

func orOne(v int) int {
	if v <= 0 {
		return 1
	}
	return v
}

// applyModelData 在一次事务内完成「目录 + 价表」换入：
//
//  1. 目录段（若有）→ modelcatalog.Load 完整校验（版本/重复 id/price_ref 闭包）；
//  2. 价表段（若有）→ parsePricingData 解析；
//  3. 同一 s.mu 临界区内换引用：目录走 modelcatalog 原子指针，价表 map 整体替换。
//     在读请求要么看到全部旧版、要么看到全部新版，不存在「新目录 + 旧价表」的
//     中间态（坏文档在步骤 1/2 已被整体拒收，内存与磁盘都保持上一份有效数据）;
//  4. 全部通过后正文才落盘（缓存 + 锚点，均 tmp+fsync+rename 原子写）。
//
// cacheWrite 非 nil 时正文落盘：带价表段的文档写 models.json + 锚点；
// 独立目录文档（catalog_url 兼容路径）写旧独立缓存 catalog.json。
// cacheWrite 为 nil 表示不落盘（本地文件来源：显式运维文件/本地缓存回读，
// 不重写源文件）。
func (s *PricingService) applyModelData(doc *modelDataDoc, source, path string, cacheWrite []byte) error {
	var cat *modelcatalog.Catalog
	if doc.hasCatalog {
		c, err := modelcatalog.Load(doc.catalogBody)
		if err != nil {
			return fmt.Errorf("validate catalog section: %w", err)
		}
		cat = c
	}
	var prices map[string]*LiteLLMModelPricing
	if doc.hasPrices {
		p, err := s.parsePricingData(doc.pricesBody)
		if err != nil {
			return fmt.Errorf("validate prices section: %w", err)
		}
		prices = p
	}
	if cat == nil && prices == nil {
		return fmt.Errorf("model data document carries no catalog and no prices")
	}

	// 显式目录文件（运维意图/air-gap）正在生效时，远程目录段让位（本地赢）；
	// 价表段照常生效。「生效」= 当前目录确实来自该文件（文件损坏未加载成功时
	// 不锁死远程，见 catalogSourceIsExplicit）。「远程」= path 为空；
	// 显式文件自身的加载/轮询（path=显式路径）不受让位拦截。
	if cat != nil && path == "" && s.catalogSourceIsExplicit() {
		s.warnCatalogConflict(doc.hash, s.catalogHashLocked())
		cat = nil
	}

	s.mu.Lock()
	if cat != nil {
		modelcatalog.Replace(cat)
	}
	if prices != nil {
		data := s.mergeFallbackPricingData(prices)
		data = mergeCatalogPricingData(s.mergeOverrideOnlyModels(data))
		warnDroppedLongContextLadders(s.pricingData, data)
		s.pricingData = data
		// 锚点 = 实际加载正文的哈希（与目录侧同语义）：锚点文件损坏/过期只会
		// 导致每轮冗余下载并告警，不会把更新永久冻结。
		s.localHash = doc.hash
		s.lastUpdated = time.Now()
	} else {
		// 目录-only 换入（显式文件 / 独立 catalog_url）：对现有价表重放 lock 覆盖
		// （调用方已持有 s.mu，用 Locked 变体避免重入死锁）。
		s.replayCatalogLockOverlayLocked()
	}
	s.mu.Unlock()

	// 目录运行态（面向运维的状态；hash 用于显式文件分歧比较与轮询指纹）
	if cat != nil {
		s.setCatalogState(path, source, "", doc.hash)
	}

	// 落盘（内存态优先，缓存只是离线种子；失败只告警）
	if cacheWrite != nil {
		if doc.hasPrices {
			if err := writeAtomic(s.getModelDataCachePath(), cacheWrite, 0644); err != nil {
				logger.L().Warn("pricing model data cache write failed (in-memory data stays active)",
					zap.String("path", s.getModelDataCachePath()), zap.Error(err))
			}
			if err := writeAtomic(s.getModelDataHashPath(), []byte(doc.hash+"\n"), 0644); err != nil {
				logger.L().Warn("pricing model data anchor write failed",
					zap.String("path", s.getModelDataHashPath()), zap.Error(err))
			}
		} else {
			if err := writeAtomic(s.getCatalogCachePath(), cacheWrite, 0644); err != nil {
				logger.L().Warn("pricing catalog cache write failed (in-memory catalog stays active)",
					zap.String("path", s.getCatalogCachePath()), zap.Error(err))
			}
		}
	}
	return nil
}

// setCatalogState 记录一次目录状态变更（path 为空 = 非本地文件来源）。
func (s *PricingService) setCatalogState(path, source, lastErr, hash string) {
	s.catalogRuntime.mu.Lock()
	s.catalogRuntime.path = path
	s.catalogRuntime.source = source
	s.catalogRuntime.loadedAt = time.Now()
	s.catalogRuntime.lastErr = lastErr
	if hash != "" {
		s.catalogRuntime.hash = hash
	}
	s.catalogRuntime.models = catalogModelCount(modelcatalog.Current())
	s.catalogRuntime.mu.Unlock()
}

// getModelDataCachePath 合并文档的本地缓存路径（启动期探测的第一顺位、
// 官方镜像种子位置 /app/data/models.json）。
func (s *PricingService) getModelDataCachePath() string {
	return filepath.Join(s.cfg.Pricing.DataDir, "models.json")
}

// getModelDataHashPath 合并文档的本地锚点文件（= 最近一次成功加载正文的哈希）。
func (s *PricingService) getModelDataHashPath() string {
	return filepath.Join(s.cfg.Pricing.DataDir, "models.json.sha256")
}

// catalogSourceIsExplicit 判断「显式 catalog_file 是否正在生效」：
// 配置了显式路径且当前目录确实来自它。文件损坏/尚未放入时返回 false，
// 远程目录段可正常接管（坏文件不锁死更新；60s 轮询修复后本地自动赢回）。
func (s *PricingService) catalogSourceIsExplicit() bool {
	explicit := ""
	if s.cfg != nil {
		explicit = strings.TrimSpace(s.cfg.Pricing.CatalogFile)
	}
	if explicit == "" {
		return false
	}
	s.catalogRuntime.mu.RLock()
	defer s.catalogRuntime.mu.RUnlock()
	return s.catalogRuntime.source == explicit
}

// modelDataLocalProbePaths 启动期本地数据文件探测顺序（高→低）：
// 合并文档缓存/镜像种子 → 旧独立目录缓存 → 旧价表缓存。
// 旧文件名保留探测是为了从两文件时代的部署平滑迁移（旧缓存仍可用，
// 首次远程同步成功后自动收敛到合并文档）。
func (s *PricingService) modelDataLocalProbePaths() []string {
	dataDir := s.cfg.Pricing.DataDir
	var paths []string
	add := func(p string) {
		if p != "" {
			paths = append(paths, p)
		}
	}
	add(filepath.Join(dataDir, "models.json"))
	add(filepath.Join(dataDir, "catalog.json"))
	add(filepath.Join(dataDir, "model_pricing.json"))
	return paths
}

// loadLocalModelData 启动期加载本地数据文件（最近一次成功同步的缓存，或镜像
// 种入的 /app/data/models.json）。形态自动识别；坏文件跳过并继续探测下一个；
// 全部失败返回错误，由调用方走兜底价表。
func (s *PricingService) loadLocalModelData() error {
	for _, p := range s.modelDataLocalProbePaths() {
		body, err := os.ReadFile(p)
		if err != nil {
			continue // 文件不存在是常态（首次部署），继续探测
		}
		doc, err := decodeModelData(body)
		if err != nil {
			logger.L().Warn("pricing model data file skipped (unrecognized shape), trying next candidate",
				zap.String("path", p), zap.Error(err))
			continue
		}
		if err := s.applyModelData(doc, p, p, nil); err != nil {
			logger.L().Warn("pricing model data file not applied, trying next candidate",
				zap.String("path", p), zap.Error(err))
			continue
		}
		logger.L().Info("pricing model data loaded from local file",
			zap.String("path", p),
			zap.String("shape", doc.shape.String()),
			zap.Int("models", s.catalogModelCountLocked()))
		return nil
	}
	return fmt.Errorf("no local model data file found")
}
