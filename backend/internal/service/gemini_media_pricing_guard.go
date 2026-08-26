package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/tidwall/gjson"
)

type geminiImageBillingIdentity struct {
	Model     string
	SizeTier  string
	InputSize string
	Count     int
}

// resolveGeminiImageBillingIdentity resolves billing from the exact model and
// Gemini request body that will be sent upstream. A public/request alias is not
// evidence that the account-mapped model is (or is not) an image SKU.
func resolveGeminiImageBillingIdentity(
	upstreamModel string,
	upstreamBody []byte,
) (geminiImageBillingIdentity, error) {
	upstreamModel = strings.TrimSpace(upstreamModel)
	if !isImageGenerationModel(upstreamModel) {
		return geminiImageBillingIdentity{}, nil
	}

	inputSize, err := extractGeminiImageInputSizeStrict(upstreamBody)
	if err != nil {
		return geminiImageBillingIdentity{}, err
	}
	sizeTier, err := normalizeGeminiImageSizeTierStrict(inputSize)
	if err != nil {
		return geminiImageBillingIdentity{}, err
	}
	return geminiImageBillingIdentity{
		Model:     upstreamModel,
		SizeTier:  sizeTier,
		InputSize: inputSize,
		Count:     1,
	}, nil
}

// normalizeGeminiImageSizeTierStrict deliberately accepts only the imageSize
// values supported by Gemini today. Missing/blank imageSize keeps the
// historical 2K billing default, but an explicit future or malformed tier must
// not silently inherit that price.
func normalizeGeminiImageSizeTierStrict(inputSize string) (string, error) {
	inputSize = strings.TrimSpace(inputSize)
	if inputSize == "" {
		return ImageBillingSize2K, nil
	}
	switch strings.ToUpper(inputSize) {
	case ImageBillingSize1K:
		return ImageBillingSize1K, nil
	case ImageBillingSize2K:
		return ImageBillingSize2K, nil
	case ImageBillingSize4K:
		return ImageBillingSize4K, nil
	default:
		return "", fmt.Errorf(
			"%w: Gemini imageSize %q has no configured image billing tier",
			ErrModelPricingUnavailable,
			inputSize,
		)
	}
}

// extractGeminiImageInputSizeStrict preserves duplicate JSON object members so
// pricing and the upstream parser cannot choose different imageSize values.
// Only the native Gemini wire path is inspected; compatibility requests call
// this helper after conversion, against the actual body sent upstream.
func extractGeminiImageInputSizeStrict(body []byte) (string, error) {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return "", fmt.Errorf(
			"%w: invalid Gemini image request body",
			ErrModelPricingUnavailable,
		)
	}

	root := parseRawJSONView(body)
	if !root.IsObject() {
		return "", fmt.Errorf(
			"%w: Gemini image request body must be an object",
			ErrModelPricingUnavailable,
		)
	}
	generationConfig, present, err := uniqueGeminiBillingObjectField(root, "generationConfig")
	if err != nil || !present {
		return "", err
	}
	imageConfig, present, err := uniqueGeminiBillingObjectField(generationConfig, "imageConfig")
	if err != nil || !present {
		return "", err
	}

	var imageSize gjson.Result
	count := 0
	imageConfig.ForEach(func(key, value gjson.Result) bool {
		if key.Str == "imageSize" {
			count++
			if count == 1 {
				imageSize = value
			}
		}
		return true
	})
	if count > 1 {
		return "", fmt.Errorf(
			"%w: duplicate Gemini generationConfig.imageConfig.imageSize fields make the billing tier ambiguous",
			ErrModelPricingUnavailable,
		)
	}
	if count == 0 {
		return "", nil
	}
	if imageSize.Type != gjson.String {
		return "", fmt.Errorf(
			"%w: Gemini generationConfig.imageConfig.imageSize must be a string",
			ErrModelPricingUnavailable,
		)
	}
	value := strings.TrimSpace(imageSize.String())
	if strings.ContainsRune(value, '\x00') {
		return "", fmt.Errorf(
			"%w: Gemini generationConfig.imageConfig.imageSize must not contain null characters",
			ErrModelPricingUnavailable,
		)
	}
	return value, nil
}

