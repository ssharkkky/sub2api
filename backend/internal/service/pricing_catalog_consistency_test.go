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

// 仓库数据文件（相对本包目录）。这两份文件由 tools/generate_model_prices.py
// 生成/校验：价表是本仓库维护的 LiteLLM 全量价表，目录是 fork 定价底稿。
// 本测试把它们绑成一组不变量，任何一侧单独漂移都会让 CI 变红。
const (
	consistencyPriceTablePath = "../../../deploy/data/model_prices.json"
	consistencyPriceSHAPath   = "../../../deploy/data/model_prices.sha256"
	consistencyCatalogPath    = "../modelcatalog/data/catalog.json"
	consistencyCatalogSHAPath = "../modelcatalog/data/catalog.sha256"
)

func sha256HexFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	require.NoError(t, err, "read %s", path)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func loadConsistencyCatalog(t *testing.T) modelcatalog.File {
	t.Helper()
	body, err := os.ReadFile(consistencyCatalogPath)
	require.NoError(t, err)
	var doc modelcatalog.File
	require.NoError(t, json.Unmarshal(body, &doc), "catalog.json must parse as modelcatalog.File")
	require.NotEmpty(t, doc.Models)
	return doc
}

func loadConsistencyPriceTable(t *testing.T) map[string]*LiteLLMModelPricing {
	t.Helper()
	body, err := os.ReadFile(consistencyPriceTablePath)
	require.NoError(t, err)
	svc := NewPricingService(&config.Config{}, nil)
	data, err := svc.parsePricingData(body)
	require.NoError(t, err, "deploy/data/model_prices.json must parse with the production parser")
	require.NotEmpty(t, data)
	return data
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

// 不变量 1：两个 .sha256 锚点文件必须与数据文件匹配（纯 hex）。
// 运行时靠它们做「无变化」短路；失配 = 每个实例每 10min 重复下载正文。
func TestConsistencySHA256AnchorsMatch(t *testing.T) {
	for name, paths := range map[string][2]string{
		"model_prices.json": {consistencyPriceTablePath, consistencyPriceSHAPath},
		"catalog.json":      {consistencyCatalogPath, consistencyCatalogSHAPath},
	} {
		t.Run(name, func(t *testing.T) {
			dataPath, shaPath := paths[0], paths[1]
			want := sha256HexFile(t, dataPath)
			gotBody, err := os.ReadFile(shaPath)
			require.NoError(t, err, "missing %s", shaPath)
			got := strings.TrimSpace(string(gotBody))
			require.Len(t, got, 64, "sha256 anchor must be bare hex")
			require.Equalf(t, want, got, "regenerate with: python3 tools/generate_model_prices.py --check")
		})
	}
}

// 不变量 2：目录里每个 id/alias 必须在价表有显式条目。
// （∨ 分支：别名允许共享 canonical 条目。）
// 这保证价表切换后，所有在售模型名都不落到家族启发式/零计费兜底。
func TestConsistencyEveryCatalogNameHasPriceTableEntry(t *testing.T) {
	doc := loadConsistencyCatalog(t)
	table := loadConsistencyPriceTable(t)

	for _, m := range doc.Models {
		names := []string{m.ID}
		names = append(names, func() []string {
			out := make([]string, 0, len(m.Aliases))
			for _, a := range m.Aliases {
				out = append(out, a.ID)
			}
			return out
		}()...)

		for _, name := range names {
			if _, ok := table[name]; ok {
				continue
			}
			if name != m.ID {
				if _, ok := table[m.ID]; ok {
					continue // 别名共享 canonical 条目
				}
			}
			t.Errorf("catalog name %q has no explicit entry in deploy/data/model_prices.json (add it, or it will fall to heuristic/zero billing)", name)
		}
	}
}

// 不变量 3：目录带价卡的每个模型，价表同名字段的每 token 价格必须与卡相等。
// 价表是运行时单一权威源，目录价卡是兜底层 + CI 一致性锚点；
// 改价必须同步两处（tools/generate_model_prices.py --check 会先变红）。
func TestConsistencyCatalogCardsMatchPriceTable(t *testing.T) {
	doc := loadConsistencyCatalog(t)
	table := loadConsistencyPriceTable(t)

	byID := make(map[string]modelcatalog.Model, len(doc.Models))
	for _, m := range doc.Models {
		byID[m.ID] = m
	}

	for _, m := range doc.Models {
		card := resolvedCard(m, byID)
		if card == nil {
			t.Errorf("catalog model %s has no resolvable price card", m.ID)
			continue
		}
		entry, ok := table[m.ID]
		require.Truef(t, ok, "catalog model %s missing from price table", m.ID)

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
}
