//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
)

func TestGrokAccountModelMappingCacheInvalidatesWithRuntimeSettings(t *testing.T) {
	original := xai.RuntimeModelMappingOptions()
	t.Cleanup(func() { xai.SetRuntimeModelMappingOptions(original) })

	// 零默认映射：账号 GetMappedModel 只返回显式 model_mapping，不再注入 Grok
	// cross-client 运行时映射（该行为已上移到分组调度层 ResolveMessagesDispatchModel，P3）。
	// 这里验证：账号显式映射在运行时设置变化（触发缓存失效重算）后仍稳定返回，
	// 不受 cross-client 运行时映射影响。
	account := &Account{Platform: PlatformGrok, Credentials: map[string]any{
		"model_mapping": map[string]any{"claude-sonnet-4-5": "grok-explicit-target"},
	}}

	xai.SetRuntimeModelMappingOptions(xai.ModelMappingOptions{})
	requireMappedModel(t, account, "claude-sonnet-4-5", "grok-explicit-target")

	xai.SetRuntimeModelMappingOptions(xai.ModelMappingOptions{
		DefaultText:          "grok-build-0.1",
		EnableCrossClientMap: true,
	})
	// 运行时设置变化后缓存失效重算，显式映射仍稳定返回（cross-client 不泄漏进账号映射）。
	requireMappedModel(t, account, "claude-sonnet-4-5", "grok-explicit-target")
}

func requireMappedModel(t *testing.T, account *Account, requested, expected string) {
	t.Helper()
	if actual := account.GetMappedModel(requested); actual != expected {
		t.Fatalf("GetMappedModel(%q) = %q, want %q", requested, actual, expected)
	}
}

func TestMatchWildcard(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		str      string
		expected bool
	}{
		// 精确匹配
		{"exact match", "claude-sonnet-4-5", "claude-sonnet-4-5", true},
		{"exact mismatch", "claude-sonnet-4-5", "claude-opus-4-5", false},

		// 通配符匹配
		{"wildcard prefix match", "claude-*", "claude-sonnet-4-5", true},
		{"wildcard prefix match 2", "claude-*", "claude-opus-4-5-thinking", true},
		{"wildcard prefix mismatch", "claude-*", "gemini-3-flash", false},
		{"wildcard partial match", "gemini-3*", "gemini-3-flash", true},
		{"wildcard partial match 2", "gemini-3*", "gemini-3-pro-image", true},
		{"wildcard partial mismatch", "gemini-3*", "gemini-2.5-flash", false},

		// 边界情况
		{"empty pattern exact", "", "", true},
		{"empty pattern mismatch", "", "claude", false},
		{"single star", "*", "anything", true},
		{"star at end only", "abc*", "abcdef", true},
		{"star at end empty suffix", "abc*", "abc", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchWildcard(tt.pattern, tt.str)
			if result != tt.expected {
				t.Errorf("matchWildcard(%q, %q) = %v, want %v", tt.pattern, tt.str, result, tt.expected)
			}
		})
	}
}

