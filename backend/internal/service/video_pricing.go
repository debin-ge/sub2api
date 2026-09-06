package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	VideoBillingUnitRequest    = "request"
	VideoBillingUnitSecond     = "second"
	VideoBillingUnitVideoToken = "video_token"
)

const maxVideoUsageResolutionBytes = 10

var (
	videoUsageResolutionLabelPattern = regexp.MustCompile(`^[1-9][0-9]{2,4}p$`)
	videoModelResolutionPattern      = regexp.MustCompile(`(?:^|[-_])([1-9][0-9]{2,4}p)(?:$|[-_])`)
)

type VideoPricingAttributes struct {
	Provider             string
	Model                string
	Operation            string
	Size                 string
	Resolution           string
	Seconds              int
	MaximumOutputSeconds int
	OutputSpecUnverified bool
	InputType            string
	InputHasVideo        bool
	InputVideoSeconds    float64
	AudioEnabled         *bool
	RequestMode          string
	InferenceMode        string
	Quality              string
	ServiceTier          string
	EstimatedVideoTokens float64
	MaximumUnits         float64
	CustomerMultiplier   *float64
	At                   time.Time
}

type VideoPriceQuote struct {
	RuleID             int64
	RuleKey            string
	Source             string
	BillingModel       string
	PricingPlatform    string
	BillingUnit        string
	UnitPrice          float64
	EstimatedUnits     float64
	MaximumUnits       float64
	CustomerMultiplier float64
	EstimatedCost      float64
	HoldAmount         float64
	Priority           int
	Specificity        int
	Conditions         json.RawMessage
	ConfigVersion      int
	ConfigHash         string
	EstimatorName      string
	Estimator          *VideoUsageEstimator
	Attributes         VideoPricingAttributes
}

func canonicalVideoUsageResolution(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || len(value) > maxVideoUsageResolutionBytes {
		return ""
	}
	if size, _, _, ok := parseVideoDimensions(value); ok {
		if len(size) <= maxVideoUsageResolutionBytes {
			return size
		}
		return ""
	}
	if videoUsageResolutionLabelPattern.MatchString(value) {
		return value
	}
	return ""
}

func videoUsageResolutionFromModels(models ...string) string {
	for _, model := range models {
		matches := videoModelResolutionPattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(model)))
		if len(matches) == 2 {
			return matches[1]
		}
	}
	return ""
}

type videoPricingConditions = VideoPricingConditions

type videoPriceCandidate struct {
	rule        PricingInterval
	conditions  videoPricingConditions
	specificity int
}

