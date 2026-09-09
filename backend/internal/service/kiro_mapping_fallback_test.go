package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestAccountKiroPassthroughWithoutExplicitMapping(t *testing.T) {
	account := &Account{Platform: PlatformKiro}

	// 零默认映射：Kiro 不再注入默认映射；无显式映射 = 透传。
	// 无快照 → fail-open 放行；GetMappedModel 无映射时原样返回。
	require.True(t, account.IsModelSupported("gpt-4o"))
	require.True(t, account.IsModelSupported("kiro-gpt-4o"))
	require.True(t, account.IsModelSupported("auto"))
	require.Equal(t, "claude-sonnet-4-6", account.GetMappedModel("claude-sonnet-4-6"))
}

func TestGatewayServiceCalculateTokenCost_KiroAutoUsesConservativeFallback(t *testing.T) {
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1.1

	svc := NewGatewayService(
		nil,                         // accountRepo
		nil,                         // groupRepo
		nil,                         // usageLogRepo
		nil,                         // usageBillingRepo
		nil,                         // userRepo
		nil,                         // userSubRepo
		nil,                         // userGroupRateRepo
		nil,                         // cache
		cfg,                         // cfg
		nil,                         // schedulerSnapshot
		nil,                         // concurrencyService
		NewBillingService(cfg, nil), // billingService
		nil,                         // rateLimitService
		nil,                         // billingCacheService
		nil,                         // identityService
		nil,                         // httpUpstream
		nil,                         // deferredService
		nil,                         // claudeTokenProvider
		nil,                         // kiroTokenProvider
		nil,                         // kiroCooldownStore
		nil,                         // sessionLimitCache
		nil,                         // rpmCache
		nil,                         // digestStore
		nil,                         // settingService
		nil,                         // tlsFPProfileService
		nil,                         // channelService
		nil,                         // resolver
		nil,                         // compositeResolver
		nil,                         // balanceNotifyService
		nil,                         // userPlatformQuotaRepo
	)

	result := &ForwardResult{
		Model:         "auto",
		UpstreamModel: "auto",
		Usage: ClaudeUsage{
			InputTokens:  20,
			OutputTokens: 10,
		},
	}

	expected, err := svc.billingService.CalculateCost(kiroConservativeFallbackBillingModel, UsageTokens{
		InputTokens:  20,
		OutputTokens: 10,
	}, 1.1)
	require.NoError(t, err)

	cost := svc.calculateTokenCost(context.Background(), result, &APIKey{}, "auto", 1.1, time.Time{}, &recordUsageOpts{IsKiroAccount: true})
	require.NotNil(t, cost)
	require.InDelta(t, expected.ActualCost, cost.ActualCost, 1e-12)
	require.InDelta(t, expected.TotalCost, cost.TotalCost, 1e-12)
}