func TestMatchWildcardMappingResult(t *testing.T) {
	tests := []struct {
		name           string
		mapping        map[string]string
		requestedModel string
		expected       string
		matched        bool
	}{
		// 精确匹配优先于通配符
		{
			name: "exact match takes precedence",
			mapping: map[string]string{
				"claude-sonnet-4-5": "claude-sonnet-4-5-exact",
				"claude-*":          "claude-default",
			},
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-4-5-exact",
			matched:        true,
		},

		// 最长通配符优先
		{
			name: "longer wildcard takes precedence",
			mapping: map[string]string{
				"claude-*":         "claude-default",
				"claude-sonnet-*":  "claude-sonnet-default",
				"claude-sonnet-4*": "claude-sonnet-4-series",
			},
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-4-series",
			matched:        true,
		},

		// 单个通配符
		{
			name: "single wildcard",
			mapping: map[string]string{
				"claude-*": "claude-mapped",
			},
			requestedModel: "claude-opus-4-5",
			expected:       "claude-mapped",
			matched:        true,
		},

		// 无匹配返回原始模型
		{
			name: "no match returns original",
			mapping: map[string]string{
				"claude-*": "claude-mapped",
			},
			requestedModel: "gemini-3-flash",
			expected:       "gemini-3-flash",
			matched:        false,
		},

		// 空映射返回原始模型
		{
			name:           "empty mapping returns original",
			mapping:        map[string]string{},
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-4-5",
			matched:        false,
		},

		// Gemini 模型映射
		{
			name: "gemini wildcard mapping",
			mapping: map[string]string{
				"gemini-3*":   "gemini-3-pro-high",
				"gemini-2.5*": "gemini-2.5-flash",
			},
			requestedModel: "gemini-3-flash-preview",
			expected:       "gemini-3-pro-high",
			matched:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, matched := matchWildcardMappingResult(tt.mapping, tt.requestedModel)
			if result != tt.expected || matched != tt.matched {
				t.Errorf("matchWildcardMappingResult(%v, %q) = (%q, %v), want (%q, %v)", tt.mapping, tt.requestedModel, result, matched, tt.expected, tt.matched)
			}
		})
	}
}

func TestAccountIsModelSupported(t *testing.T) {
	tests := []struct {
		name           string
		platform       string
		credentials    map[string]any
		extra          map[string]any
		requestedModel string
		expected       bool
	}{
		// 无映射 = 允许所有
		{
			name:           "no mapping allows all",
			credentials:    nil,
			requestedModel: "any-model",
			expected:       true,
		},
		{
			name:           "empty mapping allows all",
			credentials:    map[string]any{},
			requestedModel: "any-model",
			expected:       true,
		},

		// 精确匹配
		{
			name: "exact match supported",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-sonnet-4-5": "target-model",
				},
			},
			requestedModel: "claude-sonnet-4-5",
			expected:       true,
		},
		{
			name: "exact match not supported",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-sonnet-4-5": "target-model",
				},
			},
			// 零默认：显式映射 key claude-sonnet-4-5 可服务；claude-opus-4-5 未映射且不在上游快照 → 不支持。
			extra:          ApplyUpstreamModelSnapshot(nil, []string{"target-model"}, time.Now().UTC()),
			requestedModel: "claude-opus-4-5",
			expected:       false,
		},

		// 通配符匹配
		{
			name: "wildcard match supported",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-*": "claude-sonnet-4-5",
				},
			},
			requestedModel: "claude-opus-4-5-thinking",
			expected:       true,
		},
		{
			name:     "gemini customtools alias matches normalized mapping",
			platform: PlatformGemini,
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"gemini-3.1-pro-preview": "gemini-3.1-pro-preview",
				},
			},
			requestedModel: "gemini-3.1-pro-preview-customtools",
			expected:       true,
		},
		{
			name: "wildcard match not supported",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-*": "claude-sonnet-4-5",
				},
			},
			// 零默认：gemini-3-flash 不匹配 claude-* 且不在上游快照 → 不支持。
			extra:          ApplyUpstreamModelSnapshot(nil, []string{"claude-sonnet-4-5"}, time.Now().UTC()),
			requestedModel: "gemini-3-flash",
			expected:       false,
		},

		{
			name: "rename-only mapping does not whitelist",
			credentials: map[string]any{
				CredentialKeyModelMappingRestricts: false,
				"model_mapping": map[string]any{
					"gemini-3.6-flash": "gemini-3.6-flash-tiered",
				},
			},
			requestedModel: "gemini-3.7-flash",
			expected:       true,
		},
		{
			name: "explicit whitelist flag still restricts",
			credentials: map[string]any{
				CredentialKeyModelMappingRestricts: true,
				"model_mapping": map[string]any{
					"gemini-3.6-flash": "gemini-3.6-flash-tiered",
				},
			},
			// 零默认：legacy restricts flag 已不再参与判定；可服务集 = 显式 key ∪ 快照。
			// gemini-3.7-flash 未映射且不在快照 → 不支持（限制来自快照，非 legacy flag）。
			extra:          ApplyUpstreamModelSnapshot(nil, []string{"gemini-3.6-flash-tiered"}, time.Now().UTC()),
			requestedModel: "gemini-3.7-flash",
			expected:       false,
		},
		{
			name:           "legacy antigravity empty mapping still uses default whitelist",
			platform:       PlatformAntigravity,
			credentials:    map[string]any{},
			// 零默认：无默认白名单；可服务集 = 上游原生快照。claude-unknown-model 不在快照 → 不支持。
			extra:          ApplyUpstreamModelSnapshot(nil, []string{"claude-sonnet-4-5"}, time.Now().UTC()),
			requestedModel: "claude-unknown-model",
			expected:       false,
		},
		{
			name:     "new antigravity rename-only allows unmapped models",
			platform: PlatformAntigravity,
			credentials: map[string]any{
				CredentialKeyModelMappingRestricts: false,
			},
			requestedModel: "gemini-3.7-flash",
			expected:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform:    tt.platform,
				Credentials: tt.credentials,
				Extra:       tt.extra,
			}
			result := account.IsModelSupported(tt.requestedModel)
			if result != tt.expected {
				t.Errorf("IsModelSupported(%q) = %v, want %v", tt.requestedModel, result, tt.expected)
			}
		})
	}
}

