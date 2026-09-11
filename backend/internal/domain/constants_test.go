package domain

import (
	"strings"
	"testing"
)


	for from, want := range cases {
		got, ok := DefaultAntigravityModelMapping[from]
		if !ok {
			t.Fatalf("expected mapping for %q to exist", from)
		}
		if got != want {
			t.Fatalf("unexpected mapping for %q: got %q want %q", from, got, want)
		}
	}
}


	if len(DefaultKiroModelMapping) != len(expected) {
		t.Fatalf("expected %d Kiro mappings, got %d", len(expected), len(DefaultKiroModelMapping))
	}
	for model, want := range expected {
		if got := DefaultKiroModelMapping[model]; got != want {
			t.Fatalf("unexpected Kiro mapping for %q: got %q want %q", model, got, want)
		}
	}

	for _, model := range []string{
		"claude-opus-4-5",
		"claude-sonnet-4-5",
		"claude-sonnet-4",
		"claude-3-5-sonnet-20241022",
		"claude-3-5-haiku-20241022",
		"gpt-4o",
		"gpt-4",
		"gpt-5.6",
		"deepseek-3-2",
		"minimax-m2-1",
		"qwen3-coder-next",
		"claude-sonnet-4-6-chat",
	} {
		if _, ok := DefaultKiroModelMapping[model]; ok {
			t.Fatalf("did not expect %q to remain in DefaultKiroModelMapping", model)
		}
	}
	for model := range DefaultKiroModelMapping {
		if strings.HasSuffix(model, "-agentic") {
			t.Fatalf("did not expect agentic Kiro mapping %q", model)
		}
		if strings.HasSuffix(model, "-chat") {
			t.Fatalf("did not expect chat-only Kiro mapping %q", model)
		}
	}
}

	for from, want := range cases {
		got, ok := DefaultAntigravityModelMapping[from]
		if !ok {
			t.Fatalf("expected mapping for %q to exist", from)
		}
		if got != want {
			t.Fatalf("unexpected mapping for %q: got %q want %q", from, got, want)
		}
	}
}

	for model, want := range cases {
		if got := DefaultAntigravityModelMapping[model]; got != want {
			t.Fatalf("expected model %q to map to %q, got %q", model, want, got)
		}
	}
}


	for from, want := range cases {
		got, ok := DefaultAntigravityModelMapping[from]
		if !ok {
			t.Fatalf("expected mapping for %q to exist", from)
		}
		if got != want {
			t.Fatalf("unexpected mapping for %q: got %q want %q", from, got, want)
		}
	}
}

	}
}

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
