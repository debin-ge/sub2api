package service

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

type nonBillingEndpointPricingExemptionKey struct{}

type resolvedChannelPricingIdentityKey struct{}
type finalGeminiImagePricingGuardKey struct{}

type resolvedChannelPricingIdentity struct {
	requestedModel     string
	channelMappedModel string
	billingModelSource string
	// mapped 记录渠道映射本身是否命中，不能由 requested/mapped 模型名是否
	// 相等反推：显式配置「映射到自身 + 指定计费基准」时两者相同但确实映射了。
	mapped bool
}

func withNonBillingEndpointPricingExemption(ctx context.Context) context.Context {
	return context.WithValue(ctx, nonBillingEndpointPricingExemptionKey{}, true)
}

func hasNonBillingEndpointPricingExemption(ctx context.Context) bool {
	exempt, _ := ctx.Value(nonBillingEndpointPricingExemptionKey{}).(bool)
	return exempt
}

// WithResolvedChannelPricingIdentity pins the one channel mapping performed at
// request ingress. Some handlers must route accounts with the mapped model,
// while pricing still needs the original requested model and mapping source.
// Carrying both prevents the scheduler/final guard from feeding an already
// mapped model through wildcard mappings a second time.
func WithResolvedChannelPricingIdentity(
	ctx context.Context,
	requestedModel string,
	mapping ChannelMappingResult,
) context.Context {
	if ctx == nil {
		return ctx
	}
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return ctx
	}
	mappedModel := strings.TrimSpace(mapping.MappedModel)
	if mappedModel == "" {
		mappedModel = requestedModel
	}
	return context.WithValue(ctx, resolvedChannelPricingIdentityKey{}, resolvedChannelPricingIdentity{
		requestedModel:     requestedModel,
		channelMappedModel: mappedModel,
		billingModelSource: strings.TrimSpace(mapping.BillingModelSource),
		mapped:             mapping.Mapped,
	})
}

func resolvedChannelPricingIdentityFromContext(
	ctx context.Context,
	routedModel string,
) (resolvedChannelPricingIdentity, bool) {
	if ctx == nil {
		return resolvedChannelPricingIdentity{}, false
	}
	identity, ok := ctx.Value(resolvedChannelPricingIdentityKey{}).(resolvedChannelPricingIdentity)
	if !ok || identity.requestedModel == "" || identity.channelMappedModel == "" {
		return resolvedChannelPricingIdentity{}, false
	}
	routedModel = strings.TrimSpace(routedModel)
	if routedModel != "" && routedModel != identity.channelMappedModel {
		return resolvedChannelPricingIdentity{}, false
	}
	return identity, true
}

// WithFinalGeminiImagePricingGuard marks native Gemini routes whose concrete
// forwarder performs an exact model+tier media check before any HTTP call.
// Image-only SKUs on these routes must not also be required to have token
// pricing unless the exact channel model explicitly selects token billing.
func WithFinalGeminiImagePricingGuard(ctx context.Context) context.Context {
	if ctx == nil {
		return ctx
	}
	return context.WithValue(ctx, finalGeminiImagePricingGuardKey{}, true)
}

func hasFinalGeminiImagePricingGuard(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	enabled, _ := ctx.Value(finalGeminiImagePricingGuardKey{}).(bool)
	return enabled
}

func channelImageUsesTokenBilling(
	ctx context.Context,
	channelService *ChannelService,
	apiKey *APIKey,
	model string,
) bool {
	if channelService == nil || apiKey == nil || apiKey.GroupID == nil {
		return false
	}
	pricing := channelService.GetChannelModelPricing(
		ctx,
		*apiKey.GroupID,
		strings.TrimSpace(model),
	)
	if pricing == nil {
		return false
	}
	return pricing.BillingMode == "" || pricing.BillingMode == BillingModeToken
}

func channelTokenPricingConfigured(pricing *ChannelModelPricing) bool {
	if pricing == nil {
		return false
	}
	mode := pricing.BillingMode
	if mode != "" && mode != BillingModeToken {
		return false
	}
	flat := tokenPricePresence{
		input:      validConfiguredPrice(pricing.InputPrice),
		output:     validConfiguredPrice(pricing.OutputPrice),
		cacheWrite: validConfiguredPrice(pricing.CacheWritePrice),
		cacheRead:  validConfiguredPrice(pricing.CacheReadPrice),
	}
	if channelTokenPricingHasInvalidPrice(pricing) {
		return false
	}

	validIntervals := filterValidTokenIntervals(pricing.Intervals)

	// 完整 flat 价格覆盖所有区间缺口；区间字段可以只覆盖需要调整的桶。
	if flat.complete() {
		return true
	}
	if len(validIntervals) == 0 {
		return false
	}

	// An interval-only price for an otherwise unknown model must cover every
	// positive context size. Otherwise a gap resolves to an empty BasePricing
	// and recreates the zero-cost path this guard is intended to prevent.
	sort.Slice(validIntervals, func(i, j int) bool {
		return validIntervals[i].MinTokens < validIntervals[j].MinTokens
	})
	if validIntervals[0].MinTokens != 0 {
		return false
	}
	for i := range validIntervals {
		iv := &validIntervals[i]
		effective := flat.withInterval(iv)
		if !effective.complete() {
			return false
		}
		if i == 0 {
			continue
		}
		previousMax := validIntervals[i-1].MaxTokens
		if previousMax == nil || iv.MinTokens != *previousMax {
			return false
		}
	}
	return validIntervals[len(validIntervals)-1].MaxTokens == nil
}

