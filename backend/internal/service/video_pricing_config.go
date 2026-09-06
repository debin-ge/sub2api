package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	VideoPricingConfigVersion = 1

	VideoEstimatorPixelFrame           = "pixel_frame"
	VideoEstimatorFixedTokensPerSecond = "fixed_tokens_per_second"
	VideoEstimatorFixedMaxUnits        = "fixed_max_units"

	VideoTokenScopeOutputOnly      = "output_only"
	VideoTokenScopeInputPlusOutput = "input_plus_output"

	VideoRequestModeStandard  = "standard"
	VideoRequestModeBatch     = "batch"
	VideoInferenceModeOnline  = "online"
	VideoInferenceModeOffline = "offline"

	maxVideoPricingRules       = 128
	maxVideoPricingResolutions = 32
	maxVideoPricingEstimators  = 16
	maxVideoPricingNameLength  = 128
)

var videoDimensionPattern = regexp.MustCompile(`^([1-9][0-9]{0,5})[xX]([1-9][0-9]{0,5})$`)

// VideoPricingConfig is stored inside the existing model-price JSON payload.
// The profile is intentionally model-agnostic: every model-specific fact lives
// in data (rules, resolutions, and estimators), never in Go branches.
type VideoPricingConfig struct {
	Version     int                            `json:"version"`
	Enabled     bool                           `json:"enabled"`
	Currency    string                         `json:"currency"`
	Defaults    VideoPricingDefaults           `json:"defaults,omitempty"`
	Resolutions map[string]VideoResolutionSpec `json:"resolutions,omitempty"`
	Estimators  map[string]VideoUsageEstimator `json:"estimators,omitempty"`
	Rules       []VideoPricingRule             `json:"rules,omitempty"`
}

// UnmarshalJSON keeps video pricing strict even when it comes from the synced
// catalog, whose outer model entries deliberately accept extra LiteLLM fields.
func (c *VideoPricingConfig) UnmarshalJSON(data []byte) error {
	type plain VideoPricingConfig
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var decoded plain
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return err
	}
	*c = VideoPricingConfig(decoded)
	return nil
}

type VideoPricingDefaults struct {
	Resolution    string `json:"resolution,omitempty"`
	GenerateAudio bool   `json:"generate_audio,omitempty"`
	RequestMode   string `json:"request_mode,omitempty"`
	InferenceMode string `json:"inference_mode,omitempty"`
}

type VideoResolutionSpec struct {
	Sizes []string `json:"sizes"`
}

type VideoUsageEstimator struct {
	Type                 string                  `json:"type"`
	TokenScope           string                  `json:"token_scope,omitempty"`
	FPS                  float64                 `json:"fps,omitempty"`
	Divisor              float64                 `json:"divisor,omitempty"`
	TokensPerSecond      float64                 `json:"tokens_per_second,omitempty"`
	MaxUnits             float64                 `json:"max_units,omitempty"`
	MaxInputVideoSeconds float64                 `json:"max_input_video_seconds,omitempty"`
	MinimumUnits         []VideoMinimumUnitsRule `json:"minimum_units,omitempty"`
}

type VideoMinimumUnitsRule struct {
	Units      float64                `json:"units"`
	Conditions VideoPricingConditions `json:"conditions,omitempty"`
}

type VideoPricingRule struct {
	Key          string                 `json:"key"`
	BillingUnit  string                 `json:"billing_unit"`
	UnitPriceUSD float64                `json:"unit_price_usd"`
	Estimator    string                 `json:"estimator,omitempty"`
	Conditions   VideoPricingConditions `json:"conditions,omitempty"`
	Priority     int                    `json:"priority,omitempty"`
	ValidFrom    *time.Time             `json:"valid_from,omitempty"`
	ValidUntil   *time.Time             `json:"valid_until,omitempty"`
}

type VideoPricingConditions struct {
	Providers      []string `json:"providers,omitempty"`
	Operations     []string `json:"operations,omitempty"`
	Sizes          []string `json:"sizes,omitempty"`
	Resolutions    []string `json:"resolutions,omitempty"`
	Seconds        []int    `json:"seconds,omitempty"`
	InputTypes     []string `json:"input_types,omitempty"`
	InputHasVideo  *bool    `json:"input_has_video,omitempty"`
	GenerateAudio  *bool    `json:"generate_audio,omitempty"`
	AudioEnabled   *bool    `json:"audio_enabled,omitempty"` // compatibility with channel rules
	RequestModes   []string `json:"request_modes,omitempty"`
	InferenceModes []string `json:"inference_modes,omitempty"`
	Qualities      []string `json:"qualities,omitempty"`
	ServiceTiers   []string `json:"service_tiers,omitempty"`
}

