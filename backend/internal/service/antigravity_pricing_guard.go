package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
)

// validateResolvedAntigravityTokenPricing guards the exact model embedded in
// the v1internal request immediately before Antigravity forwarding. Account
// mapping, thinking suffixes, and model-not-found fallback can all change that
// model after scheduler admission, so the scheduler check alone is not enough.
func (s *AntigravityGatewayService) validateResolvedAntigravityTokenPricing(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	requestedModel string,
	upstreamModel string,
) error {
	upstreamModel = strings.TrimSpace(upstreamModel)
	if upstreamModel == "" {
		return fmt.Errorf("%w: Antigravity upstream model is empty", ErrModelPricingUnavailable)
	}
	if s == nil {
		return fmt.Errorf("%w: Antigravity gateway billing service unavailable", ErrModelPricingUnavailable)
	}
	// Directly constructed test services predate pricing admission. Production
	// construction always sets this flag and must fail closed even if a billing
	// dependency is accidentally missing.
	if !s.pricingGuardRequired && s.billingService == nil {
		return nil
	}

	var apiKey *APIKey
	if c != nil {
		apiKey = getAPIKeyFromContext(c)
	}
	platform := PlatformAntigravity
	if account != nil && strings.TrimSpace(account.Platform) != "" {
		platform = account.Platform
	}
	gate := newStrictGlobalPricingGateForPlatforms(
		s.billingService,
		pricingPlatformCandidates(apiKey, account),
		upstreamModel,
	)
	if s.hasResolvableAntigravityTokenPricing(ctx, apiKey, upstreamModel, gate.effective()) {
		return nil
	}

	return fmt.Errorf(
		"%w for platform=%s requested_model=%q upstream_model=%q",
		ErrModelPricingUnavailable,
		platform,
		strings.TrimSpace(requestedModel),
		upstreamModel,
	)
}

func (s *AntigravityGatewayService) hasResolvableAntigravityTokenPricing(
	ctx context.Context,
	apiKey *APIKey,
	model string,
	globallyPriced bool,
) bool {
	model = strings.TrimSpace(model)
	if model == "" {
		return false
	}
	if s.channelService != nil && apiKey != nil && apiKey.GroupID != nil {
		if pricing := s.channelService.GetChannelModelPricing(ctx, *apiKey.GroupID, model); pricing != nil {
			switch pricing.BillingMode {
			case BillingModePerRequest, BillingModeImage:
				// These modes replace token billing. A missing default cannot
				// borrow the global token price because settlement would still
				// have no request price.
				return validConfiguredPrice(pricing.PerRequestPrice)
			case "", BillingModeToken:
				if channelTokenPricingHasInvalidPrice(pricing) {
					return false
				}
				if channelTokenPricingConfigured(pricing) {
					return true
				}
				// A valid partial override may inherit the exact global SKU.
			default:
				return false
			}
		}
	}
	return globallyPriced
}

// antigravityImageUsesTokenBilling mirrors GatewayService.calculateRecordUsageCost:
// an image request is token-billed only when the exact channel model explicitly
// selects token mode. Catalog/group image pricing is otherwise sufficient and
// must not be rejected merely because the image-only SKU has no token price.
func (s *AntigravityGatewayService) antigravityImageUsesTokenBilling(
	ctx context.Context,
	c *gin.Context,
	model string,
) bool {
	if s == nil || s.channelService == nil || c == nil {
		return false
	}
	apiKey := getAPIKeyFromContext(c)
	return channelImageUsesTokenBilling(ctx, s.channelService, apiKey, model)
}

// validateResolvedAntigravityUsagePricing selects the same billing dimension
// that generic settlement will use for this result. Text requests and
// channel-token image requests require token pricing; other image requests
// require only the exact model+tier media price.
func (s *AntigravityGatewayService) validateResolvedAntigravityUsagePricing(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	requestedModel string,
	upstreamModel string,
	imageIdentity geminiImageBillingIdentity,
) error {
	if imageIdentity.Count == 0 ||
		s.antigravityImageUsesTokenBilling(ctx, c, imageIdentity.Model) {
		if err := s.validateResolvedAntigravityTokenPricing(
			ctx,
			c,
			account,
			requestedModel,
			upstreamModel,
		); err != nil {
			return err
		}
	}
	return s.validateResolvedAntigravityImagePricing(ctx, c, account, imageIdentity)
}

// validateResolvedAntigravityImagePricing applies the same final-SKU rule to
// Gemini image output. It receives an already-resolved identity and therefore
// never re-runs account or wildcard model mapping.
func (s *AntigravityGatewayService) validateResolvedAntigravityImagePricing(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	identity geminiImageBillingIdentity,
) error {
	if identity.Count == 0 {
		return nil
	}
	if s == nil {
		return fmt.Errorf(
			"%w: Antigravity gateway billing service unavailable",
			ErrModelPricingUnavailable,
		)
	}
	if !s.pricingGuardRequired && s.billingService == nil {
		return nil
	}
	if s.billingService == nil {
		return fmt.Errorf(
			"%w: Antigravity gateway billing service unavailable",
			ErrModelPricingUnavailable,
		)
	}

	var apiKey *APIKey
	if c != nil {
		apiKey = getAPIKeyFromContext(c)
	}
	if s.hasResolvableAntigravityImagePricing(
		ctx,
		apiKey,
		pricingPlatformCandidates(apiKey, account),
		identity.Model,
		identity.SizeTier,
	) {
		return nil
	}

	platform := PlatformAntigravity
	if account != nil && strings.TrimSpace(account.Platform) != "" {
		platform = account.Platform
	}
	return fmt.Errorf(
		"%w for billing_kind=%s platform=%s upstream_model=%q media_size_tier=%q",
		ErrModelPricingUnavailable,
		BillingKindImage.String(),
		platform,
		identity.Model,
		identity.SizeTier,
	)
}

func (s *AntigravityGatewayService) hasResolvableAntigravityImagePricing(
	ctx context.Context,
	apiKey *APIKey,
	platforms []string,
	model string,
	sizeTier string,
) bool {
	model = strings.TrimSpace(model)
	if model == "" || s == nil || s.billingService == nil {
		return false
	}
	if s.channelService != nil && apiKey != nil && apiKey.GroupID != nil {
		if pricing := s.channelService.GetChannelModelPricing(ctx, *apiKey.GroupID, model); pricing != nil {
			switch pricing.BillingMode {
			case "", BillingModeToken:
				// Generic settlement intentionally uses token billing when the
				// exact channel model selects token mode. The token guard above
				// has already proved that price source complete.
				return true
			case BillingModePerRequest, BillingModeImage:
				if tier := pricing.GetTierByLabel(sizeTier); tier != nil &&
					validConfiguredPrice(tier.PerRequestPrice) {
					return true
				}
				if validConfiguredPrice(pricing.PerRequestPrice) {
					return true
				}
				// Settlement falls through to the exact catalog on a channel
				// tier miss, so admission may do the same below.
			default:
				return false
			}
		}
	}

	groupConfig := imagePriceConfigFromAPIKey(apiKey)
	_, ok := s.billingService.strictImageUnitPriceForPlatforms(platforms, model, sizeTier, groupConfig)
	return ok
}
