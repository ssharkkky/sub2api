package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
	"github.com/stretchr/testify/require"
)

func TestListCatalogStorefrontModels_IncludesGemini37(t *testing.T) {
	models := ListCatalogStorefrontModels("antigravity")
	require.NotEmpty(t, models)

	var flash37 *CatalogStorefrontModel
	for i := range models {
		if models[i].ID == "gemini-3.7-flash" {
			flash37 = &models[i]
			break
		}
	}
	require.NotNil(t, flash37)
	require.Equal(t, "gemini-3.7-flash", flash37.CanonicalID)
	require.NotNil(t, flash37.InputPrice)
	require.InDelta(t, 0.75e-6, *flash37.InputPrice, 1e-12)
	require.True(t, modelcatalog.Locked("gemini-3.7-flash"))
}

func TestAnnotateCatalogStorefrontCoverage_CountsSnapshotsNotIntersection(t *testing.T) {
	parentID := int64(8)
	accounts := []Account{
		{
			ID:       1,
			Platform: PlatformAntigravity,
			Extra:    ApplyUpstreamModelSnapshot(nil, []string{"gemini-3.7-flash", "gemini-3.6-flash-tiered"}, time.Unix(1, 0).UTC()),
		},
		{
			ID:       2,
			Platform: PlatformAntigravity,
			Extra:    ApplyUpstreamModelSnapshot(nil, []string{"gemini-3.6-flash-tiered"}, time.Unix(1, 0).UTC()),
			Credentials: map[string]any{
				"model_mapping": map[string]any{"gemini-3.6-flash": "gemini-3.6-flash-tiered"},
			},
		},
		{ID: 3, Platform: PlatformAntigravity},
		{ID: 4, Platform: PlatformOpenAI, Extra: ApplyUpstreamModelSnapshot(nil, []string{"gpt-5.4"}, time.Unix(1, 0).UTC())},
		{ID: 5, Platform: PlatformAntigravity, ParentAccountID: &parentID},
	}
	filtered := filterStorefrontCoverageAccounts(accounts, "antigravity")
	require.Equal(t, []int64{1, 2, 3}, []int64{filtered[0].ID, filtered[1].ID, filtered[2].ID})

	models := AnnotateCatalogStorefrontCoverage([]CatalogStorefrontModel{
		{ID: "gemini-3.7-flash"},
		{ID: "gemini-3.6-flash"},
		{ID: "gemini-3.6-flash-tiered"},
		{ID: "claude-unknown"},
	}, filtered)
	require.Equal(t, 1, *models[0].CoverageHave)
	require.Equal(t, 3, *models[0].CoverageTotal)
	require.Equal(t, 2, *models[0].CoverageSynced)
	require.Equal(t, 1, *models[1].CoverageHave)
	require.Equal(t, 2, *models[2].CoverageHave)
	require.Equal(t, 0, *models[3].CoverageHave)
}

func TestListCatalogStorefrontModelsWithCoverage_WithoutGroupsKeepsPlainCatalog(t *testing.T) {
	svc := &ChannelService{}
	models := svc.ListCatalogStorefrontModelsWithCoverage(context.Background(), "antigravity", nil)
	require.NotEmpty(t, models)
	require.Nil(t, models[0].CoverageTotal)
}

// storefrontAccountRepoStub 只实现 ListByGroup（本测试唯一用到的方法），
// 其余接口方法通过嵌入 interface 满足。
type storefrontAccountRepoStub struct {
	AccountRepository
	byGroup map[int64][]Account
	errs    map[int64]error
}

func (f *storefrontAccountRepoStub) ListByGroup(_ context.Context, groupID int64) ([]Account, error) {
	if err := f.errs[groupID]; err != nil {
		return nil, err
	}
	return f.byGroup[groupID], nil
}

func newStorefrontService(t *testing.T, byGroup map[int64][]Account) *ChannelService {
	t.Helper()
	svc := NewChannelService(nil, nil, nil, nil)
	svc.SetAccountRepository(&storefrontAccountRepoStub{byGroup: byGroup})
	return svc
}

// 绑定分组账号并集非空时，可选列表 = 账号 snapshot 并集（目录模型带价卡）。
func TestListCatalogStorefrontModelsWithCoverage_GroupScopedList(t *testing.T) {
	svc := newStorefrontService(t, map[int64][]Account{
		2: {
			{ID: 1, Platform: PlatformAntigravity, Extra: ApplyUpstreamModelSnapshot(nil, []string{"gemini-3.6-flash", "gemini-3.7-flash"}, time.Unix(1, 0).UTC())},
			{ID: 2, Platform: PlatformAntigravity, Extra: ApplyUpstreamModelSnapshot(nil, []string{"gemini-3.7-flash", "gemini-3.6-flash-tiered"}, time.Unix(1, 0).UTC())},
		},
	})

	models := svc.ListCatalogStorefrontModelsWithCoverage(context.Background(), "antigravity", []int64{2})
	require.Len(t, models, 3)
	require.Equal(t, "gemini-3.6-flash", models[0].ID)
	require.Equal(t, "gemini-3.6-flash-tiered", models[1].ID)
	require.Equal(t, "gemini-3.7-flash", models[2].ID)
	// 目录模型保留目录价卡
	require.NotNil(t, models[0].InputPrice)
	require.NotNil(t, models[1].InputPrice)
	require.NotNil(t, models[2].InputPrice)
	// coverage：共享模型 have=2，独占模型 have=1
	require.Equal(t, 1, *models[0].CoverageHave)
	require.Equal(t, 1, *models[1].CoverageHave)
	require.Equal(t, 2, *models[2].CoverageHave)
	require.Equal(t, 2, *models[0].CoverageTotal)
	require.Equal(t, 2, *models[2].CoverageSynced)
}