type videoPricingValidationError struct {
	field  string
	detail string
}

func (e *videoPricingValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.field, e.detail)
}

func (e *videoPricingValidationError) Unwrap() error {
	return ErrVideoPricingInvalid
}

func ensureSingleJSONValue(decoder *json.Decoder) error {
	var trailing any
	err := decoder.Decode(&trailing)
	if err == io.EOF {
		return nil
	}
	if err == nil {
		return fmt.Errorf("multiple JSON values")
	}
	return err
}

func cloneVideoPricingConfig(config *VideoPricingConfig) *VideoPricingConfig {
	if config == nil {
		return nil
	}
	clone := *config
	clone.Resolutions = make(map[string]VideoResolutionSpec, len(config.Resolutions))
	for name, spec := range config.Resolutions {
		spec.Sizes = append([]string(nil), spec.Sizes...)
		clone.Resolutions[name] = spec
	}
	clone.Estimators = make(map[string]VideoUsageEstimator, len(config.Estimators))
	for name, estimator := range config.Estimators {
		estimator.MinimumUnits = append([]VideoMinimumUnitsRule(nil), estimator.MinimumUnits...)
		for i := range estimator.MinimumUnits {
			estimator.MinimumUnits[i].Conditions = cloneVideoPricingConditions(estimator.MinimumUnits[i].Conditions)
		}
		clone.Estimators[name] = estimator
	}
	clone.Rules = append([]VideoPricingRule(nil), config.Rules...)
	for i := range clone.Rules {
		clone.Rules[i].Conditions = cloneVideoPricingConditions(config.Rules[i].Conditions)
	}
	return &clone
}

func cloneVideoPricingConditions(c VideoPricingConditions) VideoPricingConditions {
	c.Providers = append([]string(nil), c.Providers...)
	c.Operations = append([]string(nil), c.Operations...)
	c.Sizes = append([]string(nil), c.Sizes...)
	c.Resolutions = append([]string(nil), c.Resolutions...)
	c.Seconds = append([]int(nil), c.Seconds...)
	c.InputTypes = append([]string(nil), c.InputTypes...)
	c.RequestModes = append([]string(nil), c.RequestModes...)
	c.InferenceModes = append([]string(nil), c.InferenceModes...)
	c.Qualities = append([]string(nil), c.Qualities...)
	c.ServiceTiers = append([]string(nil), c.ServiceTiers...)
	if c.InputHasVideo != nil {
		value := *c.InputHasVideo
		c.InputHasVideo = &value
	}
	if c.GenerateAudio != nil {
		value := *c.GenerateAudio
		c.GenerateAudio = &value
	}
	if c.AudioEnabled != nil {
		value := *c.AudioEnabled
		c.AudioEnabled = &value
	}
	return c
}

