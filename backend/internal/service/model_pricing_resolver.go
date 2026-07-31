package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// PricingSource 定价来源标识
const (
	PricingSourceChannel    = "channel"
	PricingSourceModelPrice = "model_price"
	PricingSourceFallback   = "fallback"
)

// ResolvedPricing 统一定价解析结果
type ResolvedPricing struct {
	// Mode 计费模式
	Mode BillingMode

	// Token 模式：基础定价（来自配置化模型价格目录或 fallback）
	BasePricing *ModelPricing

	// Token 模式：区间定价列表（如有，覆盖 BasePricing 中的对应字段）
	Intervals []PricingInterval

	// 按次/图片模式：分层定价
	RequestTiers []PricingInterval

	// 按次/图片模式：默认价格（未命中层级时使用）
	DefaultPerRequestPrice    float64
	DefaultPerRequestPriceSet bool

	// 来源标识
	Source string // "channel", "model_price", "fallback"

	// 是否支持缓存细分
	SupportsCacheBreakdown bool

	// 渠道定价原始配置（用于区间模式下获取 ImageOutputPrice）
	channelPricing *ChannelModelPricing
}

// ModelPricingResolver 统一模型定价解析器。
// 解析链：Channel → configured pricing catalog → Fallback。
type ModelPricingResolver struct {
	channelService *ChannelService
	billingService *BillingService
}

// NewModelPricingResolver 创建定价解析器实例
func NewModelPricingResolver(channelService *ChannelService, billingService *BillingService) *ModelPricingResolver {
	return &ModelPricingResolver{
		channelService: channelService,
		billingService: billingService,
	}
}

// PricingInput 定价解析输入
type PricingInput struct {
	Model   string
	GroupID *int64 // nil 表示不检查渠道
}

// Resolve 解析模型定价。
// 1. 获取基础定价（configured pricing catalog → Fallback）
// 2. 如果指定了 GroupID，查找渠道定价并覆盖
func (r *ModelPricingResolver) Resolve(ctx context.Context, input PricingInput) *ResolvedPricing {
	resolved, _ := r.resolve(ctx, input, false)
	return resolved
}

// ResolveStrictToken 为已经发生上游成本的 token 用量解析可结算价格。
//
// 与 Resolve 的差别只在价格证据：全局价格必须严格命中候选模型自己的条目，
// 不能通过 family/OpenAI 模糊回退借用另一个 SKU 的价格。若严格全局价格缺失，
// 只有完整、显式的渠道价格才能独立构成结算依据；部分渠道覆盖仍可叠加在严格
// 全局价格之上。
func (r *ModelPricingResolver) ResolveStrictToken(ctx context.Context, input PricingInput) (*ResolvedPricing, error) {
	return r.resolve(ctx, input, true)
}

