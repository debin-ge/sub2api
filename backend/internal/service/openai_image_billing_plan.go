package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// OpenAIImageBillingPlan locks the pricing mode, billing identity and price
// snapshot selected immediately before an Image API request reaches upstream.
//
// BillingKindImage describes the workload. Mode describes how that workload is
// charged; GPT Image models may therefore legitimately use BillingModeToken.
type OpenAIImageBillingPlan struct {
	Mode              BillingMode
	Model             string
	Source            string
	SizeTier          string
	RequireImageInput bool
	Resolved          *ResolvedPricing
}

// hasResolvableOpenAIImageTokenPricing is the scheduler-level candidate check.
// The exact image tier and price snapshot are resolved again immediately before
// forwarding. Native Responses image tools do not use this function for their
// final guard.
func (s *OpenAIGatewayService) hasResolvableOpenAIImageTokenPricing(
	ctx context.Context,
	groupID *int64,
	model string,
) bool {
	return s.hasResolvableOpenAIImageTokenPricingForPlatforms(
		ctx, groupID, []string{PlatformFromAPIKey(openAIPricingGuardAPIKey(ctx, groupID))}, model,
	)
}

func (s *OpenAIGatewayService) hasResolvableOpenAIImageTokenPricingForPlatforms(
	ctx context.Context,
	groupID *int64,
	platforms []string,
	model string,
) bool {
	if s == nil || s.billingService == nil {
		return false
	}
	resolver := s.resolver
	if resolver == nil {
		resolver = NewModelPricingResolver(s.channelService, s.billingService)
	}
	var channelPricing *ChannelModelPricing
	if groupID != nil && s.channelService != nil {
		channelPricing = s.channelService.GetChannelModelPricing(ctx, *groupID, strings.TrimSpace(model))
		if channelPricing != nil {
			mode := channelPricing.BillingMode
			if mode != "" && mode != BillingModeToken {
				return false
			}
		}
	}
	resolved, err := s.resolveStrictOpenAIImageTokenPricing(
		ctx,
		resolver,
		groupID,
		platforms,
		strings.TrimSpace(model),
		false,
	)
	return err == nil && resolved != nil
}

func (s *OpenAIGatewayService) resolveOpenAIImageBillingPlan(
	ctx context.Context,
	apiKey *APIKey,
	groupID *int64,
	model string,
	imageSizeTier string,
	requireImageInput bool,
) (*OpenAIImageBillingPlan, error) {
	return s.resolveOpenAIImageBillingPlanForPlatforms(
		ctx,
		apiKey,
		groupID,
		[]string{PlatformFromAPIKey(apiKey)},
		model,
		imageSizeTier,
		requireImageInput,
	)
}

func (s *OpenAIGatewayService) resolveOpenAIImageBillingPlanForPlatforms(
	ctx context.Context,
	apiKey *APIKey,
	groupID *int64,
	platforms []string,
	model string,
	imageSizeTier string,
	requireImageInput bool,
) (*OpenAIImageBillingPlan, error) {
	if s == nil || s.billingService == nil {
		return nil, fmt.Errorf("%w: OpenAI image billing service unavailable", ErrModelPricingUnavailable)
	}
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, fmt.Errorf("%w: image billing model is empty", ErrModelPricingUnavailable)
	}
	tier, err := NormalizeImageBillingTierStrictOrDefault(imageSizeTier)
	if err != nil {
		return nil, err
	}

	if apiKey == nil {
		apiKey = openAIPricingGuardAPIKey(ctx, groupID)
	}
	if refreshed := s.apiKeyWithFreshGroupMediaPricing(ctx, apiKey); refreshed != nil {
		apiKey = refreshed
	}
	if price := configuredOpenAIImageGroupPrice(apiKey, tier); price != nil {
		return newOpenAIImageUnitBillingPlan(model, tier, PricingSourceGroup, *price), nil
	}

	resolver := s.resolver
	if resolver == nil {
		resolver = NewModelPricingResolver(s.channelService, s.billingService)
	}
	if groupID == nil && apiKey != nil {
		groupID = apiKey.GroupID
	}

	if groupID != nil && s.channelService != nil {
		channelPricing := s.channelService.GetChannelModelPricing(ctx, *groupID, model)
		if channelPricing != nil {
			mode := channelPricing.BillingMode
			if mode == "" {
				mode = BillingModeToken
			}
			switch mode {
			case BillingModePerRequest, BillingModeImage:
				resolved := resolver.Resolve(ctx, PricingInput{Model: model, GroupID: groupID})
				if price, ok := exactOpenAIImageRequestPrice(resolver, resolved, tier); ok {
					return newOpenAIImageUnitBillingPlan(model, tier, PricingSourceChannel, price), nil
				}
				return nil, fmt.Errorf(
					"%w for image model %s tier %s: explicit channel image pricing is incomplete",
					ErrModelPricingUnavailable,
					model,
					tier,
				)
			case BillingModeToken:
				resolved, err := s.resolveStrictOpenAIImageTokenPricing(
					ctx,
					resolver,
					groupID,
					platforms,
					model,
					requireImageInput,
				)
				if err != nil {
					return nil, fmt.Errorf(
						"resolve explicit channel image token pricing for %s: %w",
						model,
						err,
					)
				}
				return &OpenAIImageBillingPlan{
					Mode:              BillingModeToken,
					Model:             model,
					Source:            resolved.Source,
					SizeTier:          tier,
					RequireImageInput: requireImageInput,
					Resolved:          resolved,
				}, nil
			}
		}
	}

	if pricing, err := s.billingService.GetImageTokenPricingStrictForPlatforms(platforms, model, requireImageInput); err == nil {
		return &OpenAIImageBillingPlan{
			Mode:              BillingModeToken,
			Model:             model,
			Source:            PricingSourceModelPrice,
			SizeTier:          tier,
			RequireImageInput: requireImageInput,
			Resolved: &ResolvedPricing{
				Mode:                   BillingModeToken,
				BasePricing:            pricing,
				Source:                 PricingSourceModelPrice,
				SupportsCacheBreakdown: pricing.SupportsCacheBreakdown,
			},
		}, nil
	}

	if price, ok := s.billingService.strictImageUnitPriceForPlatforms(platforms, model, tier, nil); ok {
		return newOpenAIImageUnitBillingPlan(model, tier, PricingSourceModelPrice, price), nil
	}
	return nil, fmt.Errorf(
		"%w for image model %s tier %s",
		ErrModelPricingUnavailable,
		model,
		tier,
	)
}