func TestAccountGetMappedModel(t *testing.T) {
	tests := []struct {
		name           string
		platform       string
		credentials    map[string]any
		requestedModel string
		expected       string
	}{
		// 无映射 = 返回原始模型
		{
			name:           "no mapping returns original",
			credentials:    nil,
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-4-5",
		},
		{
			name:           "no mapping preserves gemini customtools model",
			platform:       PlatformGemini,
			credentials:    nil,
			requestedModel: "gemini-3.1-pro-preview-customtools",
			expected:       "gemini-3.1-pro-preview-customtools",
		},

		// 精确匹配
		{
			name: "exact match",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-sonnet-4-5": "target-model",
				},
			},
			requestedModel: "claude-sonnet-4-5",
			expected:       "target-model",
		},

		// 通配符匹配（最长优先）
		{
			name: "wildcard longest match",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"claude-*":        "claude-default",
					"claude-sonnet-*": "claude-sonnet-mapped",
				},
			},
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-mapped",
		},

		// 无匹配返回原始模型
		{
			name:     "gemini customtools alias resolves through normalized mapping",
			platform: PlatformGemini,
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"gemini-3.1-pro-preview": "gemini-3.1-pro-preview",
				},
			},
			requestedModel: "gemini-3.1-pro-preview-customtools",
			expected:       "gemini-3.1-pro-preview",
		},
		{
			name:     "gemini customtools exact mapping wins over normalized fallback",
			platform: PlatformGemini,
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"gemini-3.1-pro-preview":             "gemini-3.1-pro-preview",
					"gemini-3.1-pro-preview-customtools": "gemini-3.1-pro-preview-customtools",
				},
			},
			requestedModel: "gemini-3.1-pro-preview-customtools",
			expected:       "gemini-3.1-pro-preview-customtools",
		},
		{
			name: "no match returns original",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"gemini-*": "gemini-mapped",
				},
			},
			requestedModel: "claude-sonnet-4-5",
			expected:       "claude-sonnet-4-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform:    tt.platform,
				Credentials: tt.credentials,
			}
			result := account.GetMappedModel(tt.requestedModel)
			if result != tt.expected {
				t.Errorf("GetMappedModel(%q) = %q, want %q", tt.requestedModel, result, tt.expected)
			}
		})
	}
}