func uniqueGeminiBillingObjectField(
	parent gjson.Result,
	field string,
) (gjson.Result, bool, error) {
	var value gjson.Result
	count := 0
	parent.ForEach(func(key, candidate gjson.Result) bool {
		if key.Str == field {
			count++
			if count == 1 {
				value = candidate
			}
		}
		return true
	})
	if count > 1 {
		return gjson.Result{}, false, fmt.Errorf(
			"%w: duplicate Gemini %s fields make the image billing tier ambiguous",
			ErrModelPricingUnavailable,
			field,
		)
	}
	if count == 0 {
		return gjson.Result{}, false, nil
	}
	if !value.IsObject() {
		return gjson.Result{}, false, fmt.Errorf(
			"%w: Gemini %s must be an object",
			ErrModelPricingUnavailable,
			field,
		)
	}
	return value, true, nil
}

// validateResolvedGeminiTokenPricing rechecks token admission immediately
// before ForwardNative constructs an HTTP request. The handler records the
// original request model plus its single channel-mapping result in ctx; direct
// service callers are pinned to an identity mapping so an already-routed model
// is never passed through channel mapping again.
func (s *GeminiMessagesCompatService) validateResolvedGeminiTokenPricing(
	ctx context.Context,
	account *Account,
	routedModel string,
) error {
	if s == nil {
		return fmt.Errorf(
			"%w: Gemini gateway pricing guard unavailable",
			ErrModelPricingUnavailable,
		)
	}
	if !s.pricingGuardRequired && s.gatewayPricingGuard == nil {
		// Backward compatibility for isolated direct-struct tests. Production
		// construction always enables the guard.
		return nil
	}
	if s.gatewayPricingGuard == nil {
		return fmt.Errorf(
			"%w: Gemini gateway pricing guard unavailable",
			ErrModelPricingUnavailable,
		)
	}
	routedModel = strings.TrimSpace(routedModel)
	if _, ok := resolvedChannelPricingIdentityFromContext(ctx, routedModel); !ok {
		ctx = WithResolvedChannelPricingIdentity(ctx, routedModel, ChannelMappingResult{
			MappedModel: routedModel,
		})
	}
	ctx = WithFinalGeminiImagePricingGuard(ctx)
	return s.gatewayPricingGuard.ValidateUsagePricing(
		ctx,
		geminiPricingGuardAPIKey(ctx),
		account,
		routedModel,
	)
}

// validateResolvedGeminiImagePricing checks the exact account-mapped image SKU
// without invoking the mapping pipeline a second time. Wildcard mappings need
// not be idempotent, so feeding the already-mapped model back into
// ValidateUsagePricing could validate a different model than the one sent.
func (s *GeminiMessagesCompatService) validateResolvedGeminiImagePricing(
	ctx context.Context,
	account *Account,
	identity geminiImageBillingIdentity,
) error {
	if identity.Count == 0 {
		return nil
	}
	if s == nil {
		return fmt.Errorf(
			"%w: Gemini gateway pricing guard unavailable",
			ErrModelPricingUnavailable,
		)
	}
	if !s.pricingGuardRequired && s.gatewayPricingGuard == nil {
		// Backward compatibility for isolated direct-struct tests. Production
		// construction always enables the guard.
		return nil
	}
	if s.gatewayPricingGuard == nil {
		return fmt.Errorf(
			"%w: Gemini gateway pricing guard unavailable",
			ErrModelPricingUnavailable,
		)
	}
	return s.gatewayPricingGuard.validateExactResolvedImagePricing(ctx, account, identity)
}

// validateExactResolvedImagePricing is intentionally narrower than
// ValidateUsagePricing: the caller has already completed routing and account
// mapping, so only this exact media identity may be considered.
func (s *GatewayService) validateExactResolvedImagePricing(
	ctx context.Context,
	account *Account,
	identity geminiImageBillingIdentity,
) error {
	if identity.Count == 0 {
		return nil
	}
	if s == nil {
		return fmt.Errorf(
			"%w: gateway pricing guard unavailable",
			ErrModelPricingUnavailable,
		)
	}
	if !s.pricingGuardRequired && s.billingService == nil {
		return nil
	}
	if s.billingService == nil {
		return fmt.Errorf(
			"%w: gateway billing service unavailable",
			ErrModelPricingUnavailable,
		)
	}

	apiKey := geminiPricingGuardAPIKey(ctx)
	if s.hasResolvableImagePricingForPlatforms(
		ctx,
		identity.Model,
		identity.SizeTier,
		apiKey,
		pricingPlatformCandidates(apiKey, account),
	) {
		return nil
	}

	platform := PlatformGemini
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

func geminiPricingGuardAPIKey(ctx context.Context) *APIKey {
	if ctx == nil {
		return nil
	}
	group, _ := ctx.Value(ctxkey.Group).(*Group)
	if !IsGroupContextValid(group) || group.ID <= 0 {
		return nil
	}
	groupID := group.ID
	return &APIKey{GroupID: &groupID, Group: group}
}
