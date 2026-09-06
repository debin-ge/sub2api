package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateIntervalsVideoRequiresStructuredRule(t *testing.T) {
	unit := VideoBillingUnitSecond
	price := 0.1
	now := time.Now().UTC()
	later := now.Add(time.Hour)
	valid := PricingInterval{
		TierLabel: "sora-2-720p", PerRequestPrice: &price, BillingUnit: &unit,
		Conditions: json.RawMessage(`{"operations":["generate"],"seconds":[8]}`),
		ValidFrom:  &now, ValidUntil: &later,
	}
	require.NoError(t, ValidateIntervals([]PricingInterval{valid}, BillingModeVideo))

	missingUnit := valid
	missingUnit.BillingUnit = nil
	require.ErrorContains(t, ValidateIntervals([]PricingInterval{missingUnit}, BillingModeVideo), "billing_unit")

	unknownCondition := valid
	unknownCondition.Conditions = json.RawMessage(`{"unknown":true}`)
	require.ErrorContains(t, ValidateIntervals([]PricingInterval{unknownCondition}, BillingModeVideo), "conditions")

	invalidWindow := valid
	invalidWindow.ValidUntil = &now
	require.ErrorContains(t, ValidateIntervals([]PricingInterval{invalidWindow}, BillingModeVideo), "valid_until")
}

func TestValidateIntervalsVideoRejectsAmbiguousOverlappingRules(t *testing.T) {
	unit := VideoBillingUnitSecond
	price := 0.1
	intervals := []PricingInterval{
		{TierLabel: "rule-a", PerRequestPrice: &price, BillingUnit: &unit, Priority: 10, Conditions: json.RawMessage(`{"operations":["generate"],"seconds":[8]}`)},
		{TierLabel: "rule-b", PerRequestPrice: &price, BillingUnit: &unit, Priority: 10, Conditions: json.RawMessage(`{"operations":["generate"],"sizes":["1280x720"]}`)},
	}

	err := ValidateIntervals(intervals, BillingModeVideo)

	require.ErrorContains(t, err, "overlap at equal priority and specificity")
}

func TestValidateIntervalsVideoAllowsDisjointEqualRankRules(t *testing.T) {
	unit := VideoBillingUnitSecond
	price := 0.1
	intervals := []PricingInterval{
		{TierLabel: "rule-a", PerRequestPrice: &price, BillingUnit: &unit, Priority: 10, Conditions: json.RawMessage(`{"operations":["generate"],"seconds":[8]}`)},
		{TierLabel: "rule-b", PerRequestPrice: &price, BillingUnit: &unit, Priority: 10, Conditions: json.RawMessage(`{"operations":["edit"],"seconds":[8]}`)},
	}

	require.NoError(t, ValidateIntervals(intervals, BillingModeVideo))
}

func TestValidateIntervalsVideoRejectsInvalidConditionValues(t *testing.T) {
	unit := VideoBillingUnitSecond
	price := 0.1
	base := PricingInterval{TierLabel: "rule", PerRequestPrice: &price, BillingUnit: &unit}

	for name, conditions := range map[string]string{
		"empty operation":        `{"operations":[""]}`,
		"invalid size":           `{"sizes":["720p"]}`,
		"non-positive seconds":   `{"seconds":[0]}`,
		"unknown request mode":   `{"request_modes":["realtime"]}`,
		"duplicate service tier": `{"service_tiers":["default","DEFAULT"]}`,
	} {
		t.Run(name, func(t *testing.T) {
			interval := base
			interval.Conditions = json.RawMessage(conditions)
			require.Error(t, ValidateIntervals([]PricingInterval{interval}, BillingModeVideo))
		})
	}
}
