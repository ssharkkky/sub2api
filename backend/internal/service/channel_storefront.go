package service

import (
	"context"
	"sort"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/modelcatalog"
)

// CatalogStorefrontModel is one checkbox candidate on the channel shelf.
type CatalogStorefrontModel struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	CanonicalID string   `json:"canonical_id"`
	Platforms   []string `json:"platforms"`
	BillingMode string   `json:"billing_mode"`
	InputPrice  *float64 `json:"input_price,omitempty"`
	OutputPrice *float64 `json:"output_price,omitempty"`
	CacheWrite  *float64 `json:"cache_write_price,omitempty"`
	CacheRead   *float64 `json:"cache_read_price,omitempty"`
	ImageInput  *float64 `json:"image_input_price,omitempty"`
	ImageOutput *float64 `json:"image_output_price,omitempty"`
	PerRequest  *float64 `json:"per_request_price,omitempty"`
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
