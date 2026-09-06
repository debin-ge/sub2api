package service

import (
	"errors"
	"strings"
	"time"
)

type VideoPricingPreviewAttributes struct {
	Provider             string     `json:"provider,omitempty"`
	Model                string     `json:"model,omitempty"`
	Operation            string     `json:"operation,omitempty"`
	Size                 string     `json:"size,omitempty"`
	Resolution           string     `json:"resolution,omitempty"`
	Seconds              int        `json:"seconds,omitempty"`
	MaximumOutputSeconds int        `json:"maximum_output_seconds,omitempty"`
	OutputSpecUnverified bool       `json:"output_spec_unverified,omitempty"`
	InputType            string     `json:"input_type,omitempty"`
	InputHasVideo        bool       `json:"input_has_video"`
	InputVideoSeconds    float64    `json:"input_video_seconds,omitempty"`
	GenerateAudio        *bool      `json:"generate_audio,omitempty"`
	RequestMode          string     `json:"request_mode,omitempty"`
	InferenceMode        string     `json:"inference_mode,omitempty"`
	Quality              string     `json:"quality,omitempty"`
	ServiceTier          string     `json:"service_tier,omitempty"`
	CustomerMultiplier   *float64   `json:"customer_multiplier,omitempty"`
	At                   *time.Time `json:"at,omitempty"`
}

type VideoPricingPreviewMismatch struct {
	Field    string `json:"field"`
	Expected any    `json:"expected,omitempty"`
	Actual   any    `json:"actual,omitempty"`
}

type VideoPricingPreviewRejectedRule struct {
	Key        string                        `json:"key"`
	Mismatches []VideoPricingPreviewMismatch `json:"mismatches"`
}

type VideoPricingPreviewResult struct {
	Matched              bool                              `json:"matched"`
	RuleKey              string                            `json:"rule_key,omitempty"`
	BillingUnit          string                            `json:"billing_unit,omitempty"`
	EstimatedUnits       float64                           `json:"estimated_units,omitempty"`
	MaximumUnits         float64                           `json:"maximum_units,omitempty"`
	EstimatedCost        float64                           `json:"estimated_cost,omitempty"`
	ErrorCode            string                            `json:"error_code,omitempty"`
	NormalizedAttributes VideoPricingPreviewAttributes     `json:"normalized_attributes"`
	RejectedRules        []VideoPricingPreviewRejectedRule `json:"rejected_rules"`
}

func (a VideoPricingPreviewAttributes) pricingAttributes() VideoPricingAttributes {
	at := time.Time{}
	if a.At != nil {
		at = a.At.UTC()
	}
	return VideoPricingAttributes{
		Provider: a.Provider, Model: a.Model, Operation: a.Operation,
		Size: a.Size, Resolution: a.Resolution, Seconds: a.Seconds,
		MaximumOutputSeconds: a.MaximumOutputSeconds, OutputSpecUnverified: a.OutputSpecUnverified,
		InputType: a.InputType, InputHasVideo: a.InputHasVideo, InputVideoSeconds: a.InputVideoSeconds,
		AudioEnabled: a.GenerateAudio, RequestMode: a.RequestMode, InferenceMode: a.InferenceMode,
		Quality: a.Quality, ServiceTier: a.ServiceTier, CustomerMultiplier: a.CustomerMultiplier, At: at,
	}
}

func videoPreviewAttributes(attrs VideoPricingAttributes) VideoPricingPreviewAttributes {
	at := attrs.At.UTC()
	return VideoPricingPreviewAttributes{
		Provider: attrs.Provider, Model: attrs.Model, Operation: attrs.Operation,
		Size: attrs.Size, Resolution: attrs.Resolution, Seconds: attrs.Seconds,
		MaximumOutputSeconds: attrs.MaximumOutputSeconds, OutputSpecUnverified: attrs.OutputSpecUnverified,
		InputType: attrs.InputType, InputHasVideo: attrs.InputHasVideo, InputVideoSeconds: attrs.InputVideoSeconds,
		GenerateAudio: attrs.AudioEnabled, RequestMode: attrs.RequestMode, InferenceMode: attrs.InferenceMode,
		Quality: attrs.Quality, ServiceTier: attrs.ServiceTier, CustomerMultiplier: attrs.CustomerMultiplier, At: &at,
	}
}

