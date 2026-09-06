package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreviewVideoPricingConfigExplainsQualityMismatch(t *testing.T) {
	profile := seedanceVideoPricing()
	profile.Rules = []VideoPricingRule{{
		Key: "reference-video", BillingUnit: VideoBillingUnitVideoToken, UnitPriceUSD: 23.25e-6,
		Estimator: "output-video-token", Conditions: VideoPricingConditions{
			Operations: []string{VideoOperationGenerate}, Resolutions: []string{"480p"},
			InputHasVideo: videoBool(true), Qualities: []string{"standard"},
		},
	}}

	result, err := PreviewVideoPricingConfig(profile, VideoPricingPreviewAttributes{
		Operation: VideoOperationGenerate, Size: "864x480", Seconds: 10,
		InputType: string(VideoInputRoleReferenceVideo), InputHasVideo: true,
	})

	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, "video_pricing_rule_missing", result.ErrorCode)
	require.Len(t, result.RejectedRules, 1)
	require.Equal(t, "quality", result.RejectedRules[0].Mismatches[0].Field)
	require.Equal(t, "", result.RejectedRules[0].Mismatches[0].Actual)
}

func TestPreviewVideoPricingConfigReturnsBackendQuote(t *testing.T) {
	profile := seedanceVideoPricing()

	result, err := PreviewVideoPricingConfig(profile, VideoPricingPreviewAttributes{
		Operation: VideoOperationGenerate, Size: "864x480", Seconds: 5,
		InputType: "text", InputHasVideo: false,
	})

	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "480p-no-video", result.RuleKey)
	require.Equal(t, 48_600.0, result.EstimatedUnits)
	require.Empty(t, result.ErrorCode)
}