// ResolveStrictImageToken resolves the exact image-token dimensions used by
// /v1/images/*. Unlike ResolveStrictToken, it permits catalog entries that omit
// ordinary text-output pricing when a dedicated image-output price exists.
func (r *ModelPricingResolver) ResolveStrictImageToken(
	ctx context.Context,
	input PricingInput,
	requireImageInput bool,
) (*ResolvedPricing, error) {
	if r == nil || r.billingService == nil {
		return nil, fmt.Errorf(
			"%w for image model: %s: pricing resolver unavailable",
			ErrModelPricingUnavailable,
			input.Model,
		)
	}
	var channelPricing *ChannelModelPricing
	if input.GroupID != nil && r.channelService != nil {
		channelPricing = r.channelService.GetChannelModelPricing(ctx, *input.GroupID, input.Model)
		if channelPricing != nil {
			mode := channelPricing.BillingMode
			if mode != "" && mode != BillingModeToken {
				return nil, fmt.Errorf(
					"%w for image model: %s: channel billing mode is %s",
					ErrModelPricingUnavailable,
					input.Model,
					mode,
				)
			}
		}
	}

	basePricing, baseErr := r.billingService.GetImageTokenPricingStrict(input.Model, requireImageInput)
	if baseErr != nil && !channelImageTokenPricingConfigured(channelPricing) {
		return nil, baseErr
	}
	resolved := &ResolvedPricing{
		Mode:                   BillingModeToken,
		BasePricing:            basePricing,
		Source:                 PricingSourceModelPrice,
		SupportsCacheBreakdown: basePricing != nil && basePricing.SupportsCacheBreakdown,
	}
	if channelPricing != nil {
		resolved.Source = PricingSourceChannel
		resolved.channelPricing = channelPricing
		preserveImageOutput := basePricing != nil && channelPricing.OutputPrice == nil && channelPricing.ImageOutputPrice == nil
		preserveImageInput := basePricing != nil && channelPricing.InputPrice == nil && channelPricing.ImageInputPrice == nil
		var imageOutputPrice float64
		var imageOutputExplicit bool
		var imageInputPrice float64
		var imageInputExplicit bool
		if basePricing != nil {
			imageOutputPrice = basePricing.ImageOutputPricePerToken
			imageOutputExplicit = basePricing.ImageOutputPriceExplicit
			imageInputPrice = basePricing.ImageInputPricePerToken
			imageInputExplicit = basePricing.ImageInputPriceExplicit
		}
		r.applyTokenOverrides(channelPricing, resolved)
		if preserveImageOutput {
			resolved.BasePricing.ImageOutputPricePerToken = imageOutputPrice
			resolved.BasePricing.ImageOutputPriceExplicit = imageOutputExplicit
		}
		if preserveImageInput {
			resolved.BasePricing.ImageInputPricePerToken = imageInputPrice
			resolved.BasePricing.ImageInputPriceExplicit = imageInputExplicit
		}
	}
	if !openAIImageTokenPricingComplete(resolved.BasePricing, requireImageInput) {
		return nil, fmt.Errorf(
			"%w for image token model: %s: required image dimensions are incomplete",
			ErrModelPricingUnavailable,
			input.Model,
		)
	}
	if err := validateFiniteModelPricing(input.Model, resolved.BasePricing); err != nil {
		return nil, err
	}
	return resolved, nil
}

func (r *ModelPricingResolver) resolve(
	ctx context.Context,
	input PricingInput,
	strictTokenPricing bool,
) (*ResolvedPricing, error) {
	if r == nil {
		return nil, fmt.Errorf(
			"%w for model: %s: pricing resolver unavailable",
			ErrModelPricingUnavailable,
			input.Model,
		)
	}
	var chPricing *ChannelModelPricing
	if input.GroupID != nil && r.channelService != nil {
		chPricing = r.channelService.GetChannelModelPricing(ctx, *input.GroupID, input.Model)
		if chPricing != nil {
			mode := chPricing.BillingMode
			if mode == "" {
				mode = BillingModeToken
			}
			if mode == BillingModePerRequest || mode == BillingModeImage {
				if strictTokenPricing && !validConfiguredPrice(chPricing.PerRequestPrice) {
					return nil, fmt.Errorf(
						"%w for model: %s: incomplete explicit channel pricing",
						ErrModelPricingUnavailable,
						input.Model,
					)
				}
				resolved := &ResolvedPricing{
					Mode:           mode,
					Source:         PricingSourceChannel,
					channelPricing: chPricing,
				}
				r.applyRequestTierOverrides(chPricing, resolved)
				return resolved, nil
			}
		}
	}

	// 1. 获取基础定价
	basePricing, source, baseErr := r.resolveBasePricing(input.Model, strictTokenPricing)

	resolved := &ResolvedPricing{
		Mode:                   BillingModeToken,
		BasePricing:            basePricing,
		Source:                 source,
		SupportsCacheBreakdown: basePricing != nil && basePricing.SupportsCacheBreakdown,
	}

	// 2. 如果有 GroupID，尝试渠道覆盖
	if chPricing != nil {
		resolved.Source = PricingSourceChannel
		resolved.channelPricing = chPricing
		if strictTokenPricing && basePricing == nil && !channelTokenPricingConfigured(chPricing) {
			return nil, fmt.Errorf(
				"%w for model: %s: strict global pricing unavailable and explicit channel pricing is incomplete",
				ErrModelPricingUnavailable,
				input.Model,
			)
		}
		r.applyTokenOverrides(chPricing, resolved)
	} else if !strictTokenPricing && input.GroupID != nil && r.channelService != nil {
		r.applyChannelOverrides(ctx, *input.GroupID, input.Model, resolved)
	}

	if strictTokenPricing && basePricing == nil && chPricing == nil {
		return nil, baseErr
	}
	return resolved, nil
}

