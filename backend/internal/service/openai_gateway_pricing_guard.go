package service

import (
	"context"
	"fmt"
	"strings"
)

// ValidateSelectedOpenAIModelPricing verifies token pricing for the model that
// a selected OpenAI-compatible account will actually send upstream. It is used
// before HTTP forwarding and for every Responses WebSocket turn.
func (s *OpenAIGatewayService) ValidateSelectedOpenAIModelPricing(
	ctx context.Context,
	groupID *int64,
	account *Account,
	requestedModel string,
	requireCompact bool,
) error {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return fmt.Errorf("%w: model is empty", ErrModelPricingUnavailable)
	}
	if account == nil {
		return fmt.Errorf("%w: account is nil", ErrModelPricingUnavailable)
	}
	if s == nil {
		return fmt.Errorf("%w: OpenAI gateway billing service unavailable", ErrModelPricingUnavailable)
	}
	pricingRequestedModel := requestedModel
	mapping := ChannelMappingResult{MappedModel: requestedModel}
	if identity, ok := resolvedChannelPricingIdentityFromContext(ctx, requestedModel); ok {
		pricingRequestedModel = identity.requestedModel
		mapping = ChannelMappingResult{
			Mapped:             identity.mapped,
			MappedModel:        identity.channelMappedModel,
			BillingModelSource: identity.billingModelSource,
		}
	} else if s.channelService != nil && groupID != nil {
		mapping = s.channelService.ResolveChannelMapping(ctx, *groupID, requestedModel)
	}
	return s.validateSelectedOpenAIModelPricing(
		ctx,
		groupID,
		account,
		pricingRequestedModel,
		mapping,
		"",
		requireCompact,
		false,
	)
}

// ValidateSelectedOpenAIMessagesPricing validates the exact model resolution
// used by the Anthropic Messages compatibility path. That path combines a
// channel mapping with a group-level dispatch fallback, so validating only the
// scheduler's preferred model can miss an account mapping that wins at forward
// time.
func (s *OpenAIGatewayService) ValidateSelectedOpenAIMessagesPricing(
	ctx context.Context,
	groupID *int64,
	account *Account,
	requestedModel string,
	channelMapping ChannelMappingResult,
	messagesDispatchMappedModel string,
) error {
	requestedModel = strings.TrimSpace(requestedModel)
	if requestedModel == "" {
		return fmt.Errorf("%w: model is empty", ErrModelPricingUnavailable)
	}
	if account == nil {
		return fmt.Errorf("%w: account is nil", ErrModelPricingUnavailable)
	}
	if s == nil {
		return fmt.Errorf("%w: OpenAI gateway billing service unavailable", ErrModelPricingUnavailable)
	}
	if strings.TrimSpace(channelMapping.MappedModel) == "" {
		channelMapping.MappedModel = requestedModel
	}
	return s.validateSelectedOpenAIModelPricing(
		ctx,
		groupID,
		account,
		requestedModel,
		channelMapping,
		messagesDispatchMappedModel,
		false,
		true,
	)
}

// openAIPricingGuardModels 是一次准入检查涉及的整条模型链。
//
// 它被单独拎出来是因为准入检查有两条口径（token 与媒体），而"该给哪个模型名查价"
// 的推导规则必须完全一致——两边各推一次，迟早会在某个改名/映射分支上分叉，
// 那时守卫放行的模型和结算查价的模型就不是同一个了。
type openAIPricingGuardModels struct {
	Requested string
	Channel   string
	Billing   string
	Upstream  string
	// Primary 是渠道映射声明的计费模型来源对应的那个名字。
	Primary string
	Source  string
}

func resolveOpenAIPricingGuardModels(
	account *Account,
	requestedModel string,
	channelMapping ChannelMappingResult,
	messagesDispatchMappedModel string,
	requireCompact bool,
	normalizeCompatModel bool,
) openAIPricingGuardModels {
	channelModel := strings.TrimSpace(channelMapping.MappedModel)
	if channelModel == "" {
		channelModel = requestedModel
	}
	forwardModel := channelModel
	if normalizeCompatModel {
		forwardModel = NormalizeOpenAICompatRequestedModel(forwardModel)
	}

	billingModel := strings.TrimSpace(resolveOpenAIForwardModel(account, forwardModel, messagesDispatchMappedModel))
	var upstreamModel string
	if requireCompact {
		if compactModel := strings.TrimSpace(resolveOpenAICompactForwardModel(account, billingModel)); compactModel != "" && compactModel != billingModel {
			upstreamModel = compactModel
		} else {
			upstreamModel = strings.TrimSpace(normalizeOpenAIModelForUpstream(account, billingModel))
		}
	} else {
		upstreamModel = strings.TrimSpace(normalizeOpenAIModelForUpstream(account, billingModel))
	}

	// 来源 → 用哪个模型名查价的映射表与结算阶段共用，见 billing_model_selection.go。
	primaryModel := billingModel
	if selected, ok := selectBillingModelBySource(
		channelMapping.BillingModelSource,
		requestedModel,
		channelModel,
		upstreamModel,
	); ok {
		primaryModel = selected
	}

	return openAIPricingGuardModels{
		Requested: requestedModel,
		Channel:   channelModel,
		Billing:   billingModel,
		Upstream:  upstreamModel,
		Primary:   primaryModel,
		Source:    channelMapping.BillingModelSource,
	}
}

