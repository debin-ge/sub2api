package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClassifyImageBillingTier(t *testing.T) {
	tests := []struct {
		name     string
		size     string
		wantTier string
		wantOK   bool
	}{
		{name: "explicit 2k square", size: "2048x2048", wantTier: "2K", wantOK: true},
		{name: "explicit 2k landscape", size: "2048x1152", wantTier: "2K", wantOK: true},
		{name: "explicit 4k landscape", size: "3840x2160", wantTier: "4K", wantOK: true},
		{name: "explicit 4k portrait", size: "2160x3840", wantTier: "4K", wantOK: true},
		{name: "long edge 1k", size: "1024X768", wantTier: "1K", wantOK: true},
		{name: "long edge 2k", size: "1280x768", wantTier: "2K", wantOK: true},
		{name: "long edge 4k", size: "2560x1600", wantTier: "4K", wantOK: true},
		{name: "4k boundary", size: "4096x4096", wantTier: "4K", wantOK: true},
		{name: "future 8k dimensions", size: "8192x8192", wantOK: false},
		{name: "oversized single edge", size: "4097x1024", wantOK: false},
		{name: "tier string 1k", size: "1k", wantTier: "1K", wantOK: true},
		{name: "empty", size: "", wantOK: false},
		{name: "auto", size: "auto", wantOK: false},
		{name: "invalid", size: "not-a-size", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotTier, gotOK := ClassifyImageBillingTier(tt.size)
			require.Equal(t, tt.wantOK, gotOK)
			require.Equal(t, tt.wantTier, gotTier)
		})
	}
}

func TestResolveImageBillingSize(t *testing.T) {
	tests := []struct {
		name          string
		inputSize     string
		outputSizes   []string
		wantBilling   string
		wantOutput    string
		wantSource    string
		wantBreakdown map[string]int
	}{
		{
			name:          "output wins over input",
			inputSize:     "1024x1024",
			outputSizes:   []string{"3840x2160"},
			wantBilling:   "4K",
			wantOutput:    "3840x2160",
			wantSource:    ImageSizeSourceOutput,
			wantBreakdown: map[string]int{"4K": 1},
		},
		{
			name:        "input fallback",
			inputSize:   "1024x1024",
			wantBilling: "1K",
			wantSource:  ImageSizeSourceInput,
		},
		{
			name:        "auto defaults",
			inputSize:   "auto",
			wantBilling: "2K",
			wantSource:  ImageSizeSourceDefault,
		},
		{
			name:        "empty defaults",
			inputSize:   "",
			wantBilling: "2K",
			wantSource:  ImageSizeSourceDefault,
		},
		{
			name:        "invalid input remains unpriced",
			inputSize:   "largest",
			wantBilling: "largest",
			wantSource:  ImageSizeSourceInput,
		},
		{
			name:          "mixed output chooses highest tier",
			inputSize:     "1024x1024",
			outputSizes:   []string{"1024x1024", "3840x2160", "1280x720"},
			wantBilling:   "4K",
			wantOutput:    "1024x1024",
			wantSource:    ImageSizeSourceOutput,
			wantBreakdown: map[string]int{"1K": 1, "2K": 1, "4K": 1},
		},
		{
			name:        "unparseable output falls back to parseable input",
			inputSize:   "2048x1152",
			outputSizes: []string{"auto"},
			wantBilling: "2K",
			wantOutput:  "auto",
			wantSource:  ImageSizeSourceInput,
		},
		{
			name:        "future output tier remains unpriced",
			inputSize:   "2048x1152",
			outputSizes: []string{"8K"},
			wantBilling: "8K",
			wantOutput:  "8K",
			wantSource:  ImageSizeSourceOutput,
		},
		{
			name:        "future output dimensions remain unpriced",
			inputSize:   "2048x1152",
			outputSizes: []string{"8192x8192"},
			wantBilling: "8192x8192",
			wantOutput:  "8192x8192",
			wantSource:  ImageSizeSourceOutput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveImageBillingSize(tt.inputSize, tt.outputSizes)
			require.Equal(t, tt.wantBilling, got.BillingSize)
			require.Equal(t, tt.inputSize, got.InputSize)
			require.Equal(t, tt.wantOutput, got.OutputSize)
			require.Equal(t, tt.wantSource, got.Source)
			require.Equal(t, tt.wantBreakdown, got.Breakdown)
		})
	}
}

func TestNormalizeImageBillingTierStrictOrDefault(t *testing.T) {
	for _, size := range []string{"", "auto"} {
		tier, err := NormalizeImageBillingTierStrictOrDefault(size)
		require.NoError(t, err)
		require.Equal(t, ImageBillingSize2K, tier)
	}

	for _, size := range []string{"1K", "2048x2048", "4096x4096"} {
		_, err := NormalizeImageBillingTierStrictOrDefault(size)
		require.NoError(t, err)
	}

	for _, size := range []string{"8K", "8192x8192", "largest"} {
		tier, err := NormalizeImageBillingTierStrictOrDefault(size)
		require.Empty(t, tier)
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
	}
}
