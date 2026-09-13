package service

import (
	"context"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
)

// CatalogStorefrontModel is one checkbox candidate on the channel shelf.
type CatalogStorefrontModel struct {
	ID             string   `json:"id"`
	DisplayName    string   `json:"display_name"`
	CanonicalID    string   `json:"canonical_id"`
	Platforms      []string `json:"platforms"`
	BillingMode    string   `json:"billing_mode"`
	InputPrice     *float64 `json:"input_price,omitempty"`
	OutputPrice    *float64 `json:"output_price,omitempty"`
	CacheWrite     *float64 `json:"cache_write_price,omitempty"`
	CacheRead      *float64 `json:"cache_read_price,omitempty"`
	ImageInput     *float64 `json:"image_input_price,omitempty"`
	ImageOutput    *float64 `json:"image_output_price,omitempty"`
	PerRequest     *float64 `json:"per_request_price,omitempty"`
	CoverageHave   *int     `json:"coverage_have,omitempty"`
	CoverageTotal  *int     `json:"coverage_total,omitempty"`
	CoverageSynced *int     `json:"coverage_synced,omitempty"`
}

// ListCatalogStorefrontModels returns fork-owned catalog models for a platform.
func ListCatalogStorefrontModels(platform string) []CatalogStorefrontModel {
	items := modelcatalog.StorefrontItems(platform)
	out := make([]CatalogStorefrontModel, 0, len(items))
	for _, item := range items {
		entry := CatalogStorefrontModel{
			ID:          item.ID,
			DisplayName: item.DisplayName,
			CanonicalID: item.CanonicalID,
			Platforms:   item.Platforms,
			BillingMode: "token",
		}
		if item.Price != nil {
			entry.InputPrice = perTokenPtr(item.Price.InputPerMTok)
			entry.OutputPrice = perTokenPtr(item.Price.OutputPerMTok)
			entry.CacheWrite = perTokenPtr(item.Price.CacheWritePerMTok)
			entry.CacheRead = perTokenPtr(item.Price.CacheReadPerMTok)
			entry.ImageInput = perTokenPtr(item.Price.ImageInputPerMTok)
			entry.ImageOutput = perTokenPtr(item.Price.ImageOutputPerMTok)
			entry.PerRequest = item.Price.PerRequest
		}
		out = append(out, entry)
	}
	return out
}

func perTokenPtr(perMTok *float64) *float64 {
	if perMTok == nil {
		return nil
	}
	value := modelcatalog.PerToken(*perMTok)
	return &value
}

// ListStorefrontModels returns the models a bound channel is currently selling
// when model restriction is on. enabled=false means fall back to the old
// account-mapping / default list.
func (s *ChannelService) ListStorefrontModels(ctx context.Context, groupID int64, platform string) (models []string, enabled bool) {
	if s == nil {
		return nil, false
	}
	lk, err := s.lookupGroupChannel(ctx, groupID)
	if err != nil || lk == nil || lk.channel == nil || !lk.channel.RestrictModels {
		return nil, false
	}

	want := strings.ToLower(strings.TrimSpace(platform))
	seen := make(map[string]struct{})
	out := make([]string, 0)
	for _, pricing := range lk.channel.ModelPricing {
		if want != "" && strings.ToLower(strings.TrimSpace(pricing.Platform)) != want {
			continue
		}
		for _, model := range pricing.Models {
			model = strings.TrimSpace(model)
			if model == "" {
				continue
			}
			key := strings.ToLower(model)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, model)
		}
	}
	sort.Strings(out)
	return out, true
}

func (s *ChannelService) SetAccountRepository(accountRepo AccountRepository) {
	if s == nil {
		return
	}
	s.accountRepo = accountRepo
}