type tokenPricePresence struct {
	input      bool
	output     bool
	cacheWrite bool
	cacheRead  bool
}

func (p tokenPricePresence) complete() bool {
	return p.input && p.output && p.cacheWrite && p.cacheRead
}

func (p tokenPricePresence) withInterval(iv *PricingInterval) tokenPricePresence {
	if iv == nil {
		return p
	}
	p.input = p.input || validConfiguredPrice(iv.InputPrice)
	p.output = p.output || validConfiguredPrice(iv.OutputPrice)
	p.cacheWrite = p.cacheWrite || validConfiguredPrice(iv.CacheWritePrice)
	p.cacheRead = p.cacheRead || validConfiguredPrice(iv.CacheReadPrice)
	return p
}

func validConfiguredPrice(price *float64) bool {
	return price != nil && isFiniteNonNegativePrice(*price)
}

func hasInvalidTokenPrice(prices ...*float64) bool {
	for _, price := range prices {
		if price != nil && !isFiniteNonNegativePrice(*price) {
			return true
		}
	}
	return false
}

func channelTokenPricingHasInvalidPrice(pricing *ChannelModelPricing) bool {
	if pricing == nil {
		return false
	}
	if hasInvalidTokenPrice(pricing.InputPrice, pricing.OutputPrice, pricing.CacheWritePrice, pricing.CacheReadPrice) {
		return true
	}
	for i := range pricing.Intervals {
		iv := &pricing.Intervals[i]
		if hasInvalidTokenPrice(iv.InputPrice, iv.OutputPrice, iv.CacheWritePrice, iv.CacheReadPrice) {
			return true
		}
	}
	return false
}

// ValidateUsagePricing verifies that a dynamically-routed provider model has
// an explicit channel price or a resolvable global price before the request is
// sent upstream. A configured zero price is still a valid price; only a
// missing price is rejected.
func (s *GatewayService) ValidateUsagePricing(ctx context.Context, apiKey *APIKey, account *Account, requestedModel string) error {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return fmt.Errorf("%w: model is empty", ErrModelPricingUnavailable)
	}
	if account == nil {
		return fmt.Errorf("%w: account is nil", ErrModelPricingUnavailable)
	}
	if s == nil {
		return fmt.Errorf("%w: gateway billing service unavailable", ErrModelPricingUnavailable)
	}
	mappedModel := requestedModel
	billingModelSource := ""
	if identity, ok := resolvedChannelPricingIdentityFromContext(ctx, requestedModel); ok {
		requestedModel = identity.requestedModel
		mappedModel = identity.channelMappedModel
		billingModelSource = identity.billingModelSource
	} else if s.channelService != nil && apiKey != nil && apiKey.GroupID != nil {
		mapping := s.channelService.ResolveChannelMapping(ctx, *apiKey.GroupID, requestedModel)
		if mapped := strings.TrimSpace(mapping.MappedModel); mapped != "" {
			mappedModel = mapped
		}
		billingModelSource = mapping.BillingModelSource
	}

	upstreamModel := strings.TrimSpace(resolveAccountUpstreamModel(account, mappedModel))
	// 来源 → 用哪个模型名查价的映射表与结算阶段共用，见 billing_model_selection.go。
	// 准入这一侧不给空名字兜底：选中的名字是空串就让它原样往下走，admit 判定查不到
	// 价，请求被拒——这正是 I1 要的结果。
	billingModel := mappedModel
	if selected, ok := selectBillingModelBySource(billingModelSource, requestedModel, mappedModel, upstreamModel); ok {
		billingModel = selected
	}
	// MiniMax usage billing intentionally always follows the concrete upstream
	// model so a priced request alias cannot hide an unpriced upstream target.
	// It must still pass through the strict global pricing gate below: the loose
	// MiniMax fallback uses substring matching, so checking it directly would let
	// an unknown SKU such as "MiniMax-M3-future" inherit the M3 price in enforce
	// mode.
	if account.IsMiniMax() {
		billingModel = upstreamModel
	}

	imageOnlyAdmission := hasFinalGeminiImagePricingGuard(ctx) &&
		isImageGenerationModel(upstreamModel) &&
		!channelImageUsesTokenBilling(ctx, s.channelService, apiKey, upstreamModel)
	if !imageOnlyAdmission {
		// 全局价目录只认这个具体上游模型自己的价格；模糊 family fallback
		// 不能作为产生上游成本的准入证据。
		gate := newStrictGlobalPricingGate(s.billingService, upstreamModel)
		if s.admitTokenPricing(ctx, apiKey, billingModel, upstreamModel, gate.effective()) {
		} else {
			return fmt.Errorf(
				"%w for platform=%s requested_model=%q mapped_model=%q upstream_model=%q",
				ErrModelPricingUnavailable,
				account.Platform,
				requestedModel,
				mappedModel,
				upstreamModel,
			)
		}
	}

	// A native Responses image tool carries an independent, exact media SKU.
	// Its model/tier is forwarded verbatim and must not inherit the top-level
	// text model's media price or the generic settlement fallback chain.
	if mediaIntent, ok := OpenAIImageGenerationPricingIntentFromContext(ctx); ok {
		mediaModel := strings.TrimSpace(mediaIntent.BillingModel)
		mediaTier := NormalizeImageBillingTierOrDefault(mediaIntent.SizeTier)
		if mediaModel == "" || !s.hasResolvableImagePricing(ctx, mediaModel, mediaTier, apiKey) {
			return fmt.Errorf(
				"%w for billing_kind=%s platform=%s requested_model=%q mapped_model=%q upstream_model=%q media_billing_model=%q media_size_tier=%q",
				ErrModelPricingUnavailable,
				BillingKindImage.String(),
				account.Platform,
				requestedModel,
				mappedModel,
				upstreamModel,
				mediaModel,
				mediaTier,
			)
		}
		return nil
	}

	// Other Responses-style routes can still incur an independent per-image
	// charge without an exact nested tool identity. Account selection happens
	// before an output size is known, so admission must prove that at least one
	// model in the settlement fallback chain is priced for every possible tier.
	if OpenAIImageGenerationIntentFromContext(ctx) &&
		!s.admitImageGenerationPricing(ctx, apiKey, billingModel, upstreamModel, requestedModel) {
		return fmt.Errorf(
			"%w for billing_kind=%s platform=%s requested_model=%q mapped_model=%q upstream_model=%q selected_billing_model=%q",
			ErrModelPricingUnavailable,
			BillingKindImage.String(),
			account.Platform,
			requestedModel,
			mappedModel,
			upstreamModel,
			billingModel,
		)
	}
	return nil
}

