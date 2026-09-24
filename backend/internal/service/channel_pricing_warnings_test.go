//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const imageCatalogJSON = `{
	"gemini-2.5-flash-image": {"litellm_provider": "vertex_ai-language-models", "mode": "image_generation",
		"input_cost_per_token": 3e-07, "output_cost_per_token": 2.5e-06,
		"output_cost_per_image_token": 3e-05},
	"claude-sonnet-4": {"litellm_provider": "anthropic", "mode": "chat",
		"input_cost_per_token": 3e-06, "output_cost_per_token": 1.5e-05}
}`

func TestCatalogImagePriceWarnings(t *testing.T) {
	svc := NewChannelService(nil, nil, nil, newStubPricingServiceFromJSON(t, imageCatalogJSON), nil)

	warnings := svc.CatalogImagePriceWarnings([]ChannelModelPricing{
		{Models: []string{"gemini-2.5-flash-image", "claude-sonnet-4"}, BillingMode: BillingModeToken,
			OutputPrice: testPtrFloat64(1e-06)},
	})
	require.Equal(t, []ModelPriceWarning{{
		Code: "IMAGE_PRICE_INHERITS_CATALOG", Field: "image_output_price", Model: "gemini-2.5-flash-image",
	}}, warnings)

	// 显式填了图片价、或根本没覆盖文本价、或非 token 模式：都不提示
	require.Empty(t, svc.CatalogImagePriceWarnings([]ChannelModelPricing{
		{Models: []string{"gemini-2.5-flash-image"}, OutputPrice: testPtrFloat64(1e-06), ImageOutputPrice: testPtrFloat64(2e-05)},
		{Models: []string{"gemini-2.5-flash-image"}},
		{Models: []string{"gemini-2.5-flash-image"}, BillingMode: BillingModeImage, OutputPrice: testPtrFloat64(1e-06)},
	}))

	require.Nil(t, newTestChannelService(nil).CatalogImagePriceWarnings([]ChannelModelPricing{
		{Models: []string{"gemini-2.5-flash-image"}, OutputPrice: testPtrFloat64(1e-06)},
	}))
}
