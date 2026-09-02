package service

import "github.com/Wei-Shaw/sub2api/internal/modelcatalog"

func tokenRatesToModelPricing(rates modelcatalog.TokenRates) *ModelPricing {
	return &ModelPricing{
		InputPricePerToken:                 rates.Input,
		OutputPricePerToken:                rates.Output,
		InputPricePerTokenPriority:         rates.InputPriority,
		OutputPricePerTokenPriority:        rates.OutputPriority,
		CacheCreationPricePerToken:         rates.CacheWrite,
		CacheCreationPricePerTokenPriority: rates.CacheWritePriority,
		CacheReadPricePerToken:             rates.CacheRead,
		CacheReadPricePerTokenPriority:     rates.CacheReadPriority,
		ImageInputPricePerToken:            rates.ImageInput,
		ImageOutputPricePerToken:           rates.ImageOutput,
		LongContextInputThreshold:          rates.LongContextInputThreshold,
		LongContextInputMultiplier:         rates.LongContextInputMultiplier,
		LongContextOutputMultiplier:        rates.LongContextOutputMultiplier,
		LongContextThresholdInclusive:      rates.LongContextThresholdInclusive,
	}
}

func tokenRatesToLiteLLMPricing(rates modelcatalog.TokenRates) *LiteLLMModelPricing {
	return &LiteLLMModelPricing{
		InputCostPerToken:                   rates.Input,
		OutputCostPerToken:                  rates.Output,
		InputCostPerTokenPriority:           rates.InputPriority,
		OutputCostPerTokenPriority:          rates.OutputPriority,
		CacheCreationInputTokenCost:         rates.CacheWrite,
		CacheCreationInputTokenCostPriority: rates.CacheWritePriority,
		CacheReadInputTokenCost:             rates.CacheRead,
		CacheReadInputTokenCostPriority:     rates.CacheReadPriority,
		InputCostPerImageToken:              rates.ImageInput,
		OutputCostPerImageToken:             rates.ImageOutput,
		LongContextInputTokenThreshold:      rates.LongContextInputThreshold,
		LongContextInputCostMultiplier:      rates.LongContextInputMultiplier,
		LongContextOutputCostMultiplier:     rates.LongContextOutputMultiplier,
		LongContextThresholdInclusive:       rates.LongContextThresholdInclusive,
		Mode:                                "chat",
	}
}

func overlayModelPricingFromCatalog(dst *ModelPricing, price *modelcatalog.Price) {
	if dst == nil || price == nil {
		return
	}
	if price.InputPerMTok != nil {
		dst.InputPricePerToken = modelcatalog.PerToken(*price.InputPerMTok)
	}
	if price.OutputPerMTok != nil {
		dst.OutputPricePerToken = modelcatalog.PerToken(*price.OutputPerMTok)
	}
	if price.InputPriorityPerMTok != nil {
		dst.InputPricePerTokenPriority = modelcatalog.PerToken(*price.InputPriorityPerMTok)
	}
	if price.OutputPriorityPerMTok != nil {
		dst.OutputPricePerTokenPriority = modelcatalog.PerToken(*price.OutputPriorityPerMTok)
	}
	if price.CacheWritePerMTok != nil {
		dst.CacheCreationPricePerToken = modelcatalog.PerToken(*price.CacheWritePerMTok)
	}
	if price.CacheWritePriorityPerMTok != nil {
		dst.CacheCreationPricePerTokenPriority = modelcatalog.PerToken(*price.CacheWritePriorityPerMTok)
	}
	if price.CacheReadPerMTok != nil {
		dst.CacheReadPricePerToken = modelcatalog.PerToken(*price.CacheReadPerMTok)
	}
	if price.CacheReadPriorityPerMTok != nil {
		dst.CacheReadPricePerTokenPriority = modelcatalog.PerToken(*price.CacheReadPriorityPerMTok)
	}
	if price.ImageInputPerMTok != nil {
		dst.ImageInputPricePerToken = modelcatalog.PerToken(*price.ImageInputPerMTok)
	}
	if price.ImageOutputPerMTok != nil {
		dst.ImageOutputPricePerToken = modelcatalog.PerToken(*price.ImageOutputPerMTok)
	}
	if price.LongContextInputThreshold > 0 {
		dst.LongContextInputThreshold = price.LongContextInputThreshold
	}
	if price.LongContextInputMultiplier != 0 {
		dst.LongContextInputMultiplier = price.LongContextInputMultiplier
	}
	if price.LongContextOutputMultiplier != 0 {
		dst.LongContextOutputMultiplier = price.LongContextOutputMultiplier
	}
	if price.LongContextThresholdInclusive {
		dst.LongContextThresholdInclusive = true
	}
}