func TestAccountGetModelMapping_AntigravityNormalizesGemini31ProAliases(t *testing.T) {
	t.Parallel()

	// 零默认映射：GetModelMapping 只返回账号显式 model_mapping，不再自动归一化
	// gemini-3.1-pro 别名到上游路由（历史 DefaultAntigravityModelMapping 已删除）。
	// 需要别名→路由改写的运维在账号上显式配置即可。
	account := &Account{
		Platform: PlatformAntigravity,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				domain.AntigravityGemini31ProAgentModel: domain.AntigravityGemini31ProAgentModel,
				"gemini-3.1-pro-high":                   "gemini-3.1-pro-high",
				"gemini-3.1-pro-preview":                "gemini-3.1-pro-high",
			},
		},
	}

	mapping := account.GetModelMapping()

	// 显式映射原样返回。
	if got := mapping[domain.AntigravityGemini31ProAgentModel]; got != domain.AntigravityGemini31ProAgentModel {
		t.Fatalf("expected explicit %q preserved, got %q", domain.AntigravityGemini31ProAgentModel, got)
	}
	if got := mapping["gemini-3.1-pro-high"]; got != "gemini-3.1-pro-high" {
		t.Fatalf("expected explicit gemini-3.1-pro-high preserved as-is, got %q", got)
	}
	if got := mapping["gemini-3.1-pro-preview"]; got != "gemini-3.1-pro-high" {
		t.Fatalf("expected explicit gemini-3.1-pro-preview preserved as-is, got %q", got)
	}
	// 未显式配置的别名不再自动归一化（零默认）。
	if _, ok := mapping["gemini-3.1-pro"]; ok {
		t.Fatalf("did not expect auto-normalized gemini-3.1-pro alias, got %v", mapping)
	}
}

func TestAccountGetModelMapping_AntigravityPreservesGemini31ProOverrides(t *testing.T) {
	t.Parallel()

	account := &Account{
		Platform: PlatformAntigravity,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				domain.AntigravityGemini31ProAgentModel: domain.AntigravityGemini31ProAgentModel,
				"gemini-3.1-pro-high":                   "custom-high",
				"gemini-3.1-pro-preview":                "custom-preview",
			},
		},
	}

	mapping := account.GetModelMapping()

	if got := mapping["gemini-3.1-pro-high"]; got != "custom-high" {
		t.Fatalf("expected gemini-3.1-pro-high override to be preserved, got %q", got)
	}
	if got := mapping["gemini-3.1-pro-preview"]; got != "custom-preview" {
		t.Fatalf("expected gemini-3.1-pro-preview override to be preserved, got %q", got)
	}
	// 未显式配置的 gemini-3.1-pro 别名不再自动归一化（零默认映射）。
	if _, ok := mapping["gemini-3.1-pro"]; ok {
		t.Fatalf("did not expect auto-normalized gemini-3.1-pro alias, got %v", mapping)
	}
}

func TestAccountGetModelMapping_AntigravityGemini31ProAliasesRespectWildcard(t *testing.T) {
	t.Parallel()

	account := &Account{
		Platform: PlatformAntigravity,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				domain.AntigravityGemini31ProAgentModel: domain.AntigravityGemini31ProAgentModel,
				"gemini-3.1-*":                          "custom-wildcard",
			},
		},
	}

	mapping := account.GetModelMapping()

	if got := mapping["gemini-3.1-pro"]; got != "" {
		t.Fatalf("expected gemini-3.1-pro exact alias to stay unset when wildcard exists, got %q", got)
	}
	if got := mapping["gemini-3.1-pro-high"]; got != "" {
		t.Fatalf("expected gemini-3.1-pro-high exact alias to stay unset when wildcard exists, got %q", got)
	}
	if got := mapping["gemini-3.1-pro-preview"]; got != "" {
		t.Fatalf("expected gemini-3.1-pro-preview exact alias to stay unset when wildcard exists, got %q", got)
	}
}