func ResolveVideoPrice(pricing *ChannelModelPricing, attrs VideoPricingAttributes) (*VideoPriceQuote, error) {
	if pricing == nil || pricing.BillingMode != BillingModeVideo {
		return nil, ErrVideoPricingMissing
	}
	if attrs.At.IsZero() {
		attrs.At = time.Now().UTC()
	}
	candidates := make([]videoPriceCandidate, 0, len(pricing.Intervals))
	for i := range pricing.Intervals {
		rule := pricing.Intervals[i]
		if rule.ValidFrom != nil && attrs.At.Before(*rule.ValidFrom) {
			continue
		}
		if rule.ValidUntil != nil && !attrs.At.Before(*rule.ValidUntil) {
			continue
		}
		conditions, specificity, err := decodeVideoPricingConditions(rule.Conditions)
		if err != nil {
			return nil, fmt.Errorf("%w: rule %d: %v", ErrVideoPricingInvalid, rule.ID, err)
		}
		if attrs.OutputSpecUnverified && (rule.BillingUnit == nil || !strings.EqualFold(strings.TrimSpace(*rule.BillingUnit), VideoBillingUnitRequest)) {
			continue
		}
		if !conditions.matches(attrs) {
			continue
		}
		if rule.PerRequestPrice == nil || !validVideoPrice(*rule.PerRequestPrice) {
			return nil, fmt.Errorf("%w: rule %d has no finite non-negative unit price", ErrVideoPricingInvalid, rule.ID)
		}
		if rule.BillingUnit == nil || !isVideoBillingUnit(*rule.BillingUnit) {
			return nil, fmt.Errorf("%w: rule %d has no supported billing unit", ErrVideoPricingInvalid, rule.ID)
		}
		candidates = append(candidates, videoPriceCandidate{rule: rule, conditions: conditions, specificity: specificity})
	}
	if len(candidates) == 0 {
		if attrs.OutputSpecUnverified {
			return nil, ErrVideoSourceSpecUnavailable
		}
		return nil, ErrVideoPricingMissing
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].rule.Priority != candidates[j].rule.Priority {
			return candidates[i].rule.Priority > candidates[j].rule.Priority
		}
		if candidates[i].specificity != candidates[j].specificity {
			return candidates[i].specificity > candidates[j].specificity
		}
		return candidates[i].rule.ID < candidates[j].rule.ID
	})
	best := candidates[0]
	if len(candidates) > 1 && candidates[1].rule.Priority == best.rule.Priority && candidates[1].specificity == best.specificity {
		return nil, ErrVideoPricingAmbiguous
	}

	unit := strings.ToLower(strings.TrimSpace(*best.rule.BillingUnit))
	estimatedUnits, err := videoPricingUnits(unit, attrs)
	if err != nil {
		return nil, err
	}
	maximumUnits := estimatedUnits
	if unit != VideoBillingUnitRequest && attrs.MaximumUnits > maximumUnits {
		maximumUnits = attrs.MaximumUnits
	}
	multiplier := 1.0
	if attrs.CustomerMultiplier != nil {
		multiplier = *attrs.CustomerMultiplier
	}
	if multiplier < 0 || math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
		return nil, fmt.Errorf("%w: customer multiplier is invalid", ErrVideoPricingInvalid)
	}
	unitPrice := *best.rule.PerRequestPrice
	return &VideoPriceQuote{
		RuleID:             best.rule.ID,
		BillingUnit:        unit,
		UnitPrice:          unitPrice,
		EstimatedUnits:     estimatedUnits,
		MaximumUnits:       maximumUnits,
		CustomerMultiplier: multiplier,
		EstimatedCost:      QuantizeUsageBillingAmount(unitPrice * estimatedUnits * multiplier),
		HoldAmount:         QuantizeUsageBillingAmount(unitPrice * maximumUnits * multiplier),
		Priority:           best.rule.Priority,
		Specificity:        best.specificity,
		Conditions:         append(json.RawMessage(nil), best.rule.Conditions...),
		RuleKey:            strings.TrimSpace(best.rule.TierLabel),
		Attributes:         attrs,
	}, nil
}

func decodeVideoPricingConditions(raw json.RawMessage) (videoPricingConditions, int, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		raw = json.RawMessage(`{}`)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var conditions videoPricingConditions
	if err := decoder.Decode(&conditions); err != nil {
		return conditions, 0, err
	}
	if err := ensureSingleJSONValue(decoder); err != nil {
		return conditions, 0, err
	}
	return conditions, videoPricingSpecificity(conditions), nil
}

func matchesVideoString(allowed []string, actual string) bool {
	if len(allowed) == 0 {
		return true
	}
	actual = strings.ToLower(strings.TrimSpace(actual))
	for _, value := range allowed {
		if strings.ToLower(strings.TrimSpace(value)) == actual {
			return true
		}
	}
	return false
}

func matchesVideoInt(allowed []int, actual int) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, value := range allowed {
		if value == actual {
			return true
		}
	}
	return false
}

func videoPricingUnits(unit string, attrs VideoPricingAttributes) (float64, error) {
	switch unit {
	case VideoBillingUnitRequest:
		return 1, nil
	case VideoBillingUnitSecond:
		if attrs.Seconds <= 0 {
			return 0, fmt.Errorf("%w: seconds must be positive", ErrVideoPricingInvalid)
		}
		return float64(attrs.Seconds), nil
	case VideoBillingUnitVideoToken:
		if attrs.EstimatedVideoTokens <= 0 || math.IsNaN(attrs.EstimatedVideoTokens) || math.IsInf(attrs.EstimatedVideoTokens, 0) {
			return 0, fmt.Errorf("%w: estimated video tokens must be finite and positive", ErrVideoPricingInvalid)
		}
		return attrs.EstimatedVideoTokens, nil
	default:
		return 0, fmt.Errorf("%w: unsupported billing unit %q", ErrVideoPricingInvalid, unit)
	}
}

func isVideoBillingUnit(unit string) bool {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case VideoBillingUnitRequest, VideoBillingUnitSecond, VideoBillingUnitVideoToken:
		return true
	default:
		return false
	}
}

func validVideoPrice(price float64) bool {
	return price >= 0 && !math.IsNaN(price) && !math.IsInf(price, 0)
}