// 账号里有、目录里没有的模型（如内部模型 ID）也要进列表，保留原始写法、无价卡。
func TestListCatalogStorefrontModelsWithCoverage_IncludesNonCatalogModels(t *testing.T) {
	svc := newStorefrontService(t, map[int64][]Account{
		17: {
			{ID: 1, Platform: PlatformAntigravity, Extra: ApplyUpstreamModelSnapshot(nil, []string{"gemini-3.6-flash", "tab_flash_lite_preview"}, time.Unix(1, 0).UTC())},
		},
	})

	models := svc.ListCatalogStorefrontModelsWithCoverage(context.Background(), "antigravity", []int64{17})
	require.Len(t, models, 2)
	require.Equal(t, "gemini-3.6-flash", models[0].ID)
	require.NotNil(t, models[0].InputPrice)

	require.Equal(t, "tab_flash_lite_preview", models[1].ID)
	require.Equal(t, "tab_flash_lite_preview", models[1].DisplayName)
	require.Equal(t, "tab_flash_lite_preview", models[1].CanonicalID)
	require.Nil(t, models[1].InputPrice)
	require.Nil(t, models[1].OutputPrice)
	require.Equal(t, []string{"antigravity"}, models[1].Platforms)
	require.Equal(t, "token", models[1].BillingMode)
}

// 目录别名（gemini-3.7-flash-high → gemini-3.7-flash）命中时复用目录条目（含共享价卡）。
func TestListCatalogStorefrontModelsWithCoverage_AliasHit(t *testing.T) {
	svc := newStorefrontService(t, map[int64][]Account{
		2: {
			{ID: 1, Platform: PlatformAntigravity, Extra: ApplyUpstreamModelSnapshot(nil, []string{"gemini-3.7-flash-high"}, time.Unix(1, 0).UTC())},
		},
	})

	models := svc.ListCatalogStorefrontModelsWithCoverage(context.Background(), "antigravity", []int64{2})
	require.Len(t, models, 1)
	require.Equal(t, "gemini-3.7-flash-high", models[0].ID)
	require.Equal(t, "gemini-3.7-flash", models[0].CanonicalID)
	require.NotNil(t, models[0].InputPrice)
}

// 绑定分组里没有任何账号有 snapshot 时回退全目录（探测失败不能清空候选）。
func TestListCatalogStorefrontModelsWithCoverage_EmptyUnionFallsBackToCatalog(t *testing.T) {
	svc := newStorefrontService(t, map[int64][]Account{
		2: {{ID: 1, Platform: PlatformAntigravity}},
	})

	models := svc.ListCatalogStorefrontModelsWithCoverage(context.Background(), "antigravity", []int64{2})
	catalog := ListCatalogStorefrontModels("antigravity")
	require.Len(t, models, len(catalog))
	require.Equal(t, catalog[0].ID, models[0].ID)
	require.NotNil(t, models[0].CoverageTotal)
	require.Equal(t, 1, *models[0].CoverageTotal)
	require.Equal(t, 0, *models[0].CoverageHave)
}

// 大小写 / models/ 前缀 / 重复：规范化去重后命中目录条目。
func TestListCatalogStorefrontModelsWithCoverage_Normalization(t *testing.T) {
	svc := newStorefrontService(t, map[int64][]Account{
		2: {
			{ID: 1, Platform: PlatformAntigravity, Extra: ApplyUpstreamModelSnapshot(nil, []string{"GEMINI-3.6-FLASH", "gemini-3.6-flash", "models/gemini-3.7-flash"}, time.Unix(1, 0).UTC())},
		},
	})

	models := svc.ListCatalogStorefrontModelsWithCoverage(context.Background(), "antigravity", []int64{2})
	require.Len(t, models, 2)
	require.Equal(t, "gemini-3.6-flash", models[0].ID)
	require.Equal(t, "gemini-3.7-flash", models[1].ID)
	require.NotNil(t, models[0].InputPrice)
	require.NotNil(t, models[1].InputPrice)
	// 大写 snapshot 条目经规范化后 coverage 仍计数（与并集口径一致）
	require.Equal(t, 1, *models[0].CoverageHave)
	require.Equal(t, 1, *models[1].CoverageHave)
	require.Equal(t, 1, *models[0].CoverageTotal)
}