func TestAccountResolveMappedModel(t *testing.T) {
	tests := []struct {
		name           string
		platform       string
		credentials    map[string]any
		requestedModel string
		expectedModel  string
		expectedMatch  bool
	}{
		{
			name:           "no mapping reports unmatched",
			credentials:    nil,
			requestedModel: "gpt-5.4",
			expectedModel:  "gpt-5.4",
			expectedMatch:  false,
		},
		{
			name: "exact passthrough mapping still counts as matched",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5.4": "gpt-5.4",
				},
			},
			requestedModel: "gpt-5.4",
			expectedModel:  "gpt-5.4",
			expectedMatch:  true,
		},
		{
			name: "wildcard passthrough mapping still counts as matched",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-*": "gpt-5.4",
				},
			},
			requestedModel: "gpt-5.4",
			expectedModel:  "gpt-5.4",
			expectedMatch:  true,
		},
		{
			name:     "gemini customtools alias reports normalized match",
			platform: PlatformGemini,
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"gemini-3.1-pro-preview": "gemini-3.1-pro-preview",
				},
			},
			requestedModel: "gemini-3.1-pro-preview-customtools",
			expectedModel:  "gemini-3.1-pro-preview",
			expectedMatch:  true,
		},
		{
			name:     "gemini customtools exact mapping reports exact match",
			platform: PlatformGemini,
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"gemini-3.1-pro-preview":             "gemini-3.1-pro-preview",
					"gemini-3.1-pro-preview-customtools": "gemini-3.1-pro-preview-customtools",
				},
			},
			requestedModel: "gemini-3.1-pro-preview-customtools",
			expectedModel:  "gemini-3.1-pro-preview-customtools",
			expectedMatch:  true,
		},
		{
			name: "missing mapping reports unmatched",
			credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5.2": "gpt-5.2",
				},
			},
			requestedModel: "gpt-5.4",
			expectedModel:  "gpt-5.4",
			expectedMatch:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := &Account{
				Platform:    tt.platform,
				Credentials: tt.credentials,
			}
			mappedModel, matched := account.ResolveMappedModel(tt.requestedModel)
			if mappedModel != tt.expectedModel || matched != tt.expectedMatch {
				t.Fatalf("ResolveMappedModel(%q) = (%q, %v), want (%q, %v)", tt.requestedModel, mappedModel, matched, tt.expectedModel, tt.expectedMatch)
			}
		})
	}
}

func TestAccountGetModelMapping_AntigravityEnsuresGeminiDefaultPassthroughs(t *testing.T) {
	account := &Account{
		Platform: PlatformAntigravity,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gemini-3-pro-high": "gemini-3.1-pro-high",
			},
		},
	}

	// 零默认映射：Antigravity 账号 GetModelMapping 只返回显式 model_mapping，
	// 不再自动填充 gemini 默认透传（历史 DefaultAntigravityModelMapping 已删除）。
	mapping := account.GetModelMapping()

	// 显式映射保留。
	if got := mapping["gemini-3-pro-high"]; got != "gemini-3.1-pro-high" {
		t.Fatalf("expected explicit gemini-3-pro-high mapping preserved, got %q", got)
	}
	// 未显式配置的模型不再自动填充透传。
	for _, model := range []string{"gemini-3-flash", "gemini-3.1-pro-high", "gemini-3.1-pro-low"} {
		if _, ok := mapping[model]; ok {
			t.Fatalf("did not expect auto-filled passthrough for %q, got %v", model, mapping)
		}
	}
}

func TestAccountGetModelMapping_GoogleOneUsesConservativeDefaults(t *testing.T) {
	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"oauth_type": "google_one",
		},
	}

	// 零默认映射：google_one 账号无显式 model_mapping 时 GetModelMapping 为空，
	// 不再自动填充保守默认透传（历史 geminicli.GoogleOneModelMapping 已删除）。
	mapping := account.GetModelMapping()
	if len(mapping) != 0 {
		t.Fatalf("expected empty mapping for google_one account without explicit mapping, got %v", mapping)
	}

	// 无快照且无显式映射：支持判定回退到上游快照；快照缺失不限制调度（透传）→ 支持。
	if !account.IsModelSupported("gemini-3.5-flash") {
		t.Fatal("google_one account without snapshot should passthrough (no restriction) and report the model as supported")
	}

	// 有快照且快照不含该模型时，按证据型判定 → 不支持。
	withSnapshot := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"oauth_type": "google_one",
		},
		Extra: ApplyUpstreamModelSnapshot(nil, []string{"gemini-2.5-flash"}, time.Now().UTC()),
	}
	if withSnapshot.IsModelSupported("gemini-3.5-flash") {
		t.Fatal("google_one account with a snapshot lacking the model must not report it as supported")
	}
	if !withSnapshot.IsModelSupported("gemini-2.5-flash") {
		t.Fatal("google_one account snapshot covering the model should report it as supported")
	}
}