func overlayLiteLLMFromCatalog(dst *LiteLLMModelPricing, price *modelcatalog.Price) {
	if dst == nil || price == nil {
		return
	}
	if price.InputPerMTok != nil {
		dst.InputCostPerToken = modelcatalog.PerToken(*price.InputPerMTok)
	}
	if price.OutputPerMTok != nil {
		dst.OutputCostPerToken = modelcatalog.PerToken(*price.OutputPerMTok)
	}
	if price.InputPriorityPerMTok != nil {
		dst.InputCostPerTokenPriority = modelcatalog.PerToken(*price.InputPriorityPerMTok)
	}
	if price.OutputPriorityPerMTok != nil {
		dst.OutputCostPerTokenPriority = modelcatalog.PerToken(*price.OutputPriorityPerMTok)
	}
	if price.CacheWritePerMTok != nil {
		dst.CacheCreationInputTokenCost = modelcatalog.PerToken(*price.CacheWritePerMTok)
	}
	if price.CacheWritePriorityPerMTok != nil {
		dst.CacheCreationInputTokenCostPriority = modelcatalog.PerToken(*price.CacheWritePriorityPerMTok)
	}
	if price.CacheReadPerMTok != nil {
		dst.CacheReadInputTokenCost = modelcatalog.PerToken(*price.CacheReadPerMTok)
	}
	if price.CacheReadPriorityPerMTok != nil {
		dst.CacheReadInputTokenCostPriority = modelcatalog.PerToken(*price.CacheReadPriorityPerMTok)
	}
	if price.ImageInputPerMTok != nil {
		dst.InputCostPerImageToken = modelcatalog.PerToken(*price.ImageInputPerMTok)
	}
	if price.ImageOutputPerMTok != nil {
		dst.OutputCostPerImageToken = modelcatalog.PerToken(*price.ImageOutputPerMTok)
	}
	if price.LongContextInputThreshold > 0 {
		dst.LongContextInputTokenThreshold = price.LongContextInputThreshold
	}
	if price.LongContextInputMultiplier != 0 {
		dst.LongContextInputCostMultiplier = price.LongContextInputMultiplier
	}
	if price.LongContextOutputMultiplier != 0 {
		dst.LongContextOutputCostMultiplier = price.LongContextOutputMultiplier
	}
	if price.LongContextThresholdInclusive {
		dst.LongContextThresholdInclusive = true
	}
}

func catalogShouldWriteFallback(entry *modelcatalog.Entry) bool {
	if entry == nil || entry.Price == nil {
		return false
	}
	return entry.IsCanonical() || modelcatalog.SharedRateCardID(entry.ID) != ""
}

// catalogFallbackPricing 从当前生效目录（原子指针）惰性解析 baseline 价。
// 硬编码回退 map 在构造后保持不变；目录条目按请求解析，因此目录热换入
// （本地热加载/仓库远程同步）对计费回退立即可见，无需重建 map 或重启。
func catalogFallbackPricing(model string) *ModelPricing {
	cat := modelcatalog.Current()
	if cat == nil {
		return nil
	}
	// 思考档变体（-high/-low/-medium/-tiered）共享基础卡
	if card := cat.SharedRateCardID(model); card != "" && card != model {
		entry := cat.Lookup(card)
		if entry == nil || entry.Price == nil {
			return nil
		}
		return tokenRatesToModelPricing(entry.Rates())
	}
	entry := cat.Lookup(model)
	if entry == nil || entry.Price == nil || !catalogShouldWriteFallback(entry) {
		return nil
	}
	return tokenRatesToModelPricing(entry.Rates())
}

// lookupExactFallbackPricing 精确名回退（不走系列启发式）：
//  1. 硬编码回退 map 命中：目录有 lock_price 卡时克隆后覆盖（最强覆盖，
//     不改共享指针），否则原样返回；
//  2. map 未命中：从当前生效目录惰性解析 baseline（含思考档共享卡）。
func (s *BillingService) lookupExactFallbackPricing(model string) *ModelPricing {
	if s == nil {
		return nil
	}
	if s.fallbackPrices != nil {
		if pricing, ok := s.fallbackPrices[model]; ok && pricing != nil {
			if cat := modelcatalog.Current(); cat != nil {
				if entry := cat.Lookup(model); entry != nil && entry.LockPrice && entry.Price != nil {
					cloned := *pricing
					overlayModelPricingFromCatalog(&cloned, entry.Price)
					return &cloned
				}
			}
			return pricing
		}
	}
	return catalogFallbackPricing(model)
}