func ValidateVideoPricingConfig(config *VideoPricingConfig) error {
	if config == nil {
		return nil
	}
	if config.Version != VideoPricingConfigVersion {
		return videoPricingConfigError("version", "version must be 1")
	}
	if strings.ToUpper(strings.TrimSpace(config.Currency)) != ModelPriceCurrencyUSD {
		return videoPricingConfigError("currency", "currency must be USD")
	}
	if len(config.Resolutions) > maxVideoPricingResolutions {
		return videoPricingConfigError("resolutions", "too many resolution definitions")
	}
	if len(config.Estimators) > maxVideoPricingEstimators {
		return videoPricingConfigError("estimators", "too many estimators")
	}
	if len(config.Rules) > maxVideoPricingRules {
		return videoPricingConfigError("rules", "too many pricing rules")
	}
	if !config.Enabled {
		return nil
	}
	if len(config.Rules) == 0 {
		return videoPricingConfigError("rules", "enabled video pricing requires at least one rule")
	}

	resolutionNames := make(map[string]string, len(config.Resolutions))
	sizeOwners := make(map[string]string)
	for name, spec := range config.Resolutions {
		trimmed, normalized, err := validateVideoPricingName(name, "resolutions")
		if err != nil {
			return err
		}
		if prior := resolutionNames[normalized]; prior != "" && prior != trimmed {
			return videoPricingConfigError("resolutions", "resolution names must be unique ignoring case")
		}
		resolutionNames[normalized] = trimmed
		if len(spec.Sizes) == 0 {
			return videoPricingConfigError("resolutions."+trimmed+".sizes", "a resolution requires at least one size")
		}
		seenSizes := make(map[string]struct{}, len(spec.Sizes))
		for _, rawSize := range spec.Sizes {
			size, _, _, ok := parseVideoDimensions(rawSize)
			if !ok {
				return videoPricingConfigError("resolutions."+trimmed+".sizes", "size must use positive WIDTHxHEIGHT dimensions")
			}
			if _, duplicate := seenSizes[size]; duplicate {
				return videoPricingConfigError("resolutions."+trimmed+".sizes", "duplicate size")
			}
			seenSizes[size] = struct{}{}
			if owner := sizeOwners[size]; owner != "" && !strings.EqualFold(owner, trimmed) {
				return videoPricingConfigError("resolutions."+trimmed+".sizes", "a size cannot belong to multiple resolutions")
			}
			sizeOwners[size] = trimmed
		}
	}
	if value := strings.TrimSpace(config.Defaults.Resolution); value != "" {
		if _, ok := resolutionNames[strings.ToLower(value)]; !ok {
			return videoPricingConfigError("defaults.resolution", "default resolution is not defined")
		}
	}
	if err := validateVideoMode(config.Defaults.RequestMode, "defaults.request_mode", VideoRequestModeStandard, VideoRequestModeBatch); err != nil {
		return err
	}
	if err := validateVideoMode(config.Defaults.InferenceMode, "defaults.inference_mode", VideoInferenceModeOnline, VideoInferenceModeOffline); err != nil {
		return err
	}

	estimatorNames := make(map[string]string, len(config.Estimators))
	for name, estimator := range config.Estimators {
		trimmed, normalized, err := validateVideoPricingName(name, "estimators")
		if err != nil {
			return err
		}
		if prior := estimatorNames[normalized]; prior != "" && prior != trimmed {
			return videoPricingConfigError("estimators", "estimator names must be unique ignoring case")
		}
		estimatorNames[normalized] = trimmed
		if err := validateVideoUsageEstimator(trimmed, estimator, resolutionNames, sizeOwners); err != nil {
			return err
		}
	}

	ruleKeys := make(map[string]struct{}, len(config.Rules))
	for index := range config.Rules {
		rule := &config.Rules[index]
		path := fmt.Sprintf("rules[%d]", index)
		_, normalizedKey, err := validateVideoPricingName(rule.Key, path+".key")
		if err != nil {
			return err
		}
		if _, duplicate := ruleKeys[normalizedKey]; duplicate {
			return videoPricingConfigError(path+".key", "rule keys must be unique ignoring case")
		}
		ruleKeys[normalizedKey] = struct{}{}
		if !isVideoBillingUnit(rule.BillingUnit) {
			return videoPricingConfigError(path+".billing_unit", "unsupported billing unit")
		}
		if !finiteNonNegative(rule.UnitPriceUSD) {
			return videoPricingConfigError(path+".unit_price_usd", "unit price must be finite and non-negative")
		}
		if rule.ValidFrom != nil && rule.ValidUntil != nil && !rule.ValidFrom.Before(*rule.ValidUntil) {
			return videoPricingConfigError(path+".valid_until", "valid_until must be after valid_from")
		}
		if err := validateVideoPricingConditions(rule.Conditions, path+".conditions", resolutionNames, sizeOwners); err != nil {
			return err
		}
		estimator := strings.ToLower(strings.TrimSpace(rule.Estimator))
		if strings.EqualFold(strings.TrimSpace(rule.BillingUnit), VideoBillingUnitVideoToken) {
			if estimator == "" {
				return videoPricingConfigError(path+".estimator", "video_token rules require an estimator")
			}
			if _, ok := estimatorNames[estimator]; !ok {
				return videoPricingConfigError(path+".estimator", "referenced estimator is not defined")
			}
		} else if estimator != "" {
			if _, ok := estimatorNames[estimator]; !ok {
				return videoPricingConfigError(path+".estimator", "referenced estimator is not defined")
			}
		}
	}
	for i := 0; i < len(config.Rules); i++ {
		for j := i + 1; j < len(config.Rules); j++ {
			left, right := config.Rules[i], config.Rules[j]
			if left.Priority != right.Priority || videoPricingSpecificity(left.Conditions) != videoPricingSpecificity(right.Conditions) {
				continue
			}
			if videoPricingWindowsOverlap(left.ValidFrom, left.ValidUntil, right.ValidFrom, right.ValidUntil) &&
				videoPricingConditionsOverlap(left.Conditions, right.Conditions, config) {
				return videoPricingConfigError("rules", fmt.Sprintf("rules %q and %q overlap at equal priority and specificity", left.Key, right.Key))
			}
		}
	}
	return nil
}

