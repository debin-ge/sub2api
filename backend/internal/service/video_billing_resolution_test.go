//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeVideoBillingResolutionStrictOrDefault(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		want       string
		wantErr    bool
		looseValue string
	}{
		{name: "omitted uses documented default", input: "", want: VideoBillingResolution480P, looseValue: VideoBillingResolution480P},
		{name: "known alias", input: " HD ", want: VideoBillingResolution720P, looseValue: VideoBillingResolution720P},
		{name: "known full hd alias", input: "full-hd", want: VideoBillingResolution1080P, looseValue: VideoBillingResolution1080P},
		{name: "explicit 4k is unpriced", input: "4k", wantErr: true, looseValue: VideoBillingResolution480P},
		{name: "explicit 8k is unpriced", input: "8192x8192", wantErr: true, looseValue: VideoBillingResolution480P},
		{name: "semantic unknown tier is unpriced", input: "largest", wantErr: true, looseValue: VideoBillingResolution480P},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeVideoBillingResolutionStrictOrDefault(tt.input)
			if tt.wantErr {
				require.ErrorIs(t, err, ErrModelPricingUnavailable)
				require.Empty(t, got)
			} else {
				require.NoError(t, err)
				require.Equal(t, tt.want, got)
			}
			require.Equal(t, tt.looseValue, NormalizeVideoBillingResolutionOrDefault(tt.input))
		})
	}
}

func TestCalculateVideoCostStrict_UnknownResolutionDoesNotBorrow480PPrice(t *testing.T) {
	svc := newTestBillingService()
	price480P := 0.08

	cost, err := svc.CalculateVideoCostStrict(
		"grok-imagine-video-1.5",
		"4k",
		1,
		8,
		&VideoPriceConfig{Price480P: &price480P},
		1,
	)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, cost)
}
