package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	VideoPricingSourceGroup              = "group"
	VideoPricingSourceChannel            = "channel"
	VideoPricingSourceModelPriceOverride = "model_price_override"
	VideoPricingSourceCatalog            = "catalog"
)

type VideoPricingResolveRequest struct {
	Group          *Group
	Platform       string
	Mapping        ChannelMappingResult
	RequestedModel string
	ChannelModel   string
	UpstreamModel  string
	Provider       string
	Attributes     VideoPricingAttributes
}

type VideoPricingResolver struct {
	channels *ChannelService
	pricing  *PricingService
}

func NewVideoPricingResolver(channels *ChannelService, pricing *PricingService) *VideoPricingResolver {
	return &VideoPricingResolver{channels: channels, pricing: pricing}
}

func (r *VideoPricingResolver) Resolve(ctx context.Context, request VideoPricingResolveRequest) (*VideoPriceQuote, error) {
	if request.Group == nil {
		return nil, ErrVideoPricingMissing
	}
	source := strings.TrimSpace(request.Mapping.BillingModelSource)
	if source == "" {
		source = BillingModelSourceChannelMapped
	}
	if source == BillingModelSourceResponse {
		return nil, fmt.Errorf("%w: response_model cannot price an asynchronous video before submission", ErrVideoPricingInvalid)
	}
	billingModel, recognized := selectBillingModelBySource(
		source,
		request.RequestedModel,
		request.ChannelModel,
		request.UpstreamModel,
	)
	if !recognized {
		billingModel = strings.TrimSpace(request.ChannelModel)
	}
	if billingModel == "" {
		return nil, ErrVideoPricingMissing
	}

	attrs := request.Attributes
	attrs.Provider = request.Provider
	attrs.Model = request.UpstreamModel
	globalEntry := r.strictGlobalEntry(request.Platform, billingModel)
	var technicalProfile *VideoPricingConfig
	if globalEntry != nil {
		technicalProfile = globalEntry.VideoPricing
	}

	if groupPricing := findVideoGroupPricing(request.Group, request.Platform, billingModel); groupPricing != nil && groupPricing.BillingMode == BillingModeVideo {
		return r.resolveChannelStylePricing(groupPricing, technicalProfile, attrs, VideoPricingSourceGroup, request.Platform, billingModel)
	}
	if r.channels != nil {
		if channelPricing := r.channels.GetChannelModelPricing(ctx, request.Group.ID, billingModel); channelPricing != nil && channelPricing.BillingMode == BillingModeVideo {
			return r.resolveChannelStylePricing(channelPricing, technicalProfile, attrs, VideoPricingSourceChannel, request.Platform, billingModel)
		}
	}
	if globalEntry == nil || globalEntry.VideoPricing == nil || !globalEntry.VideoPricing.Enabled {
		return nil, ErrVideoPricingMissing
	}
	quote, err := ResolveVideoPricingConfig(globalEntry.VideoPricing, attrs)
	if err != nil {
		return nil, err
	}
	quote.Source = VideoPricingSourceCatalog
	if globalEntry.VideoPricingOperatorOverride {
		quote.Source = VideoPricingSourceModelPriceOverride
	}
	quote.BillingModel = billingModel
	quote.PricingPlatform = request.Platform
	return quote, nil
}

func (r *VideoPricingResolver) strictGlobalEntry(platform, model string) *ModelPriceEntry {
	if r == nil || r.pricing == nil {
		return nil
	}
	return r.pricing.LookupModelPricingStrictForPlatform(platform, model)
}

func (r *VideoPricingResolver) resolveChannelStylePricing(
	pricing *ChannelModelPricing,
	technicalProfile *VideoPricingConfig,
	attrs VideoPricingAttributes,
	source, platform, billingModel string,
) (*VideoPriceQuote, error) {
	if technicalProfile != nil && technicalProfile.Enabled && ValidateVideoPricingConfig(technicalProfile) == nil {
		if normalized, err := normalizeVideoPricingAttributes(technicalProfile, attrs); err == nil {
			attrs = normalized
		}
	}
	if attrs.RequestMode == "" {
		attrs.RequestMode = VideoRequestModeStandard
	}
	if attrs.InferenceMode == "" {
		attrs.InferenceMode = VideoInferenceModeOnline
	}
	if attrs.AudioEnabled == nil && !attrs.OutputSpecUnverified {
		value := false
		attrs.AudioEnabled = &value
	}
	if attrs.MaximumOutputSeconds <= 0 {
		attrs.MaximumOutputSeconds = attrs.Seconds
	}
	attrs.MaximumUnits = float64(attrs.MaximumOutputSeconds)

	probeAttrs := attrs
	probeAttrs.EstimatedVideoTokens = 1
	if probeAttrs.MaximumUnits < 1 {
		probeAttrs.MaximumUnits = 1
	}
	probe, err := ResolveVideoPrice(pricing, probeAttrs)
	if errors.Is(err, ErrVideoPricingMissing) {
		return nil, ErrVideoPricingRuleMissing
	}
	if err != nil {
		return nil, err
	}
	var technicalQuote *VideoPriceQuote
	if probe.BillingUnit == VideoBillingUnitVideoToken {
		if technicalProfile == nil || !technicalProfile.Enabled {
			return nil, ErrVideoPricingEstimatorMissing
		}
		technicalQuote, err = ResolveVideoPricingConfig(technicalProfile, attrs)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrVideoPricingEstimatorMissing, err)
		}
		if technicalQuote.BillingUnit != VideoBillingUnitVideoToken || technicalQuote.Estimator == nil {
			return nil, ErrVideoPricingEstimatorMissing
		}
		attrs = technicalQuote.Attributes
		attrs.EstimatedVideoTokens = technicalQuote.EstimatedUnits
		attrs.MaximumUnits = technicalQuote.MaximumUnits
	}
	quote, err := ResolveVideoPrice(pricing, attrs)
	if errors.Is(err, ErrVideoPricingMissing) {
		return nil, ErrVideoPricingRuleMissing
	}
	if err != nil {
		return nil, err
	}
	quote.Source = source
	quote.BillingModel = billingModel
	quote.PricingPlatform = platform
	quote.Attributes = attrs
	if quote.RuleKey == "" {
		quote.RuleKey = fmt.Sprintf("interval:%d", quote.RuleID)
	}
	if technicalProfile != nil && technicalProfile.Enabled {
		quote.ConfigVersion = technicalProfile.Version
		quote.ConfigHash = videoPricingConfigHash(technicalProfile)
	}
	if technicalQuote != nil {
		quote.EstimatorName = technicalQuote.EstimatorName
		quote.Estimator = technicalQuote.Estimator
	}
	return quote, nil
}