func validateVideoUsageEstimator(name string, estimator VideoUsageEstimator, resolutions map[string]string, sizes map[string]string) error {
	path := "estimators." + name
	switch strings.ToLower(strings.TrimSpace(estimator.Type)) {
	case VideoEstimatorPixelFrame:
		scope := strings.ToLower(strings.TrimSpace(estimator.TokenScope))
		if scope != VideoTokenScopeOutputOnly && scope != VideoTokenScopeInputPlusOutput {
			return videoPricingConfigError(path+".token_scope", "pixel_frame requires output_only or input_plus_output")
		}
		if !finitePositive(estimator.FPS) {
			return videoPricingConfigError(path+".fps", "fps must be finite and positive")
		}
		if !finitePositive(estimator.Divisor) {
			return videoPricingConfigError(path+".divisor", "divisor must be finite and positive")
		}
		if scope == VideoTokenScopeInputPlusOutput && !finitePositive(estimator.MaxInputVideoSeconds) {
			return videoPricingConfigError(path+".max_input_video_seconds", "input_plus_output requires a positive input-video bound")
		}
	case VideoEstimatorFixedTokensPerSecond:
		if !finitePositive(estimator.TokensPerSecond) {
			return videoPricingConfigError(path+".tokens_per_second", "tokens_per_second must be finite and positive")
		}
	case VideoEstimatorFixedMaxUnits:
		if !finitePositive(estimator.MaxUnits) {
			return videoPricingConfigError(path+".max_units", "max_units must be finite and positive")
		}
	default:
		return videoPricingConfigError(path+".type", "unsupported estimator type")
	}
	for index, minimum := range estimator.MinimumUnits {
		minimumPath := fmt.Sprintf("%s.minimum_units[%d]", path, index)
		if !finitePositive(minimum.Units) {
			return videoPricingConfigError(minimumPath+".units", "minimum units must be finite and positive")
		}
		if err := validateVideoPricingConditions(minimum.Conditions, minimumPath+".conditions", resolutions, sizes); err != nil {
			return err
		}
	}
	return nil
}

func validateVideoPricingConditions(c VideoPricingConditions, path string, resolutions map[string]string, sizes map[string]string) error {
	if err := validateVideoPricingConditionValues(c, path); err != nil {
		return err
	}
	for _, value := range c.Resolutions {
		if _, ok := resolutions[strings.ToLower(strings.TrimSpace(value))]; !ok {
			return videoPricingConfigError(path+".resolutions", "referenced resolution is not defined")
		}
	}
	for _, value := range c.Sizes {
		size, _, _, ok := parseVideoDimensions(value)
		if !ok {
			return videoPricingConfigError(path+".sizes", "size must use positive WIDTHxHEIGHT dimensions")
		}
		if _, declared := sizes[size]; !declared {
			return videoPricingConfigError(path+".sizes", "size is not declared by a resolution")
		}
	}
	return nil
}

func validateVideoPricingConditionValues(c VideoPricingConditions, path string) error {
	seenSizes := make(map[string]struct{}, len(c.Sizes))
	for _, value := range c.Sizes {
		size, _, _, ok := parseVideoDimensions(value)
		if !ok {
			return videoPricingConfigError(path+".sizes", "size must use positive WIDTHxHEIGHT dimensions")
		}
		if _, duplicate := seenSizes[size]; duplicate {
			return videoPricingConfigError(path+".sizes", "duplicate condition value")
		}
		seenSizes[size] = struct{}{}
	}
	seenSeconds := make(map[int]struct{}, len(c.Seconds))
	for _, seconds := range c.Seconds {
		if seconds <= 0 {
			return videoPricingConfigError(path+".seconds", "seconds must be positive")
		}
		if _, duplicate := seenSeconds[seconds]; duplicate {
			return videoPricingConfigError(path+".seconds", "duplicate condition value")
		}
		seenSeconds[seconds] = struct{}{}
	}
	for _, value := range c.RequestModes {
		if err := validateVideoMode(value, path+".request_modes", VideoRequestModeStandard, VideoRequestModeBatch); err != nil {
			return err
		}
	}
	for _, value := range c.InferenceModes {
		if err := validateVideoMode(value, path+".inference_modes", VideoInferenceModeOnline, VideoInferenceModeOffline); err != nil {
			return err
		}
	}
	for field, values := range map[string][]string{
		"providers": c.Providers, "operations": c.Operations, "input_types": c.InputTypes,
		"resolutions": c.Resolutions, "request_modes": c.RequestModes,
		"inference_modes": c.InferenceModes, "qualities": c.Qualities, "service_tiers": c.ServiceTiers,
	} {
		seen := make(map[string]struct{}, len(values))
		for _, value := range values {
			trimmed := strings.TrimSpace(value)
			if trimmed == "" || len(trimmed) > maxVideoPricingNameLength {
				return videoPricingConfigError(path+"."+field, "condition values must be non-empty and at most 128 characters")
			}
			normalized := strings.ToLower(trimmed)
			if _, duplicate := seen[normalized]; duplicate {
				return videoPricingConfigError(path+"."+field, "duplicate condition value")
			}
			seen[normalized] = struct{}{}
		}
	}
	if c.GenerateAudio != nil && c.AudioEnabled != nil && *c.GenerateAudio != *c.AudioEnabled {
		return videoPricingConfigError(path+".generate_audio", "generate_audio conflicts with audio_enabled")
	}
	return nil
}