// resolveBasePricing 从配置化模型价格目录或 fallback 获取基础定价。
func (r *ModelPricingResolver) resolveBasePricing(model string, strict bool) (*ModelPricing, string, error) {
	if r == nil || r.billingService == nil {
		return nil, PricingSourceFallback, fmt.Errorf(
			"%w for model: %s: billing service unavailable",
			ErrModelPricingUnavailable,
			model,
		)
	}
	var (
		pricing *ModelPricing
		err     error
	)
	if strict {
		pricing, err = r.billingService.GetModelPricingStrict(model)
	} else {
		pricing, err = r.billingService.GetModelPricing(model)
	}
	if err != nil {
		slog.Debug("failed to get model pricing",
			"model", model, "strict", strict, "error", err)
		return nil, PricingSourceFallback, err
	}
	return pricing, PricingSourceModelPrice, nil
}

// applyChannelOverrides 应用渠道定价覆盖
func (r *ModelPricingResolver) applyChannelOverrides(ctx context.Context, groupID int64, model string, resolved *ResolvedPricing) {
	chPricing := r.channelService.GetChannelModelPricing(ctx, groupID, model)
	if chPricing == nil {
		return
	}

	resolved.Source = PricingSourceChannel
	resolved.channelPricing = chPricing
	resolved.Mode = chPricing.BillingMode
	if resolved.Mode == "" {
		resolved.Mode = BillingModeToken
	}

	switch resolved.Mode {
	case BillingModeToken:
		r.applyTokenOverrides(chPricing, resolved)
	case BillingModePerRequest, BillingModeImage:
		r.applyRequestTierOverrides(chPricing, resolved)
	}
}

// applyTokenOverrides 应用 token 模式的渠道覆盖
func (r *ModelPricingResolver) applyTokenOverrides(chPricing *ChannelModelPricing, resolved *ResolvedPricing) {
	// Token 区间只接受 token 价格字段。PerRequestPrice 属于按次/图片模式，
	// 不能让它创建一个命中后所有 token 单价均为 0 的区间。
	resolved.Intervals = filterValidTokenIntervals(chPricing.Intervals)

	// 覆盖顺序固定为：全局基础价 -> 渠道 flat -> 渠道 interval。
	// 即使存在 interval，flat 仍是区间缺口的安全 fallback，interval 中未配置的
	// 字段也会继承这里的结果。
	if resolved.BasePricing == nil {
		resolved.BasePricing = &ModelPricing{}
	} else {
		// 防止修改 fallbackPrices 中的共享指针
		cloned := *resolved.BasePricing
		resolved.BasePricing = &cloned
	}

	if chPricing.InputPrice != nil {
		resolved.BasePricing.InputPricePerToken = *chPricing.InputPrice
		resolved.BasePricing.InputPriceExplicit = true
		resolved.BasePricing.InputPricePerTokenPriority = *chPricing.InputPrice
		resolved.BasePricing.InputPriorityPriceExplicit = true
	}
	if chPricing.OutputPrice != nil {
		resolved.BasePricing.OutputPricePerToken = *chPricing.OutputPrice
		resolved.BasePricing.OutputPriceExplicit = true
		resolved.BasePricing.OutputPricePerTokenPriority = *chPricing.OutputPrice
		resolved.BasePricing.OutputPriorityPriceExplicit = true
	}
	if chPricing.CacheWritePrice != nil {
		resolved.BasePricing.CacheCreationPricePerToken = *chPricing.CacheWritePrice
		resolved.BasePricing.CacheCreationPricePerTokenPriority = *chPricing.CacheWritePrice
		resolved.BasePricing.CacheCreationPriceExplicit = true
		resolved.BasePricing.CacheCreationPriorityPriceExplicit = true
		resolved.BasePricing.CacheCreation5mPrice = *chPricing.CacheWritePrice
		resolved.BasePricing.CacheCreation1hPrice = *chPricing.CacheWritePrice
		resolved.BasePricing.CacheCreation1hPriceExplicit = true
	}
	if chPricing.CacheReadPrice != nil {
		resolved.BasePricing.CacheReadPricePerToken = *chPricing.CacheReadPrice
		resolved.BasePricing.CacheReadPricePerTokenPriority = *chPricing.CacheReadPrice
		resolved.BasePricing.CacheReadPriceExplicit = true
		resolved.BasePricing.CacheReadPriorityPriceExplicit = true
	}
	applyChannelImagePrices(chPricing, resolved.BasePricing)
}