func wrapOpenAIImageBillingPlanError(
	err error,
	account *Account,
	upstreamModel string,
	imageSizeTier string,
) error {
	if err == nil || !errors.Is(err, ErrModelPricingUnavailable) {
		return err
	}
	platform := PlatformOpenAI
	if account != nil && strings.TrimSpace(account.Platform) != "" {
		platform = account.Platform
	}
	return fmt.Errorf(
		"%w for billing_kind=%s platform=%s upstream_model=%q media_size_tier=%q: %v",
		ErrModelPricingUnavailable,
		BillingKindImage.String(),
		platform,
		strings.TrimSpace(upstreamModel),
		strings.TrimSpace(imageSizeTier),
		err,
	)
}

func (s *OpenAIGatewayService) resolveStrictOpenAIImageTokenPricing(
	ctx context.Context,
	resolver *ModelPricingResolver,
	groupID *int64,
	platforms []string,
	model string,
	requireImageInput bool,
) (*ResolvedPricing, error) {
	if resolver == nil {
		return nil, fmt.Errorf("%w: image pricing resolver is nil", ErrModelPricingUnavailable)
	}
	return resolver.ResolveStrictImageToken(ctx, PricingInput{
		Model:     model,
		Platforms: platforms,
		GroupID:   groupID,
	}, requireImageInput)
}

func channelImageTokenPricingConfigured(pricing *ChannelModelPricing) bool {
	if pricing == nil {
		return false
	}
	mode := pricing.BillingMode
	if mode != "" && mode != BillingModeToken {
		return false
	}
	if channelTokenPricingHasInvalidPrice(pricing) {
		return false
	}
	inputConfigured := validConfiguredPrice(pricing.InputPrice)
	outputConfigured := validConfiguredPrice(pricing.ImageOutputPrice) ||
		validConfiguredPrice(pricing.OutputPrice)
	return inputConfigured && outputConfigured
}

func openAIImageTokenPricingComplete(pricing *ModelPricing, requireImageInput bool) bool {
	if pricing == nil {
		return false
	}
	inputConfigured := pricing.InputPriceExplicit || pricing.InputPricePerToken > 0
	imageOutputConfigured := pricing.ImageOutputPriceExplicit ||
		pricing.ImageOutputPricePerToken > 0 ||
		pricing.OutputPriceExplicit ||
		pricing.OutputPricePerToken > 0
	imageInputConfigured := pricing.ImageInputPriceExplicit ||
		pricing.ImageInputPricePerToken > 0 ||
		inputConfigured
	return inputConfigured && imageOutputConfigured && (!requireImageInput || imageInputConfigured)
}

func configuredOpenAIImageGroupPrice(apiKey *APIKey, tier string) *float64 {
	if apiKey == nil || apiKey.Group == nil {
		return nil
	}
	price := apiKey.Group.GetImagePrice(tier)
	if !validConfiguredPrice(price) {
		return nil
	}
	value := *price
	return &value
}

func exactOpenAIImageRequestPrice(
	resolver *ModelPricingResolver,
	resolved *ResolvedPricing,
	tier string,
) (float64, bool) {
	if resolver == nil || resolved == nil ||
		(resolved.Mode != BillingModeImage && resolved.Mode != BillingModePerRequest) {
		return 0, false
	}
	if price, ok := resolver.LookupRequestTierPrice(resolved, tier); ok && isFiniteNonNegativePrice(price) {
		return price, true
	}
	if resolved.DefaultPerRequestPriceSet && isFiniteNonNegativePrice(resolved.DefaultPerRequestPrice) {
		return resolved.DefaultPerRequestPrice, true
	}
	return 0, false
}

func newOpenAIImageUnitBillingPlan(model, tier, source string, unitPrice float64) *OpenAIImageBillingPlan {
	return &OpenAIImageBillingPlan{
		Mode:     BillingModeImage,
		Model:    model,
		Source:   source,
		SizeTier: tier,
		Resolved: &ResolvedPricing{
			Mode:                      BillingModeImage,
			Source:                    source,
			DefaultPerRequestPrice:    unitPrice,
			DefaultPerRequestPriceSet: true,
		},
	}
}