func validateVideoPricingName(value, field string) (string, string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > maxVideoPricingNameLength {
		return "", "", videoPricingConfigError(field, "value is required and must not exceed 128 characters")
	}
	return trimmed, strings.ToLower(trimmed), nil
}

func validateVideoMode(value, field string, allowed ...string) error {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil
	}
	for _, candidate := range allowed {
		if value == candidate {
			return nil
		}
	}
	return videoPricingConfigError(field, "unsupported mode")
}

func videoPricingConfigError(field, message string) error {
	return &videoPricingValidationError{field: field, detail: message}
}

func finiteNonNegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func finitePositive(value float64) bool {
	return value > 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func parseVideoDimensions(raw string) (string, int64, int64, bool) {
	match := videoDimensionPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return "", 0, 0, false
	}
	width, errWidth := strconv.ParseInt(match[1], 10, 64)
	height, errHeight := strconv.ParseInt(match[2], 10, 64)
	if errWidth != nil || errHeight != nil || width <= 0 || height <= 0 {
		return "", 0, 0, false
	}
	return fmt.Sprintf("%dx%d", width, height), width, height, true
}

func videoPricingSpecificity(c VideoPricingConditions) int {
	specificity := 0
	for _, values := range [][]string{c.Providers, c.Operations, c.Sizes, c.Resolutions, c.InputTypes, c.RequestModes, c.InferenceModes, c.Qualities, c.ServiceTiers} {
		if len(values) > 0 {
			specificity++
		}
	}
	if len(c.Seconds) > 0 {
		specificity++
	}
	if c.InputHasVideo != nil {
		specificity++
	}
	if c.GenerateAudio != nil || c.AudioEnabled != nil {
		specificity++
	}
	return specificity
}

func videoPricingWindowsOverlap(aFrom, aUntil, bFrom, bUntil *time.Time) bool {
	if aUntil != nil && bFrom != nil && !bFrom.Before(*aUntil) {
		return false
	}
	if bUntil != nil && aFrom != nil && !aFrom.Before(*bUntil) {
		return false
	}
	return true
}

func videoPricingConditionsOverlap(left, right VideoPricingConditions, config *VideoPricingConfig) bool {
	if !videoStringSetsOverlap(left.Providers, right.Providers) ||
		!videoStringSetsOverlap(left.Operations, right.Operations) ||
		!videoStringSetsOverlap(left.InputTypes, right.InputTypes) ||
		!videoStringSetsOverlap(left.RequestModes, right.RequestModes) ||
		!videoStringSetsOverlap(left.InferenceModes, right.InferenceModes) ||
		!videoStringSetsOverlap(left.Qualities, right.Qualities) ||
		!videoStringSetsOverlap(left.ServiceTiers, right.ServiceTiers) ||
		!videoIntSetsOverlap(left.Seconds, right.Seconds) ||
		!videoBoolConditionsOverlap(left.InputHasVideo, right.InputHasVideo) ||
		!videoBoolConditionsOverlap(videoAudioCondition(left), videoAudioCondition(right)) {
		return false
	}
	return videoSizeResolutionConditionsOverlap(left, right, config)
}

func videoAudioCondition(c VideoPricingConditions) *bool {
	if c.GenerateAudio != nil {
		return c.GenerateAudio
	}
	return c.AudioEnabled
}

func videoStringSetsOverlap(left, right []string) bool {
	if len(left) == 0 || len(right) == 0 {
		return true
	}
	seen := make(map[string]struct{}, len(left))
	for _, value := range left {
		seen[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[strings.ToLower(strings.TrimSpace(value))]; ok {
			return true
		}
	}
	return false
}

func videoIntSetsOverlap(left, right []int) bool {
	if len(left) == 0 || len(right) == 0 {
		return true
	}
	seen := make(map[int]struct{}, len(left))
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[value]; ok {
			return true
		}
	}
	return false
}

func videoBoolConditionsOverlap(left, right *bool) bool {
	return left == nil || right == nil || *left == *right
}