// ListCatalogStorefrontModelsWithCoverage returns the selectable model list
// for a channel being configured.
//
// When group IDs are provided and at least one bound account has synced
// upstream capability data, the selectable list is the union of those
// accounts' upstream model snapshots (group-scoped shelf): catalog-known
// models keep the catalog display name and price card, account-only models
// (e.g. provider-prefixed IDs) are surfaced without a price card. Models
// owned by other groups or channels on the same platform therefore do not
// leak into the picker.
//
// When no bound account has synced data, the full platform catalog is kept
// so a missing probe cannot leave the picker empty (same fallback semantics
// as HasSyncedUpstreamModel). Per-model coverage is always annotated from
// the bound accounts' snapshots.
func (s *ChannelService) ListCatalogStorefrontModelsWithCoverage(ctx context.Context, platform string, groupIDs []int64) []CatalogStorefrontModel {
	if s == nil || s.accountRepo == nil || len(groupIDs) == 0 {
		return ListCatalogStorefrontModels(platform)
	}
	accounts := make([]Account, 0)
	seenGroups := make(map[int64]struct{}, len(groupIDs))
	for _, groupID := range groupIDs {
		if groupID <= 0 {
			continue
		}
		if _, ok := seenGroups[groupID]; ok {
			continue
		}
		seenGroups[groupID] = struct{}{}
		groupAccounts, err := s.accountRepo.ListByGroup(ctx, groupID)
		if err != nil {
			continue
		}
		accounts = append(accounts, groupAccounts...)
	}
	boundAccounts := filterStorefrontCoverageAccounts(accounts, platform)
	catalog := ListCatalogStorefrontModels(platform)

	if union := storefrontModelUnion(boundAccounts); len(union) > 0 {
		return AnnotateCatalogStorefrontCoverage(storefrontModelsFromUnion(union, catalog, platform), boundAccounts)
	}
	return AnnotateCatalogStorefrontCoverage(catalog, boundAccounts)
}

// storefrontModelKey normalizes a model ID for union/matching: trimmed,
// lowercased, and stripped of the Google-style "models/" prefix. It mirrors
// modelcatalog normalization so account-snapshot models match catalog IDs.
func storefrontModelKey(model string) string {
	model = strings.ToLower(strings.TrimSpace(model))
	return strings.TrimPrefix(model, "models/")
}

// storefrontModelUnion returns the union of all non-empty upstream model
// snapshots across the given (already platform-filtered) accounts. Keys are
// normalized model IDs; values keep the first-seen original spelling for
// display when a model is not catalog-known. First-seen order is group
// order, then account order, then snapshot order (deterministic for
// identical inputs).
// storefrontModelUnion returns the union of all servable public names across the
// given (already platform-filtered) accounts. A servable public name is either an
// explicit mapping key (a public name the operator can request) or a native
// snapshot model (served via passthrough). Wildcard mapping keys are expanded
// against the account's synced snapshot, then the platform catalog.
func storefrontModelUnion(accounts []Account) map[string]string {
	union := make(map[string]string)
	for i := range accounts {
		acct := &accounts[i]
		// Native snapshot models (passthrough public names).
		if snapshot := acct.UpstreamModelSnapshot(); snapshot != nil {
			for _, model := range snapshot.Models {
				addToStorefrontUnion(union, model)
			}
		}
		// Explicit mapping keys (public names).
		if mapping := acct.GetModelMapping(); mapping != nil {
			keys := make([]string, 0, len(mapping))
			for key := range mapping {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			for _, key := range keys {
				addStorefrontMappingKey(union, acct, key)
			}
		}
	}
	return union
}

func addToStorefrontUnion(union map[string]string, model string) {
	key := storefrontModelKey(model)
	if key == "" {
		return
	}
	if _, ok := union[key]; !ok {
		union[key] = strings.TrimSpace(model)
	}
}

// addStorefrontMappingKey adds an explicit mapping key (a public name) to the
// picker union. Exact keys are added directly; wildcard keys are expanded into
// concrete public names (snapshot first, then platform catalog).
func addStorefrontMappingKey(union map[string]string, acct *Account, key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	if !strings.Contains(key, "*") {
		addToStorefrontUnion(union, key)
		return
	}
	for _, model := range expandWildcardForStorefront(acct, key) {
		addToStorefrontUnion(union, model)
	}
}

// expandWildcardForStorefront expands a wildcard mapping key into concrete
// public names: prefer the account's synced native snapshot, then the platform
// catalog defaults (PlatformDefaultModelIDs). If neither matches, nothing is
// added (no fabricated models; the operator can still price a specific model).
func expandWildcardForStorefront(acct *Account, pattern string) []string {
	if snapshot := acct.UpstreamModelSnapshot(); snapshot != nil && len(snapshot.Models) > 0 {
		if matched := matchWildcardAgainst(snapshot.Models, pattern); len(matched) > 0 {
			return matched
		}
	}
	return matchWildcardAgainst(PlatformDefaultModelIDs(acct.Platform), pattern)
}

// matchWildcardAgainst returns the models (from the given list) that match the
// wildcard pattern, deduplicated by normalized key, preserving list order.
func matchWildcardAgainst(models []string, pattern string) []string {
	out := make([]string, 0)
	seen := make(map[string]struct{})
	for _, model := range models {
		if !matchWildcard(pattern, model) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(model))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, strings.TrimSpace(model))
	}
	return out
}