// applyChannelImagePrices 应用渠道图片 token 价格。
// ImageOutputPrice=nil 表示未配置，必须回退到当前文本 output 价；只有显式
// 配置为 0 才表示图片输出免费。先清掉基础目录中的图片价，避免渠道已覆盖
// 文本 output 后仍意外沿用全局图片价。
func applyChannelImagePrices(chPricing *ChannelModelPricing, pricing *ModelPricing) {
	if chPricing != nil && chPricing.ImageOutputPrice != nil {
		pricing.ImageOutputPricePerToken = *chPricing.ImageOutputPrice
		pricing.ImageOutputPriceExplicit = true
	} else {
		pricing.ImageOutputPricePerToken = 0
		pricing.ImageOutputPriceExplicit = false
	}
	applyChannelImageInputPrice(chPricing, pricing)
}

// applyChannelImageInputPrice 应用渠道图片输入价：显式配置则用配置值；
// 未配置时归零，使 computeTokenBreakdown 回退到文本输入价（向后兼容，
// 避免 commit 引入的 LiteLLM 图片输入价泄漏进渠道自定义定价）。
// 与 image_output 不同，此处不设 Explicit 标志——图片输入未配置应回退文本价，
// 而非硬置 0。
func applyChannelImageInputPrice(chPricing *ChannelModelPricing, pricing *ModelPricing) {
	if chPricing != nil && chPricing.ImageInputPrice != nil {
		pricing.ImageInputPricePerToken = *chPricing.ImageInputPrice
		pricing.ImageInputPriceExplicit = true
	} else {
		pricing.ImageInputPricePerToken = 0
		pricing.ImageInputPriceExplicit = false
	}
}

// applyRequestTierOverrides 应用按次/图片模式的渠道覆盖
func (r *ModelPricingResolver) applyRequestTierOverrides(chPricing *ChannelModelPricing, resolved *ResolvedPricing) {
	resolved.RequestTiers = filterValidRequestTiers(chPricing.Intervals)
	if chPricing.PerRequestPrice != nil {
		resolved.DefaultPerRequestPrice = *chPricing.PerRequestPrice
		resolved.DefaultPerRequestPriceSet = true
	}
}

// filterValidTokenIntervals 仅保留包含 token 价格字段的区间。
func filterValidTokenIntervals(intervals []PricingInterval) []PricingInterval {
	var valid []PricingInterval
	for _, iv := range intervals {
		if iv.InputPrice != nil || iv.OutputPrice != nil ||
			iv.CacheWritePrice != nil || iv.CacheReadPrice != nil {
			valid = append(valid, iv)
		}
	}
	return valid
}

// filterValidRequestTiers 仅保留按次价格已显式配置的层级。
func filterValidRequestTiers(intervals []PricingInterval) []PricingInterval {
	var valid []PricingInterval
	for _, iv := range intervals {
		if iv.PerRequestPrice != nil {
			valid = append(valid, iv)
		}
	}
	return valid
}

// GetIntervalPricing 根据 context token 数获取区间定价。
// 如果有区间列表，找到匹配区间并构造 ModelPricing；否则直接返回 BasePricing。
func (r *ModelPricingResolver) GetIntervalPricing(resolved *ResolvedPricing, totalContextTokens int) *ModelPricing {
	if len(resolved.Intervals) == 0 {
		return resolved.BasePricing
	}

	lookupTokens := totalContextTokens
	if lookupTokens <= 0 {
		// FindMatchingInterval 使用 (min,max] 语义。上游只返回 output usage
		// 时 context 为 0；将其放入最低档，避免 interval-only 未知模型回退到空价。
		lookupTokens = 1
	}
	iv := FindMatchingInterval(resolved.Intervals, lookupTokens)
	if iv == nil {
		return resolved.BasePricing
	}

	return intervalToModelPricing(iv, resolved.BasePricing, resolved.SupportsCacheBreakdown, resolved.channelPricing)
}