func videoSizeResolutionConditionsOverlap(left, right VideoPricingConditions, config *VideoPricingConfig) bool {
	if len(left.Sizes) == 0 && len(right.Sizes) == 0 && len(left.Resolutions) == 0 && len(right.Resolutions) == 0 {
		return true
	}
	if config == nil || len(config.Resolutions) == 0 {
		return videoStringSetsOverlap(left.Sizes, right.Sizes) && videoStringSetsOverlap(left.Resolutions, right.Resolutions)
	}
	for resolution, spec := range config.Resolutions {
		if !matchesVideoString(left.Resolutions, resolution) || !matchesVideoString(right.Resolutions, resolution) {
			continue
		}
		for _, size := range spec.Sizes {
			if matchesVideoString(left.Sizes, size) && matchesVideoString(right.Sizes, size) {
				return true
			}
		}
	}
	return false
}

func (c VideoPricingConditions) matches(attrs VideoPricingAttributes) bool {
	if attrs.OutputSpecUnverified && c.needsVerifiedOutput() {
		return false
	}
	audio := videoAudioCondition(c)
	return matchesVideoString(c.Providers, attrs.Provider) &&
		matchesVideoString(c.Operations, attrs.Operation) &&
		matchesVideoString(c.Sizes, attrs.Size) &&
		matchesVideoString(c.Resolutions, attrs.Resolution) &&
		matchesVideoInt(c.Seconds, attrs.Seconds) &&
		matchesVideoString(c.InputTypes, attrs.InputType) &&
		(c.InputHasVideo == nil || *c.InputHasVideo == attrs.InputHasVideo) &&
		(audio == nil || (attrs.AudioEnabled != nil && *audio == *attrs.AudioEnabled)) &&
		matchesVideoString(c.RequestModes, attrs.RequestMode) &&
		matchesVideoString(c.InferenceModes, attrs.InferenceMode) &&
		matchesVideoString(c.Qualities, attrs.Quality) &&
		matchesVideoString(c.ServiceTiers, attrs.ServiceTier)
}

func (c VideoPricingConditions) needsVerifiedOutput() bool {
	return len(c.Sizes) > 0 || len(c.Resolutions) > 0 || len(c.Seconds) > 0 ||
		c.GenerateAudio != nil || c.AudioEnabled != nil || len(c.Qualities) > 0 || len(c.ServiceTiers) > 0
}

func normalizeVideoPricingAttributes(config *VideoPricingConfig, attrs VideoPricingAttributes) (VideoPricingAttributes, error) {
	if config == nil {
		return attrs, ErrVideoPricingMissing
	}
	if attrs.OutputSpecUnverified {
		attrs.Size, attrs.Resolution, attrs.Seconds, attrs.MaximumOutputSeconds = "", "", 0, 0
		attrs.AudioEnabled, attrs.Quality, attrs.ServiceTier = nil, "", ""
		attrs.InputVideoSeconds, attrs.EstimatedVideoTokens, attrs.MaximumUnits = 0, 0, 0
		return attrs, nil
	}
	attrs.Size = strings.TrimSpace(attrs.Size)
	attrs.RequestMode = strings.ToLower(strings.TrimSpace(attrs.RequestMode))
	if attrs.RequestMode == "" {
		attrs.RequestMode = strings.ToLower(strings.TrimSpace(config.Defaults.RequestMode))
		if attrs.RequestMode == "" {
			attrs.RequestMode = VideoRequestModeStandard
		}
	}
	attrs.InferenceMode = strings.ToLower(strings.TrimSpace(attrs.InferenceMode))
	if attrs.InferenceMode == "" {
		attrs.InferenceMode = strings.ToLower(strings.TrimSpace(config.Defaults.InferenceMode))
		if attrs.InferenceMode == "" {
			attrs.InferenceMode = VideoInferenceModeOnline
		}
	}
	if attrs.AudioEnabled == nil {
		value := config.Defaults.GenerateAudio
		attrs.AudioEnabled = &value
	}
	if len(config.Resolutions) == 0 && strings.TrimSpace(attrs.Size) == "" && strings.TrimSpace(attrs.Resolution) == "" {
		return attrs, nil
	}

	resolution := strings.TrimSpace(attrs.Resolution)
	if attrs.Size != "" {
		if normalizedSize, _, _, dimensions := parseVideoDimensions(attrs.Size); dimensions {
			attrs.Size = normalizedSize
			matched := ""
			for name, spec := range config.Resolutions {
				if matchesVideoString(spec.Sizes, normalizedSize) {
					if matched != "" && !strings.EqualFold(matched, name) {
						return attrs, ErrVideoPricingResolutionMissing
					}
					matched = name
				}
			}
			if matched == "" {
				return attrs, ErrVideoPricingResolutionMissing
			}
			resolution = matched
		} else if _, _, specExists := videoResolutionSpec(config, attrs.Size); specExists {
			resolution = attrs.Size
		} else {
			return attrs, ErrVideoPricingResolutionMissing
		}
	}
	if resolution == "" {
		resolution = strings.TrimSpace(config.Defaults.Resolution)
	}
	name, _, ok := videoResolutionSpec(config, resolution)
	if !ok {
		return attrs, ErrVideoPricingResolutionMissing
	}
	attrs.Resolution = name
	return attrs, nil
}

