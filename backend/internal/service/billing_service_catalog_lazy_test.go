package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
	"github.com/stretchr/testify/require"
)

// 目录 baseline 回退必须在热换入后对计费立即可见（不重启、不重建 map）：
// 这是「仓库 PR 合并 → 自动拉取」链路的最后一环，也是反向 skew 窗口
// （新目录 + 旧价表 ≤10min）的兜底。
func TestBillingCatalogFallbackVisibleAfterHotSwap(t *testing.T) {
	restoreEmbeddedCatalog(t)
	svc := NewBillingService(&config.Config{}, nil)

	v1 := loadTestCatalog(t, `{
	  "version": 1,
	  "models": [
	    {"id": "lazy-catalog-model", "platforms": ["openai"], "kind": "chat", "billing_mode": "token",
	     "price": {"input_per_mtok": 1, "output_per_mtok": 2}}
	  ]
	}`)
	modelcatalog.Replace(v1)

	p1, err := svc.GetModelPricing("lazy-catalog-model")
	require.NoError(t, err)
	require.InDelta(t, 1e-6, p1.InputPricePerToken, 1e-18)
	require.InDelta(t, 2e-6, p1.OutputPricePerToken, 1e-18)

	// 热换入新价（模拟仓库更新 / 本地热修复生效）
	v2 := loadTestCatalog(t, `{
	  "version": 1,
	  "models": [
	    {"id": "lazy-catalog-model", "platforms": ["openai"], "kind": "chat", "billing_mode": "token",
	     "price": {"input_per_mtok": 3, "output_per_mtok": 4}}
	  ]
	}`)
	modelcatalog.Replace(v2)

	p2, err := svc.GetModelPricing("lazy-catalog-model")
	require.NoError(t, err)
	require.InDelta(t, 3e-6, p2.InputPricePerToken, 1e-18, "hot-swapped catalog price must be visible without restart")
	require.InDelta(t, 4e-6, p2.OutputPricePerToken, 1e-18)
}

// 思考档变体走共享卡，热换入后同样即时可见。
func TestBillingCatalogFallbackSharedCardMembers(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)

	for _, model := range []string{"gemini-3.7-flash-high", "gemini-3.7-flash-low"} {
		t.Run(model, func(t *testing.T) {
			pricing, err := svc.GetModelPricing(model)
			require.NoError(t, err)
			require.InDelta(t, 0.75e-6, pricing.InputPricePerToken, 1e-15)
			require.InDelta(t, 3.75e-6, pricing.OutputPricePerToken, 1e-15)
		})
	}
}

// lock_price 卡对硬编码回退条目的覆盖必须走克隆，不改动共享的 map 值。
func TestBillingCatalogLockOverlayDoesNotMutateSharedEntry(t *testing.T) {
	svc := NewBillingService(&config.Config{}, nil)
	shared := svc.fallbackPrices["gemini-3.6-flash"]
	require.NotNil(t, shared, "hardcoded fallback entry must exist")
	sharedInput := shared.InputPricePerToken

	pricing, err := svc.GetModelPricing("gemini-3.6-flash")
	require.NoError(t, err)
	// 内嵌目录的 lock 卡（$1.50/$7.50/$0.15）是最强覆盖
	require.InDelta(t, 1.5e-6, pricing.InputPricePerToken, 1e-15)
	require.InDelta(t, 7.5e-6, pricing.OutputPricePerToken, 1e-15)
	require.InDelta(t, 0.15e-6, pricing.CacheReadPricePerToken, 1e-15)
	// 共享条目保持原值
	require.InDelta(t, sharedInput, shared.InputPricePerToken, 1e-15)
}

// HasIdentifiedTokenPricing 的准入口径必须包含目录 baseline
// （上游自报模型名的计费准入判断依赖它）。
func TestBillingHasIdentifiedTokenPricingCoversCatalog(t *testing.T) {
	restoreEmbeddedCatalog(t)
	svc := NewBillingService(&config.Config{}, nil)
	modelcatalog.Replace(loadTestCatalog(t, `{
	  "version": 1,
	  "models": [
	    {"id": "identified-catalog-model", "platforms": ["openai"], "kind": "chat", "billing_mode": "token",
	     "price": {"input_per_mtok": 1, "output_per_mtok": 2}}
	  ]
	}`))
	require.True(t, svc.HasIdentifiedTokenPricing("identified-catalog-model"))
	require.False(t, svc.HasIdentifiedTokenPricing("totally-unknown-model-xyz"))
}