func (m openAIPricingGuardModels) unavailableError(kind BillingKind, account *Account) error {
	platform := ""
	if account != nil {
		platform = account.Platform
	}
	return fmt.Errorf(
		"%w for billing_kind=%s platform=%s requested_model=%q channel_model=%q billing_model=%q upstream_model=%q billing_model_source=%q selected_billing_model=%q",
		ErrModelPricingUnavailable,
		kind.String(),
		platform,
		m.Requested,
		m.Channel,
		m.Billing,
		m.Upstream,
		m.Source,
		m.Primary,
	)
}

func (s *OpenAIGatewayService) validateSelectedOpenAIModelPricing(
	ctx context.Context,
	groupID *int64,
	account *Account,
	requestedModel string,
	channelMapping ChannelMappingResult,
	messagesDispatchMappedModel string,
	requireCompact bool,
	normalizeCompatModel bool,
) error {
	models := resolveOpenAIPricingGuardModels(
		account,
		requestedModel,
		channelMapping,
		messagesDispatchMappedModel,
		requireCompact,
		normalizeCompatModel,
	)

	// 全局价目录只认这个具体上游模型自己的价格。
	gate := newStrictGlobalPricingGate(s.billingService, models.Upstream)
	if s.admitOpenAITokenPricing(ctx, groupID, models, gate.effective()) {
		return nil
	}

	return models.unavailableError(BillingKindToken, account)
}

// enforceResolvedOpenAITokenPricing validates a token model that is already
// the exact value about to be sent upstream. It deliberately skips channel and
// account model remapping: Live session payloads are forwarded verbatim, so
// applying another mapping here could validate a different SKU than the one
// the upstream will execute.
func (s *OpenAIGatewayService) enforceResolvedOpenAITokenPricing(
	ctx context.Context,
	groupID *int64,
	account *Account,
	requestedModel string,
	upstreamModel string,
) error {
	if s == nil {
		return fmt.Errorf("%w: OpenAI gateway billing service unavailable", ErrModelPricingUnavailable)
	}
	if account == nil {
		return fmt.Errorf("%w: account is nil", ErrModelPricingUnavailable)
	}
	upstreamModel = strings.TrimSpace(upstreamModel)
	models := openAIPricingGuardModels{
		Requested: strings.TrimSpace(requestedModel),
		Channel:   upstreamModel,
		Billing:   upstreamModel,
		Upstream:  upstreamModel,
		Primary:   upstreamModel,
	}
	gate := newStrictGlobalPricingGate(s.billingService, upstreamModel)
	if upstreamModel != "" && s.admitOpenAITokenPricing(ctx, groupID, models, gate.effective()) {
		return nil
	}
	return models.unavailableError(BillingKindToken, account)
}

// admitOpenAITokenPricing 是 token 路由准入的判定主体，拆出来的理由同
// GatewayService.admitTokenPricing：shadow 档需要用两种口径各跑一遍再比结论。
func (s *OpenAIGatewayService) admitOpenAITokenPricing(
	ctx context.Context,
	groupID *int64,
	models openAIPricingGuardModels,
	upstreamGloballyPriced bool,
) bool {
	// The concrete upstream SKU is authoritative for admission. In particular,
	// a priced public/requested alias must not hide an account mapping to an
	// unknown upstream model. If that concrete SKU is not in a trusted pricing
	// catalog, only an explicit, complete channel price for the channel's chosen
	// billing source is enough to opt into forwarding it.
	if s.hasResolvableOpenAITokenPricing(ctx, groupID, models.Upstream, upstreamGloballyPriced) {
		return true
	}
	return s.hasExplicitOpenAIChannelPricing(ctx, groupID, models.Primary)
}

// hasResolvableOpenAITokenPricing 判定渠道价；全局价的结论由调用方按灰度口径算好传入，
// 因为"该用严格还是宽松口径"是准入策略，不是这一层的职责。
func (s *OpenAIGatewayService) hasResolvableOpenAITokenPricing(
	ctx context.Context,
	groupID *int64,
	model string,
	globallyPriced bool,
) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	if groupID != nil && s.channelService != nil {
		if pricing := s.channelService.GetChannelModelPricing(ctx, *groupID, model); pricing != nil {
			mode := pricing.BillingMode
			if mode == BillingModePerRequest || mode == BillingModeImage {
				// These modes replace token billing entirely. A missing/invalid
				// default cannot borrow the global token price because settlement
				// would still fail when no tier matches.
				return validConfiguredPrice(pricing.PerRequestPrice)
			}
			if channelTokenPricingHasInvalidPrice(pricing) {
				return false
			}
			if channelTokenPricingConfigured(pricing) {
				return true
			}
			// A valid partial token override may inherit the remaining fields
			// from the global price below.
		}
	}
	return globallyPriced
}

func (s *OpenAIGatewayService) hasExplicitOpenAIChannelPricing(ctx context.Context, groupID *int64, model string) bool {
	model = strings.TrimSpace(model)
	if model == "" || groupID == nil || s.channelService == nil {
		return false
	}
	pricing := s.channelService.GetChannelModelPricing(ctx, *groupID, model)
	if pricing == nil {
		return false
	}
	mode := pricing.BillingMode
	if mode == BillingModePerRequest || mode == BillingModeImage {
		return validConfiguredPrice(pricing.PerRequestPrice)
	}
	return channelTokenPricingConfigured(pricing)
}