func videoResolutionSpec(config *VideoPricingConfig, name string) (string, VideoResolutionSpec, bool) {
	for configuredName, spec := range config.Resolutions {
		if strings.EqualFold(strings.TrimSpace(configuredName), strings.TrimSpace(name)) {
			return configuredName, spec, true
		}
	}
	return "", VideoResolutionSpec{}, false
}

type videoProfileCandidate struct {
	rule        VideoPricingRule
	specificity int
}

func ResolveVideoPricingConfig(config *VideoPricingConfig, attrs VideoPricingAttributes) (*VideoPriceQuote, error) {
	if config == nil || !config.Enabled {
		return nil, ErrVideoPricingMissing
	}
	if err := ValidateVideoPricingConfig(config); err != nil {
		return nil, err
	}
	normalized, err := normalizeVideoPricingAttributes(config, attrs)
	if err != nil {
		return nil, err
	}
	if normalized.At.IsZero() {
		normalized.At = time.Now().UTC()
	}
	candidates := make([]videoProfileCandidate, 0, len(config.Rules))
	for _, rule := range config.Rules {
		if normalized.OutputSpecUnverified && rule.BillingUnit != VideoBillingUnitRequest {
			continue
		}
		if rule.ValidFrom != nil && normalized.At.Before(*rule.ValidFrom) {
			continue
		}
		if rule.ValidUntil != nil && !normalized.At.Before(*rule.ValidUntil) {
			continue
		}
		if rule.Conditions.matches(normalized) {
			candidates = append(candidates, videoProfileCandidate{rule: rule, specificity: videoPricingSpecificity(rule.Conditions)})
		}
	}
	if len(candidates) == 0 {
		if normalized.OutputSpecUnverified {
			return nil, ErrVideoSourceSpecUnavailable
		}
		return nil, ErrVideoPricingRuleMissing
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].rule.Priority != candidates[j].rule.Priority {
			return candidates[i].rule.Priority > candidates[j].rule.Priority
		}
		if candidates[i].specificity != candidates[j].specificity {
			return candidates[i].specificity > candidates[j].specificity
		}
		return candidates[i].rule.Key < candidates[j].rule.Key
	})
	best := candidates[0]
	if len(candidates) > 1 && candidates[1].rule.Priority == best.rule.Priority && candidates[1].specificity == best.specificity {
		return nil, ErrVideoPricingAmbiguous
	}

	estimated, maximum, estimator, estimatorName, err := videoProfileUnits(config, best.rule, normalized)
	if err != nil {
		return nil, err
	}
	multiplier := 1.0
	if normalized.CustomerMultiplier != nil {
		multiplier = *normalized.CustomerMultiplier
	}
	if !finiteNonNegative(multiplier) {
		return nil, fmt.Errorf("%w: customer multiplier is invalid", ErrVideoPricingInvalid)
	}
	conditions, _ := json.Marshal(best.rule.Conditions)
	return &VideoPriceQuote{
		RuleKey: best.rule.Key, BillingUnit: strings.ToLower(strings.TrimSpace(best.rule.BillingUnit)),
		UnitPrice: best.rule.UnitPriceUSD, EstimatedUnits: estimated, MaximumUnits: maximum,
		CustomerMultiplier: multiplier,
		EstimatedCost:      QuantizeUsageBillingAmount(best.rule.UnitPriceUSD * estimated * multiplier),
		HoldAmount:         QuantizeUsageBillingAmount(best.rule.UnitPriceUSD * maximum * multiplier),
		Priority:           best.rule.Priority, Specificity: best.specificity, Conditions: conditions,
		ConfigVersion: config.Version, ConfigHash: videoPricingConfigHash(config),
		EstimatorName: estimatorName, Estimator: estimator, Attributes: normalized,
	}, nil
}