func PreviewVideoPricingConfig(config *VideoPricingConfig, input VideoPricingPreviewAttributes) (*VideoPricingPreviewResult, error) {
	if err := ValidateVideoPricingConfig(config); err != nil {
		return nil, err
	}
	attrs := input.pricingAttributes()
	if attrs.At.IsZero() {
		attrs.At = time.Now().UTC()
	}
	normalized, err := normalizeVideoPricingAttributes(config, attrs)
	result := &VideoPricingPreviewResult{RejectedRules: []VideoPricingPreviewRejectedRule{}}
	if err != nil {
		result.ErrorCode = videoPricingPreviewErrorCode(err)
		result.NormalizedAttributes = videoPreviewAttributes(attrs)
		return result, nil
	}
	result.NormalizedAttributes = videoPreviewAttributes(normalized)
	for _, rule := range config.Rules {
		mismatches := videoPricingRulePreviewMismatches(rule, normalized)
		if len(mismatches) > 0 {
			result.RejectedRules = append(result.RejectedRules, VideoPricingPreviewRejectedRule{
				Key: strings.TrimSpace(rule.Key), Mismatches: mismatches,
			})
		}
	}
	quote, err := ResolveVideoPricingConfig(config, normalized)
	if err != nil {
		result.ErrorCode = videoPricingPreviewErrorCode(err)
		return result, nil
	}
	result.Matched = true
	result.RuleKey = quote.RuleKey
	result.BillingUnit = quote.BillingUnit
	result.EstimatedUnits = quote.EstimatedUnits
	result.MaximumUnits = quote.MaximumUnits
	result.EstimatedCost = quote.EstimatedCost
	return result, nil
}

func videoPricingPreviewErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrVideoPricingRuleMissing):
		return "video_pricing_rule_missing"
	case errors.Is(err, ErrVideoPricingAmbiguous):
		return "video_pricing_ambiguous"
	case errors.Is(err, ErrVideoPricingEstimatorMissing):
		return "video_pricing_estimator_missing"
	case errors.Is(err, ErrVideoPricingResolutionMissing):
		return "video_pricing_resolution_missing"
	case errors.Is(err, ErrVideoSourceSpecUnavailable):
		return "video_source_spec_unavailable"
	default:
		return "video_pricing_invalid"
	}
}

func videoPricingRulePreviewMismatches(rule VideoPricingRule, attrs VideoPricingAttributes) []VideoPricingPreviewMismatch {
	mismatches := make([]VideoPricingPreviewMismatch, 0)
	appendString := func(field string, expected []string, actual string) {
		if len(expected) > 0 && !matchesVideoString(expected, actual) {
			mismatches = append(mismatches, VideoPricingPreviewMismatch{Field: field, Expected: expected, Actual: actual})
		}
	}
	if rule.ValidFrom != nil && attrs.At.Before(*rule.ValidFrom) {
		mismatches = append(mismatches, VideoPricingPreviewMismatch{Field: "valid_from", Expected: rule.ValidFrom, Actual: attrs.At})
	}
	if rule.ValidUntil != nil && !attrs.At.Before(*rule.ValidUntil) {
		mismatches = append(mismatches, VideoPricingPreviewMismatch{Field: "valid_until", Expected: rule.ValidUntil, Actual: attrs.At})
	}
	if attrs.OutputSpecUnverified && rule.BillingUnit != VideoBillingUnitRequest {
		mismatches = append(mismatches, VideoPricingPreviewMismatch{Field: "output_spec_verified", Expected: true, Actual: false})
	}
	c := rule.Conditions
	appendString("provider", c.Providers, attrs.Provider)
	appendString("operation", c.Operations, attrs.Operation)
	appendString("size", c.Sizes, attrs.Size)
	appendString("resolution", c.Resolutions, attrs.Resolution)
	if len(c.Seconds) > 0 && !matchesVideoInt(c.Seconds, attrs.Seconds) {
		mismatches = append(mismatches, VideoPricingPreviewMismatch{Field: "seconds", Expected: c.Seconds, Actual: attrs.Seconds})
	}
	appendString("input_type", c.InputTypes, attrs.InputType)
	if c.InputHasVideo != nil && *c.InputHasVideo != attrs.InputHasVideo {
		mismatches = append(mismatches, VideoPricingPreviewMismatch{Field: "input_has_video", Expected: *c.InputHasVideo, Actual: attrs.InputHasVideo})
	}
	audio := videoAudioCondition(c)
	if audio != nil && (attrs.AudioEnabled == nil || *audio != *attrs.AudioEnabled) {
		var actual any
		if attrs.AudioEnabled != nil {
			actual = *attrs.AudioEnabled
		}
		mismatches = append(mismatches, VideoPricingPreviewMismatch{Field: "generate_audio", Expected: *audio, Actual: actual})
	}
	appendString("request_mode", c.RequestModes, attrs.RequestMode)
	appendString("inference_mode", c.InferenceModes, attrs.InferenceMode)
	appendString("quality", c.Qualities, attrs.Quality)
	appendString("service_tier", c.ServiceTiers, attrs.ServiceTier)
	return mismatches
}