// intervalToModelPricing 将区间定价转换为 ModelPricing
func intervalToModelPricing(iv *PricingInterval, base *ModelPricing, supportsCacheBreakdown bool, chPricing *ChannelModelPricing) *ModelPricing {
	pricing := &ModelPricing{}
	if base != nil {
		*pricing = *base
	}
	pricing.SupportsCacheBreakdown = supportsCacheBreakdown
	if iv.InputPrice != nil {
		pricing.InputPricePerToken = *iv.InputPrice
		pricing.InputPriceExplicit = true
		pricing.InputPricePerTokenPriority = *iv.InputPrice
		pricing.InputPriorityPriceExplicit = true
	}
	if iv.OutputPrice != nil {
		pricing.OutputPricePerToken = *iv.OutputPrice
		pricing.OutputPriceExplicit = true
		pricing.OutputPricePerTokenPriority = *iv.OutputPrice
		pricing.OutputPriorityPriceExplicit = true
	}
	if iv.CacheWritePrice != nil {
		pricing.CacheCreationPricePerToken = *iv.CacheWritePrice
		pricing.CacheCreationPricePerTokenPriority = *iv.CacheWritePrice
		pricing.CacheCreationPriceExplicit = true
		pricing.CacheCreationPriorityPriceExplicit = true
		pricing.CacheCreation5mPrice = *iv.CacheWritePrice
		pricing.CacheCreation1hPrice = *iv.CacheWritePrice
		pricing.CacheCreation1hPriceExplicit = true
	}
	if iv.CacheReadPrice != nil {
		pricing.CacheReadPricePerToken = *iv.CacheReadPrice
		pricing.CacheReadPricePerTokenPriority = *iv.CacheReadPrice
		pricing.CacheReadPriceExplicit = true
		pricing.CacheReadPriorityPriceExplicit = true
	}
	// 区间不携带图片 token 价格，沿用渠道级语义。
	if chPricing != nil {
		applyChannelImagePrices(chPricing, pricing)
	}
	return pricing
}

// LookupRequestTierPrice 根据层级标签获取按次价格，并区分“未配置”和“显式 0”。
func (r *ModelPricingResolver) LookupRequestTierPrice(resolved *ResolvedPricing, tierLabel string) (float64, bool) {
	tierLabel = strings.TrimSpace(tierLabel)
	for _, tier := range resolved.RequestTiers {
		if strings.EqualFold(strings.TrimSpace(tier.TierLabel), tierLabel) && tier.PerRequestPrice != nil {
			return *tier.PerRequestPrice, true
		}
	}
	return 0, false
}

// GetRequestTierPrice 保留原有便捷接口；需要区分显式 0 时使用 LookupRequestTierPrice。
func (r *ModelPricingResolver) GetRequestTierPrice(resolved *ResolvedPricing, tierLabel string) float64 {
	price, _ := r.LookupRequestTierPrice(resolved, tierLabel)
	return price
}

// LookupRequestTierPriceByContext 根据 context token 数获取按次价格，并返回是否命中。
func (r *ModelPricingResolver) LookupRequestTierPriceByContext(resolved *ResolvedPricing, totalContextTokens int) (float64, bool) {
	lookupTokens := totalContextTokens
	if lookupTokens <= 0 {
		lookupTokens = 1
	}
	for i := range resolved.RequestTiers {
		iv := &resolved.RequestTiers[i]
		// 带 label 的媒体 tier 只能由 SizeTier 精确命中，不能在普通按次文本
		// 请求中因默认 min=0/max=nil 而抢先匹配。
		if strings.TrimSpace(iv.TierLabel) != "" {
			continue
		}
		if lookupTokens > iv.MinTokens && (iv.MaxTokens == nil || lookupTokens <= *iv.MaxTokens) && iv.PerRequestPrice != nil {
			return *iv.PerRequestPrice, true
		}
	}
	return 0, false
}

// GetRequestTierPriceByContext 保留原有便捷接口。
func (r *ModelPricingResolver) GetRequestTierPriceByContext(resolved *ResolvedPricing, totalContextTokens int) float64 {
	price, _ := r.LookupRequestTierPriceByContext(resolved, totalContextTokens)
	return price
}
