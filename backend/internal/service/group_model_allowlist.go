package service

import (
	"net/http"
	"slices"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// GroupModelAllowlist 是 service 层的分组模型白名单（与 domain.GroupModelAllowlist
// 字段一致，ent 持久化用 domain 类型，边界处显式转换）。
type GroupModelAllowlist struct {
	Enabled bool     `json:"enabled"`
	Models  []string `json:"models,omitempty"`
}

// DomainGroupModelAllowlist 把 service 白名单转换为 ent 持久化使用的 domain 类型。
func DomainGroupModelAllowlist(cfg GroupModelAllowlist) domain.GroupModelAllowlist {
	return domain.GroupModelAllowlist{Enabled: cfg.Enabled, Models: cfg.Models}
}

// GroupModelAllowlistFromDomain 把 ent 读出的 domain 白名单转换为 service 类型。
func GroupModelAllowlistFromDomain(cfg domain.GroupModelAllowlist) GroupModelAllowlist {
	return GroupModelAllowlist{Enabled: cfg.Enabled, Models: cfg.Models}
}

// supplementUnmappedOpenAIModels ensures a partial mapping catalog does not
// hide models from unmapped OpenAI accounts. An empty catalog is left unchanged
// so callers retain their existing discovery fallback.
func supplementUnmappedOpenAIModels(accounts []Account, models []string) []string {
	if len(models) == 0 {
		return models
	}
	for i := range accounts {
		account := &accounts[i]
		if account.Platform == PlatformOpenAI && len(account.GetModelMapping()) == 0 {
			return dedupeAndSortModelIDs(slices.Concat(models, openai.DefaultModelIDs()))
		}
	}
	return models
}

// normalizeGroupModelAllowlist 归一化管理端提交的分组模型白名单：
// 条目 TrimSpace、按小写去重保序；`*` 只允许出现在条目末尾；
// enabled=true 且列表为空视为配置错误，返回 400 而不是运行时静默放行/拒绝。
func normalizeGroupModelAllowlist(cfg GroupModelAllowlist) (GroupModelAllowlist, error) {
	out := GroupModelAllowlist{Enabled: cfg.Enabled}
	if len(cfg.Models) == 0 {
		if out.Enabled {
			return out, infraerrors.New(http.StatusBadRequest, "INVALID_MODEL_ALLOWLIST", "model allowlist cannot be enabled with an empty model list")
		}
		return out, nil
	}

	seen := make(map[string]struct{}, len(cfg.Models))
	out.Models = make([]string, 0, len(cfg.Models))
	for _, model := range cfg.Models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if strings.Contains(strings.TrimSuffix(model, "*"), "*") {
			return out, infraerrors.New(http.StatusBadRequest, "INVALID_MODEL_ALLOWLIST", `wildcard "*" is only allowed at the end of an allowlist entry`)
		}
		key := strings.ToLower(model)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out.Models = append(out.Models, model)
	}
	if len(out.Models) == 0 {
		if out.Enabled {
			return out, infraerrors.New(http.StatusBadRequest, "INVALID_MODEL_ALLOWLIST", "model allowlist cannot be enabled with an empty model list")
		}
		out.Models = nil
	}
	return out, nil
}

// ModelAllowlistEnabled 报告该分组是否启用了模型白名单。
// 开启后所有携带模型的网关请求与模型列表接口都受白名单约束。
func (g *Group) ModelAllowlistEnabled() bool {
	return g != nil && g.ModelAllowlist.Enabled
}

// Allows 判断某个请求模型名是否被该分组放行。随 R3 删除：分组模型白名单不再作为
// 独立用户可见约束源（渠道定价是唯一约束），请求门禁已移除，恒放行。
func (a GroupModelAllowlist) Allows(model string) bool {
	return true
}


// FilterForListing 已随 R3 删除：分组模型白名单不再作为独立用户可见约束源，
// 模型列表不再按白名单过滤（渠道定价是唯一约束）。保留方法以兼容既有调用点，
// 直接返回 source（无过滤）。
func (a GroupModelAllowlist) FilterForListing(source []string) []string {
	return source
}