func TestAccountGetModelMapping_GoogleOnePreservesExplicitMapping(t *testing.T) {
	account := &Account{
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"oauth_type": "google_one",
			"model_mapping": map[string]any{
				"custom-model": "gemini-2.5-flash",
			},
		},
	}

	mapping := account.GetModelMapping()
	if mapping["custom-model"] != "gemini-2.5-flash" {
		t.Fatalf("expected explicit Google One mapping to be preserved, got %v", mapping)
	}
	if _, ok := mapping["gemini-2.5-flash"]; ok {
		t.Fatalf("did not expect defaults to overwrite an explicit mapping: %v", mapping)
	}
}

func TestAccountGetModelMapping_AntigravityRespectsWildcardOverride(t *testing.T) {
	account := &Account{
		Platform: PlatformAntigravity,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gemini-3*": "gemini-3.1-pro-high",
			},
		},
	}

	mapping := account.GetModelMapping()
	if _, exists := mapping["gemini-3-flash"]; exists {
		t.Fatalf("did not expect explicit gemini-3-flash passthrough when wildcard already exists")
	}
	if _, exists := mapping["gemini-3.1-pro-high"]; exists {
		t.Fatalf("did not expect explicit gemini-3.1-pro-high passthrough when wildcard already exists")
	}
	if _, exists := mapping["gemini-3.1-pro-low"]; exists {
		t.Fatalf("did not expect explicit gemini-3.1-pro-low passthrough when wildcard already exists")
	}
	if mapped := account.GetMappedModel("gemini-3-flash"); mapped != "gemini-3.1-pro-high" {
		t.Fatalf("expected wildcard mapping to stay effective, got: %q", mapped)
	}
}

func TestAccountGetModelMapping_CacheInvalidatesOnCredentialsReplace(t *testing.T) {
	account := &Account{
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"claude-3-5-sonnet": "upstream-a",
			},
		},
	}

	first := account.GetModelMapping()
	if first["claude-3-5-sonnet"] != "upstream-a" {
		t.Fatalf("unexpected first mapping: %v", first)
	}

	account.Credentials = map[string]any{
		"model_mapping": map[string]any{
			"claude-3-5-sonnet": "upstream-b",
		},
	}
	second := account.GetModelMapping()
	if second["claude-3-5-sonnet"] != "upstream-b" {
		t.Fatalf("expected cache invalidated after credentials replace, got: %v", second)
	}
}

func TestAccountGetModelMapping_CacheInvalidatesOnMappingLenChange(t *testing.T) {
	rawMapping := map[string]any{
		"claude-sonnet": "sonnet-a",
	}
	account := &Account{
		Credentials: map[string]any{
			"model_mapping": rawMapping,
		},
	}

	first := account.GetModelMapping()
	if len(first) != 1 {
		t.Fatalf("unexpected first mapping length: %d", len(first))
	}

	rawMapping["claude-opus"] = "opus-b"
	second := account.GetModelMapping()
	if second["claude-opus"] != "opus-b" {
		t.Fatalf("expected cache invalidated after mapping len change, got: %v", second)
	}
}

func TestAccountGetModelMapping_CacheInvalidatesOnInPlaceValueChange(t *testing.T) {
	rawMapping := map[string]any{
		"claude-sonnet": "sonnet-a",
	}
	account := &Account{
		Credentials: map[string]any{
			"model_mapping": rawMapping,
		},
	}

	first := account.GetModelMapping()
	if first["claude-sonnet"] != "sonnet-a" {
		t.Fatalf("unexpected first mapping: %v", first)
	}

	rawMapping["claude-sonnet"] = "sonnet-b"
	second := account.GetModelMapping()
	if second["claude-sonnet"] != "sonnet-b" {
		t.Fatalf("expected cache invalidated after in-place value change, got: %v", second)
	}
}
