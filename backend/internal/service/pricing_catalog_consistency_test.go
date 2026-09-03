package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
	"github.com/stretchr/testify/require"
)

// 仓库模型数据文件（相对本包目录）。deploy/data/models.json 是合并文档：
// models 段（目录：id/alias/重写/lock 卡）+ prices 段（LiteLLM 全量价表）。
// 它由 tools/generate_model_prices.py 生成/校验，本测试把它绑成一组不变量，
// 且全部走生产代码路径（形态识别 + 目录校验 + 价表解析），
// 保证提交进仓库的文件一定能被运行时接受。
const (
	consistencyDocPath    = "../../../deploy/data/models.json"
	consistencyDocSHAPath = "../../../deploy/data/models.json.sha256"
)

func sha256HexFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// loadConsistencyDoc 用运行时同一套生产路径加载合并文档：
// 形态识别（必须是 merged）→ 目录段完整校验 → 价表段生产解析。
func loadConsistencyDoc(t *testing.T) (modelcatalog.File, map[string]*LiteLLMModelPricing) {
	t.Helper()
	body, err := os.ReadFile(consistencyDocPath)
	require.NoError(t, err)

	require.Equalf(t, shapeMerged, classifyModelData(body),
		"deploy/data/models.json must be a merged document {models,prices} (regenerate with tools/generate_model_prices.py)")
	doc, err := decodeModelData(body)
	require.NoError(t, err)
	require.True(t, doc.hasCatalog && doc.hasPrices)

	var file modelcatalog.File
	require.NoError(t, json.Unmarshal(doc.catalogBody, &file), "models section must parse as modelcatalog.File")
	require.NotEmpty(t, file.Models)

	svc := NewPricingService(&config.Config{}, nil)
	table, err := svc.parsePricingData(doc.pricesBody)
	require.NoError(t, err, "prices section must parse with the production parser")
	require.NotEmpty(t, table)
	return file, table
}

// resolvedCard 复刻目录加载时的 price_ref 解析规则。
func resolvedCard(m modelcatalog.Model, byID map[string]modelcatalog.Model) *modelcatalog.Price {
	if m.Price != nil {
		return m.Price
	}
	if m.PriceRef != "" {
		if ref, ok := byID[m.PriceRef]; ok {
			return ref.Price
		}
	}
	return nil
}

// priceNear 允许 1e-9 相对误差：生成器用 %.12g 规整浮点表示，
// 与 Go 的 perMTok*1e-6 可能存在 1 ULP 差异；真实价格漂移远超该量级。
func priceNear(t *testing.T, actual, expected float64, field string) {
	t.Helper()
	tol := 1e-9 * math.Max(math.Abs(expected), math.Abs(actual))
	if tol == 0 {
		tol = 1e-30
	}
	require.InDeltaf(t, expected, actual, tol, "%s", field)
}

// 不变量 1：.sha256 锚点文件必须与合并文档匹配（纯 hex）。
// 运行时靠它做「无变化」短路（锚点相等连正文都不下载）；
// 失配 = 每个实例每 10min 重复下载正文。
func TestConsistencySHA256AnchorMatches(t *testing.T) {
	want := sha256HexFile(t, consistencyDocPath)
	gotBody, err := os.ReadFile(consistencyDocSHAPath)
	require.NoError(t, err, "missing %s", consistencyDocSHAPath)
	got := strings.TrimSpace(string(gotBody))
	require.Len(t, got, 64, "sha256 anchor must be bare hex")
	require.Equalf(t, want, got, "regenerate with: python3 tools/generate_model_prices.py --check")
}

// 不变量 2：目录里每个 id/alias 必须在 prices 段有显式条目。
// （∨ 分支：别名允许共享 canonical 条目。）
// 这保证远程换入后，所有在售模型名都能精确命中价表，
// 不落到家族启发式/零计费兜底。
func TestConsistencyEveryCatalogNameHasPriceTableEntry(t *testing.T) {
	file, table := loadConsistencyDoc(t)

	for _, m := range file.Models {
		names := []string{m.ID}
		for _, a := range m.Aliases {
			names = append(names, a.ID)
		}

		for _, name := range names {
			if _, ok := table[name]; ok {
				continue
			}
			if name != m.ID {
				if _, ok := table[m.ID]; ok {
					continue // 别名共享 canonical 条目
				}
			}
			t.Errorf("catalog name %q has no explicit entry in deploy/data/models.json prices (add it, or it will fall to heuristic/zero billing)", name)
		}
	}
}

// 不变量 3：每个 lock 模型必须带可解析价卡，且价表同名字段的每 token 价格
// 必须与卡相等。lock 卡是合并文档里唯一保留的价卡（其余模型价格由 prices 段
// 承载，上游/仓库生成）；改价必须同步两处
// （tools/generate_model_prices.py --check 会先变红）。
func TestConsistencyLockCardsMatchPriceTable(t *testing.T) {
	file, table := loadConsistencyDoc(t)

	byID := make(map[string]modelcatalog.Model, len(file.Models))
	for _, m := range file.Models {
		byID[m.ID] = m
	}

	locks := 0
	for _, m := range file.Models {
		if !m.LockPrice {
			continue
		}
		locks++
		card := resolvedCard(m, byID)
		require.NotNilf(t, card, "locked catalog model %s has no resolvable price card", m.ID)
		entry, ok := table[m.ID]
		require.Truef(t, ok, "locked catalog model %s missing from prices", m.ID)

		check := func(got float64, want *float64, field string) {
			if want == nil {
				return
			}
			priceNear(t, got, modelcatalog.PerToken(*want), m.ID+"."+field)
		}
		check(entry.InputCostPerToken, card.InputPerMTok, "input")
		check(entry.OutputCostPerToken, card.OutputPerMTok, "output")
		check(entry.InputCostPerTokenPriority, card.InputPriorityPerMTok, "input_priority")
		check(entry.OutputCostPerTokenPriority, card.OutputPriorityPerMTok, "output_priority")
		check(entry.CacheCreationInputTokenCost, card.CacheWritePerMTok, "cache_write")
		check(entry.CacheCreationInputTokenCostPriority, card.CacheWritePriorityPerMTok, "cache_write_priority")
		check(entry.CacheReadInputTokenCost, card.CacheReadPerMTok, "cache_read")
		check(entry.CacheReadInputTokenCostPriority, card.CacheReadPriorityPerMTok, "cache_read_priority")
	}
	require.GreaterOrEqualf(t, locks, 1, "merged document must keep at least one lock card (gemini-3.6/3.7-flash)")
}

// 不变量 4：合并文档的目录段必须通过运行时完整校验（版本号/重复 id/
// price 与 price_ref 互斥/price_ref 闭包）——即 modelcatalog.Load 接受它。
// 坏文档会在运行时被整体拒收（保留 last-good），CI 先于生产发现。
func TestConsistencyCatalogSectionLoadsThroughProductionValidator(t *testing.T) {
	body, err := os.ReadFile(consistencyDocPath)
	require.NoError(t, err)
	doc, err := decodeModelData(body)
	require.NoError(t, err)
	require.True(t, doc.hasCatalog)
	_, err = modelcatalog.Load(doc.catalogBody)
	require.NoErrorf(t, err, "models section must pass modelcatalog.Load (the exact runtime validator)")
}