// 空串/纯空白/纯 models/ 前缀的 snapshot 条目不进入并集；
// 大写条目命中目录后 coverage 仍按规范化口径计数（大小写不敏感）。
func TestListCatalogStorefrontModelsWithCoverage_EmptySnapshotEntriesSkipped(t *testing.T) {
	svc := newStorefrontService(t, map[int64][]Account{
		2: {
			{ID: 1, Platform: PlatformAntigravity, Extra: ApplyUpstreamModelSnapshot(nil, []string{"", "   ", "models/", "models/   ", "GEMINI-3.7-FLASH"}, time.Unix(1, 0).UTC())},
		},
	})

	models := svc.ListCatalogStorefrontModelsWithCoverage(context.Background(), "antigravity", []int64{2})
	require.Len(t, models, 1)
	require.Equal(t, "gemini-3.7-flash", models[0].ID)
	require.Equal(t, 1, *models[0].CoverageHave) // 大写 snapshot 条目规范化后仍计数
}

// 影子账号（ParentAccountID != nil）不参与并集，也不计入 total。
func TestListCatalogStorefrontModelsWithCoverage_ShadowAccountsExcluded(t *testing.T) {
	parentID := int64(1)
	svc := newStorefrontService(t, map[int64][]Account{
		2: {
			{ID: 3, Platform: PlatformAntigravity, ParentAccountID: &parentID, Extra: ApplyUpstreamModelSnapshot(nil, []string{"gemini-3.6-flash"}, time.Unix(1, 0).UTC())},
			{ID: 2, Platform: PlatformAntigravity},
		},
	})

	models := svc.ListCatalogStorefrontModelsWithCoverage(context.Background(), "antigravity", []int64{2})
	// 影子被排除 → 并集为空 → 回退全目录
	require.Len(t, models, len(ListCatalogStorefrontModels("antigravity")))
	require.Equal(t, 1, *models[0].CoverageTotal) // total 只算非影子账号
	require.Equal(t, 0, *models[0].CoverageHave)
}

// 某个分组的 ListByGroup 报错不影响其他分组的范围判定。
func TestListCatalogStorefrontModelsWithCoverage_ListByGroupErrorTolerated(t *testing.T) {
	svc := NewChannelService(nil, nil, nil, nil)
	svc.SetAccountRepository(&storefrontAccountRepoStub{
		byGroup: map[int64][]Account{
			3: {{ID: 2, Platform: PlatformAntigravity, Extra: ApplyUpstreamModelSnapshot(nil, []string{"gemini-3.6-flash"}, time.Unix(1, 0).UTC())}},
		},
		errs: map[int64]error{2: errors.New("boom")},
	})

	models := svc.ListCatalogStorefrontModelsWithCoverage(context.Background(), "antigravity", []int64{2, 3})
	require.Len(t, models, 1)
	require.Equal(t, "gemini-3.6-flash", models[0].ID)
	require.Equal(t, 1, *models[0].CoverageTotal)
	require.Equal(t, 1, *models[0].CoverageHave)
}

// 过滤后 coverage 语义不变：have 只算 snapshot 覆盖的账号，total 含无 snapshot 账号。
func TestListCatalogStorefrontModelsWithCoverage_CoverageStillAnnotated(t *testing.T) {
	svc := newStorefrontService(t, map[int64][]Account{
		2: {
			{ID: 1, Platform: PlatformAntigravity, Extra: ApplyUpstreamModelSnapshot(nil, []string{"gemini-3.6-flash"}, time.Unix(1, 0).UTC())},
			{ID: 2, Platform: PlatformAntigravity},
		},
	})

	models := svc.ListCatalogStorefrontModelsWithCoverage(context.Background(), "antigravity", []int64{2})
	require.Len(t, models, 1)
	require.Equal(t, "gemini-3.6-flash", models[0].ID)
	require.NotNil(t, models[0].CoverageHave)
	require.Equal(t, 1, *models[0].CoverageHave)
	require.Equal(t, 2, *models[0].CoverageTotal)
	require.Equal(t, 1, *models[0].CoverageSynced)
}

// 其他平台的账号不参与并集（平台过滤口径与 coverage 一致）。
func TestListCatalogStorefrontModelsWithCoverage_IgnoresOtherPlatforms(t *testing.T) {
	svc := newStorefrontService(t, map[int64][]Account{
		6: {
			{ID: 1, Platform: PlatformOpenAI, Extra: ApplyUpstreamModelSnapshot(nil, []string{"gpt-5.4"}, time.Unix(1, 0).UTC())},
			{ID: 2, Platform: PlatformAntigravity, Extra: ApplyUpstreamModelSnapshot(nil, []string{"gemini-3.7-flash"}, time.Unix(1, 0).UTC())},
		},
	})

	models := svc.ListCatalogStorefrontModelsWithCoverage(context.Background(), "antigravity", []int64{6})
	require.Len(t, models, 1)
	require.Equal(t, "gemini-3.7-flash", models[0].ID)
	require.Equal(t, 1, *models[0].CoverageTotal) // total 只算 antigravity 账号
}