func videoProfileUnits(config *VideoPricingConfig, rule VideoPricingRule, attrs VideoPricingAttributes) (float64, float64, *VideoUsageEstimator, string, error) {
	switch strings.ToLower(strings.TrimSpace(rule.BillingUnit)) {
	case VideoBillingUnitRequest:
		return 1, 1, nil, "", nil
	case VideoBillingUnitSecond:
		if attrs.Seconds <= 0 {
			return 0, 0, nil, "", fmt.Errorf("%w: seconds must be positive", ErrVideoPricingInvalid)
		}
		maximum := attrs.MaximumOutputSeconds
		if maximum <= 0 {
			maximum = attrs.Seconds
		}
		return float64(attrs.Seconds), float64(maximum), nil, "", nil
	case VideoBillingUnitVideoToken:
		name, estimator, ok := videoEstimator(config, rule.Estimator)
		if !ok {
			return 0, 0, nil, "", ErrVideoPricingEstimatorMissing
		}
		estimated, maximum, err := estimateVideoTokenUnits(config, estimator, attrs)
		if err != nil {
			return 0, 0, nil, "", err
		}
		copy := estimator
		return estimated, maximum, &copy, name, nil
	default:
		return 0, 0, nil, "", fmt.Errorf("%w: unsupported billing unit", ErrVideoPricingInvalid)
	}
}

func videoEstimator(config *VideoPricingConfig, requested string) (string, VideoUsageEstimator, bool) {
	for name, estimator := range config.Estimators {
		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(requested)) {
			return name, estimator, true
		}
	}
	return "", VideoUsageEstimator{}, false
}

func estimateVideoTokenUnits(config *VideoPricingConfig, estimator VideoUsageEstimator, attrs VideoPricingAttributes) (float64, float64, error) {
	seconds := float64(attrs.Seconds)
	maximumSeconds := float64(attrs.MaximumOutputSeconds)
	if maximumSeconds <= 0 {
		maximumSeconds = seconds
	}
	if seconds <= 0 || maximumSeconds <= 0 {
		return 0, 0, fmt.Errorf("%w: output seconds must be positive", ErrVideoPricingEstimatorMissing)
	}
	var estimated, maximum float64
	switch strings.ToLower(strings.TrimSpace(estimator.Type)) {
	case VideoEstimatorPixelFrame:
		pixels, err := videoPricingPixels(config, attrs)
		if err != nil {
			return 0, 0, err
		}
		inputSeconds := 0.0
		if estimator.TokenScope == VideoTokenScopeInputPlusOutput && attrs.InputHasVideo {
			inputSeconds = attrs.InputVideoSeconds
			if inputSeconds <= 0 {
				inputSeconds = estimator.MaxInputVideoSeconds
			}
			if inputSeconds <= 0 {
				return 0, 0, ErrVideoPricingEstimatorMissing
			}
		}
		estimated = math.Ceil((seconds + inputSeconds) * pixels * estimator.FPS / estimator.Divisor)
		maximum = math.Ceil((maximumSeconds + inputSeconds) * pixels * estimator.FPS / estimator.Divisor)
	case VideoEstimatorFixedTokensPerSecond:
		estimated = math.Ceil(seconds * estimator.TokensPerSecond)
		maximum = math.Ceil(maximumSeconds * estimator.TokensPerSecond)
	case VideoEstimatorFixedMaxUnits:
		estimated, maximum = estimator.MaxUnits, estimator.MaxUnits
	default:
		return 0, 0, ErrVideoPricingEstimatorMissing
	}
	for _, minimum := range estimator.MinimumUnits {
		if minimum.Conditions.matches(attrs) {
			estimated = math.Max(estimated, minimum.Units)
			maximum = math.Max(maximum, minimum.Units)
		}
	}
	if !finitePositive(estimated) || !finitePositive(maximum) {
		return 0, 0, ErrVideoPricingEstimatorMissing
	}
	if maximum < estimated {
		maximum = estimated
	}
	return estimated, maximum, nil
}

func videoPricingPixels(config *VideoPricingConfig, attrs VideoPricingAttributes) (float64, error) {
	if _, width, height, exact := parseVideoDimensions(attrs.Size); exact {
		return float64(width * height), nil
	}
	_, spec, ok := videoResolutionSpec(config, attrs.Resolution)
	if !ok || len(spec.Sizes) == 0 {
		return 0, ErrVideoPricingResolutionMissing
	}
	var maximum int64
	for _, rawSize := range spec.Sizes {
		_, width, height, parsed := parseVideoDimensions(rawSize)
		if parsed && width*height > maximum {
			maximum = width * height
		}
	}
	if maximum <= 0 {
		return 0, ErrVideoPricingResolutionMissing
	}
	return float64(maximum), nil
}

func videoPricingConfigHash(config *VideoPricingConfig) string {
	if config == nil {
		return ""
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
