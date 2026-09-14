package domain

import (
	"testing"
)

// 注意：上游在 v0.2.2 后继续扩展 DefaultAntigravityModelMapping（Gemini 3.7/3.8 Flash
// 档位），本 fork 按零默认映射重构（FORK_MODEL_REFACTOR_PLAN §3）已删除该变量与自动注入，
// 因此不保留对应的上游测试；账号可用模型 = 显式 mapping keys ∪ 原生快照。

func TestDefaultBedrockModelMapping_ContainsNewClaudeModels(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"claude-fable-5-1": "anthropic.claude-fable-5-1",
		"claude-fable-5":   "anthropic.claude-fable-5",
		"claude-opus-4-8":  "us.anthropic.claude-opus-4-8-v1",
	}
	for from, want := range cases {
		got, ok := DefaultBedrockModelMapping[from]
		if !ok {
			t.Fatalf("expected Bedrock mapping for %q to exist", from)
		}
		if got != want {
			t.Fatalf("unexpected Bedrock mapping for %q: got %q want %q", from, got, want)
		}
	}
}