// storefrontModelsFromUnion maps the bound-account model union onto picker
// candidates, sorted by normalized key. Union keys present in the platform
// catalog (canonical or alias) reuse the catalog candidate (display name,
// canonical ID, price card); everything else becomes a bare candidate whose
// ID keeps the original spelling so operators can price it directly.
func storefrontModelsFromUnion(union map[string]string, catalog []CatalogStorefrontModel, platform string) []CatalogStorefrontModel {
	byKey := make(map[string]CatalogStorefrontModel, len(catalog))
	for _, model := range catalog {
		key := storefrontModelKey(model.ID)
		if key == "" {
			continue
		}
		if _, ok := byKey[key]; !ok {
			byKey[key] = model
		}
	}

	keys := make([]string, 0, len(union))
	for key := range union {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	out := make([]CatalogStorefrontModel, 0, len(keys))
	for _, key := range keys {
		if model, ok := byKey[key]; ok {
			out = append(out, model)
			continue
		}
		original := union[key]
		entry := CatalogStorefrontModel{
			ID:          original,
			DisplayName: original,
			CanonicalID: original,
			BillingMode: "token",
		}
		if normalized := strings.ToLower(strings.TrimSpace(platform)); normalized != "" {
			entry.Platforms = []string{normalized}
		}
		out = append(out, entry)
	}
	return out
}

func filterStorefrontCoverageAccounts(accounts []Account, platform string) []Account {
	want := strings.ToLower(strings.TrimSpace(platform))
	if want == "" {
		return nil
	}
	out := make([]Account, 0, len(accounts))
	seen := make(map[int64]struct{}, len(accounts))
	for _, account := range accounts {
		if account.ID != 0 {
			if _, ok := seen[account.ID]; ok {
				continue
			}
			seen[account.ID] = struct{}{}
		}
		if account.IsShadow() {
			continue
		}
		if strings.ToLower(strings.TrimSpace(account.Platform)) != want {
			continue
		}
		out = append(out, account)
	}
	return out
}

// AnnotateCatalogStorefrontCoverage fills per-model account coverage. It never
// uses the intersection as the user-facing shelf.
//
// Coverage = how many bound accounts can PARTICIPATE IN ROUTING this public
// name, judged by evidence (storefrontAccountCoversModel): an account counts
// only if it has an explicit mapping hit OR a native snapshot hit (including the
// OpenAI OAuth vendor guard). Accounts with neither a mapping nor a snapshot are
// NOT counted (evidence-based, not fail-open). CoverageSynced separately reports
// the number of accounts that currently hold a valid snapshot.
func AnnotateCatalogStorefrontCoverage(models []CatalogStorefrontModel, accounts []Account) []CatalogStorefrontModel {
	total := len(accounts)
	synced := 0
	for i := range accounts {
		if snapshot := accounts[i].UpstreamModelSnapshot(); snapshot != nil && len(snapshot.Models) > 0 {
			synced++
		}
	}
	for i := range models {
		have := 0
		for j := range accounts {
			if storefrontAccountCoversModel(&accounts[j], models[i].ID) {
				have++
			}
		}
		haveCopy, totalCopy, syncedCopy := have, total, synced
		models[i].CoverageHave = &haveCopy
		models[i].CoverageTotal = &totalCopy
		models[i].CoverageSynced = &syncedCopy
	}
	return models
}

// storefrontAccountCoversModel reports whether the account has EVIDENCE it can
// serve the public name for storefront coverage: an explicit mapping key hit
// (exact or wildcard), or a synced snapshot that covers the model (case/
// models/-tolerant match, or the model's mapped upstream name present in the
// snapshot). Unsynced accounts without a matching mapping key do not count, so
// coverage never pretends an unprobed account has a model it may not.
func storefrontAccountCoversModel(acct *Account, model string) bool {
	if acct == nil {
		return false
	}
	if mapping := acct.GetModelMapping(); mapping != nil && mappingSupportsRequestedModel(mapping, model) {
		return true
	}
	snapshot := acct.UpstreamModelSnapshot()
	if snapshot == nil || len(snapshot.Models) == 0 {
		return false
	}
	if key := storefrontModelKey(model); key != "" {
		for _, m := range snapshot.Models {
			if storefrontModelKey(m) == key {
				return true
			}
		}
	}
	return snapshotCoversRequestedModel(acct, snapshot.Models, model)
}
