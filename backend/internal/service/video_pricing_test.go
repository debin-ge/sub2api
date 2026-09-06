package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func videoPrice(value float64) *float64 { return &value }
func videoUnit(value string) *string    { return &value }

func TestResolveVideoPriceSelectsPriorityThenSpecificity(t *testing.T) {
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	pricing := &ChannelModelPricing{
		BillingMode: BillingModeVideo,
		Intervals: []PricingInterval{
			{
				ID: 1, PerRequestPrice: videoPrice(0.10), BillingUnit: videoUnit(VideoBillingUnitSecond), Priority: 1,
				Conditions: json.RawMessage(`{"providers":["openai"]}`),
			},
			{
				ID: 2, PerRequestPrice: videoPrice(0.25), BillingUnit: videoUnit(VideoBillingUnitSecond), Priority: 2,
				Conditions: json.RawMessage(`{"providers":["openai"],"operations":["generate"],"sizes":["1280x720"]}`),
			},
		},
	}
	multiplier := 1.5
	quote, err := ResolveVideoPrice(pricing, VideoPricingAttributes{
		Provider: "openai", Model: "sora-2", Operation: "generate", Size: "1280x720", Seconds: 8,
		CustomerMultiplier: &multiplier, MaximumUnits: 20, At: now,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), quote.RuleID)
	require.Equal(t, 3, quote.Specificity)
	require.Equal(t, 8.0, quote.EstimatedUnits)
	require.Equal(t, 20.0, quote.MaximumUnits)
	require.InDelta(t, 3, quote.EstimatedCost, 0.000001)
	require.InDelta(t, 7.5, quote.HoldAmount, 0.000001)
}

func TestResolveVideoPriceRejectsAmbiguousRules(t *testing.T) {
	pricing := &ChannelModelPricing{
		BillingMode: BillingModeVideo,
		Intervals: []PricingInterval{
			{ID: 1, PerRequestPrice: videoPrice(1), BillingUnit: videoUnit(VideoBillingUnitRequest), Priority: 5, Conditions: json.RawMessage(`{"operations":["generate"]}`)},
			{ID: 2, PerRequestPrice: videoPrice(2), BillingUnit: videoUnit(VideoBillingUnitRequest), Priority: 5, Conditions: json.RawMessage(`{"sizes":["1280x720"]}`)},
		},
	}
	_, err := ResolveVideoPrice(pricing, VideoPricingAttributes{Operation: "generate", Size: "1280x720"})
	require.ErrorIs(t, err, ErrVideoPricingAmbiguous)
}

func TestResolveVideoPriceHonorsValidityAndFailsClosed(t *testing.T) {
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	past := now.Add(-time.Hour)
	pricing := &ChannelModelPricing{
		BillingMode: BillingModeVideo,
		Intervals: []PricingInterval{
			{ID: 1, PerRequestPrice: videoPrice(1), BillingUnit: videoUnit(VideoBillingUnitSecond), ValidUntil: &past},
		},
	}
	_, err := ResolveVideoPrice(pricing, VideoPricingAttributes{Seconds: 8, At: now})
	require.ErrorIs(t, err, ErrVideoPricingMissing)

	pricing.Intervals[0].ValidUntil = nil
	pricing.Intervals[0].Conditions = json.RawMessage(`{"unknown":true}`)
	_, err = ResolveVideoPrice(pricing, VideoPricingAttributes{Seconds: 8, At: now})
	require.ErrorIs(t, err, ErrVideoPricingInvalid)
}

func TestResolveVideoPriceRequiresFiniteHoldBound(t *testing.T) {
	pricing := &ChannelModelPricing{
		BillingMode: BillingModeVideo,
		Intervals: []PricingInterval{
			{ID: 1, PerRequestPrice: videoPrice(0.5), BillingUnit: videoUnit(VideoBillingUnitVideoToken)},
		},
	}
	_, err := ResolveVideoPrice(pricing, VideoPricingAttributes{})
	require.ErrorIs(t, err, ErrVideoPricingInvalid)

	quote, err := ResolveVideoPrice(pricing, VideoPricingAttributes{EstimatedVideoTokens: 100, MaximumUnits: 120})
	require.NoError(t, err)
	require.InDelta(t, 50, quote.EstimatedCost, 0.000001)
	require.InDelta(t, 60, quote.HoldAmount, 0.000001)
}

func TestResolveVideoPriceZeroMultiplierIsExplicitlyFree(t *testing.T) {
	pricing := &ChannelModelPricing{
		BillingMode: BillingModeVideo,
		Intervals: []PricingInterval{
			{ID: 1, PerRequestPrice: videoPrice(0.5), BillingUnit: videoUnit(VideoBillingUnitSecond)},
		},
	}
	zero := 0.0
	quote, err := ResolveVideoPrice(pricing, VideoPricingAttributes{Seconds: 8, CustomerMultiplier: &zero})
	require.NoError(t, err)
	require.Zero(t, quote.HoldAmount)
}
