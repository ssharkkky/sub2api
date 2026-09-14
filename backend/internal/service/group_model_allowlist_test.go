package service

import (
	"strings"
	"testing"
)

func TestNormalizeGroupModelAllowlist(t *testing.T) {
	tests := []struct {
		name    string
		in      GroupModelAllowlist
		want    GroupModelAllowlist
		wantErr string
	}{
		{
			name: "trims and dedupes case-insensitively preserving order",
			in: GroupModelAllowlist{
				Enabled: true,
				Models:  []string{" gpt-5.4 ", "GPT-5.4", "claude-sonnet-4.5", ""},
			},
			want: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4", "claude-sonnet-4.5"}},
		},
		{
			name: "disabled with empty list is fine",
			in:   GroupModelAllowlist{Enabled: false},
			want: GroupModelAllowlist{Enabled: false},
		},
		{
			name:    "enabled with empty list is rejected",
			in:      GroupModelAllowlist{Enabled: true},
			wantErr: "INVALID_MODEL_ALLOWLIST",
		},
		{
			name:    "enabled with only blank entries is rejected",
			in:      GroupModelAllowlist{Enabled: true, Models: []string{" ", ""}},
			wantErr: "INVALID_MODEL_ALLOWLIST",
		},
		{
			name: "trailing wildcard is accepted",
			in:   GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.5-*"}},
			want: GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.5-*"}},
		},
		{
			name:    "wildcard in the middle is rejected",
			in:      GroupModelAllowlist{Enabled: true, Models: []string{"gpt-*-5.4"}},
			wantErr: "INVALID_MODEL_ALLOWLIST",
		},
		{
			name:    "bare wildcard is accepted as allow-all",
			in:      GroupModelAllowlist{Enabled: true, Models: []string{"*"}},
			want:    GroupModelAllowlist{Enabled: true, Models: []string{"*"}},
			wantErr: "",
		},
		{
			name:    "disabled config with invalid wildcard still rejected",
			in:      GroupModelAllowlist{Enabled: false, Models: []string{"foo-*bar"}},
			wantErr: "INVALID_MODEL_ALLOWLIST",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := normalizeGroupModelAllowlist(tt.in)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Enabled != tt.want.Enabled {
				t.Fatalf("enabled mismatch: got %v want %v", got.Enabled, tt.want.Enabled)
			}
			if len(got.Models) != len(tt.want.Models) {
				t.Fatalf("models mismatch: got %#v want %#v", got.Models, tt.want.Models)
			}
			for i := range tt.want.Models {
				if got.Models[i] != tt.want.Models[i] {
					t.Fatalf("models[%d] mismatch: got %q want %q", i, got.Models[i], tt.want.Models[i])
				}
			}
		})
	}
}

func TestGroupModelAllowlistAllows(t *testing.T) {
	// R3：白名单不再是独立约束源，Allows 恒放行。
	for _, tc := range []struct{ name, model string }{
		{"listed exact", "claude-sonnet-4.5"},
		{"unlisted", "claude-opus-4.6"},
		{"wildcard model", "grok-4.6"},
		{"empty", ""},
		{"models prefix", "models/gemini-2.5-pro"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allowlist := GroupModelAllowlist{Enabled: true, Models: []string{"claude-sonnet-4.5", "gpt-5.5", "grok-*"}}
			if !allowlist.Allows(tc.model) {
				t.Fatalf("Allows(%q) must be true (no-op gatekeeper)", tc.model)
			}
		})
	}
}

func TestGroupModelAllowlistEnabled(t *testing.T) {
	var nilGroup *Group
	if nilGroup.ModelAllowlistEnabled() {
		t.Fatal("nil group must not report enabled")
	}
	group := &Group{}
	if group.ModelAllowlistEnabled() {
		t.Fatal("default group must not report enabled")
	}
	group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{"m"}}
	if !group.ModelAllowlistEnabled() {
		t.Fatal("enabled group must report enabled")
	}
}

func TestGroupModelAllowlistFilterForListing(t *testing.T) {
	// R3：白名单不再过滤模型列表，FilterForListing 直接返回 source。
	source := []string{"claude-opus-4.6", "claude-sonnet-4.5", "gpt-5.4", "grok-4.6"}

	t.Run("returns source when enabled", func(t *testing.T) {
		cfg := GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4"}}
		if got := cfg.FilterForListing(source); len(got) != len(source) {
			t.Fatalf("FilterForListing must return source, got %#v", got)
		}
	})

	t.Run("returns source when disabled", func(t *testing.T) {
		cfg := GroupModelAllowlist{Enabled: false}
		if got := cfg.FilterForListing(source); len(got) != len(source) {
			t.Fatalf("FilterForListing must return source, got %#v", got)
		}
	})

	t.Run("returns nil for nil source", func(t *testing.T) {
		cfg := GroupModelAllowlist{Enabled: true, Models: []string{"gpt-5.4"}}
		if got := cfg.FilterForListing(nil); got != nil {
			t.Fatalf("FilterForListing(nil) must be nil, got %#v", got)
		}
	})
}