func (s *GatewayService) admitImageGenerationPricing(
	ctx context.Context,
	apiKey *APIKey,
	models ...string,
) bool {
	seen := make(map[string]struct{}, len(models))
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		if _, exists := seen[model]; exists {
			continue
		}
		seen[model] = struct{}{}
		pricedForEveryTier := true
		for _, tier := range []string{ImageBillingSize1K, ImageBillingSize2K, ImageBillingSize4K} {
			if !s.hasResolvableImagePricing(ctx, model, tier, apiKey) {
				pricedForEveryTier = false
				break
			}
		}
		if pricedForEveryTier {
			return true
		}
	}
	return false
}

// admitTokenPricing 是 token 路由准入的判定主体。
//
// 它从 ValidateUsagePricing 里拆出来，是为了能用两种"上游模型有没有全局价"的口径各跑
// 一遍：shadow 档要记的是两次结论不同的那部分请求，而不是两种口径本身不同的那部分。
func (s *GatewayService) admitTokenPricing(
	ctx context.Context,
	apiKey *APIKey,
	billingModel string,
	upstreamModel string,
	upstreamGloballyPriced bool,
) bool {
	// A price attached to the requested/channel alias must never hide an
	// unknown concrete upstream SKU. If the concrete upstream model has a
	// global price, use only that model as the fallback candidate so aliases
	// still work without reopening the "any priced candidate" bypass.
	if upstreamGloballyPriced {
		billableModel := s.billableModelWithFallback(
			ctx,
			apiKey,
			billingModel,
			upstreamModel,
		)
		if s.hasResolvableTokenPricing(ctx, billableModel, apiKey) {
			return true
		}
	}

	// An administrator may intentionally price a public/requested alias
	// independently of the upstream SKU. When the upstream model itself is
	// unknown, require a complete channel price on exactly the model selected
	// by BillingModelSource; another SKU's global or channel price is not
	// sufficient.
	return s.hasCompleteExplicitChannelPricing(ctx, billingModel, apiKey)
}

func (s *GatewayService) hasCompleteExplicitChannelPricing(ctx context.Context, model string, apiKey *APIKey) bool {
	model = strings.TrimSpace(model)
	if model == "" || s == nil || s.channelService == nil || apiKey == nil || apiKey.GroupID == nil {
		return false
	}
	pricing := s.channelService.GetChannelModelPricing(ctx, *apiKey.GroupID, model)
	if pricing == nil {
		return false
	}
	switch pricing.BillingMode {
	case BillingModePerRequest, BillingModeImage:
		return validConfiguredPrice(pricing.PerRequestPrice)
	case "", BillingModeToken:
		return channelTokenPricingConfigured(pricing)
	default:
		return false
	}
}

func pricingGuardAPIKey(ctx context.Context, s *GatewayService, groupID *int64) *APIKey {
	if groupID == nil {
		return nil
	}
	group := s.groupFromContext(ctx, *groupID)
	if group == nil {
		// Price resolution only needs the group identity. The fully hydrated group
		// is normally already in context, but a minimal value keeps forced-platform
		// and isolated service callers on the same fail-closed path.
		group = &Group{ID: *groupID}
	}
	return &APIKey{GroupID: groupID, Group: group}
}
