//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newPricedGeminiMediaCompatService(
	t *testing.T,
	upstream *geminiCompatHTTPUpstreamStub,
	model string,
) *GeminiMessagesCompatService {
	t.Helper()
	cfg := &config.Config{RunMode: config.RunModeStandard}
	guard := newGatewayPricingGuardService(cfg)
	pricing := NewPricingService(cfg, nil)
	pricing.pricingData[model] = &ModelPriceEntry{
		InputCostPerToken:  1e-6,
		OutputCostPerToken: 2e-6,
		OutputCostPerImage: 0.04,
	}
	guard.billingService.pricingService = pricing
	return &GeminiMessagesCompatService{
		httpUpstream:         upstream,
		gatewayPricingGuard:  guard,
		cfg:                  cfg,
		pricingGuardRequired: true,
	}
}

func geminiImageSuccessResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":2,"candidatesTokenCount":1}}`,
		)),
	}
}

func newGeminiMediaTestContext(
	t *testing.T,
	path string,
	body []byte,
) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func TestGeminiCompatFinalMappedImageIdentityAcrossProtocols(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		requestedModel = "public-image-alias"
		upstreamModel  = "gemini-3-pro-image"
	)
	account := &Account{
		ID:          9081,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key": "gemini-test-key",
			// If the final model were fed through mapping a second time, the
			// broader gemini-* rule would select the unpriced SKU.
			"model_mapping": map[string]any{
				"public-*": "gemini-3-pro-image",
				"gemini-*": "gemini-unpriced-second-pass",
			},
		},
	}

	tests := []struct {
		name string
		path string
		body []byte
		call func(*GeminiMessagesCompatService, *gin.Context, []byte) (*ForwardResult, error)
	}{
		{
			name: "messages",
			path: "/v1/messages",
			body: []byte(`{"model":"public-image-alias","max_tokens":16,"messages":[{"role":"user","content":"draw"}]}`),
			call: func(s *GeminiMessagesCompatService, c *gin.Context, body []byte) (*ForwardResult, error) {
				return s.Forward(context.Background(), c, account, body)
			},
		},
		{
			name: "chat completions",
			path: "/v1/chat/completions",
			body: []byte(`{"model":"public-image-alias","messages":[{"role":"user","content":"draw"}]}`),
			call: func(s *GeminiMessagesCompatService, c *gin.Context, body []byte) (*ForwardResult, error) {
				return s.ForwardAsChatCompletions(context.Background(), c, account, body)
			},
		},
		{
			name: "native",
			path: "/v1beta/models/public-image-alias:generateContent",
			body: []byte(`{"contents":[{"role":"user","parts":[{"text":"draw"}]}]}`),
			call: func(s *GeminiMessagesCompatService, c *gin.Context, body []byte) (*ForwardResult, error) {
				return s.ForwardNative(
					context.Background(),
					c,
					account,
					requestedModel,
					"generateContent",
					false,
					body,
				)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			upstream := &geminiCompatHTTPUpstreamStub{response: geminiImageSuccessResponse()}
			svc := newPricedGeminiMediaCompatService(t, upstream, upstreamModel)
			c, _ := newGeminiMediaTestContext(t, tt.path, tt.body)

			result, err := tt.call(svc, c, tt.body)

			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, requestedModel, result.Model)
			require.Equal(t, upstreamModel, result.UpstreamModel)
			require.Equal(t, upstreamModel, result.BillingModel)
			require.Equal(t, 1, result.ImageCount)
			require.Equal(t, ImageBillingSize2K, result.ImageSize)
			require.Empty(t, result.ImageInputSize)
			require.Equal(t, 1, upstream.calls)
			require.NotNil(t, upstream.lastReq)
			require.Contains(t, upstream.lastReq.URL.String(), "/models/"+upstreamModel+":")
			require.NotContains(t, upstream.lastReq.URL.String(), "second-pass")
		})
	}
}

func TestGeminiResponsesCompatLocksFinalMappedImageIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		requestedModel = "public-image-alias"
		upstreamModel  = "gemini-3-pro-image"
	)
	body := []byte(`{"model":"public-image-alias","input":"draw","stream":false}`)
	upstream := &geminiCompatHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(strings.Join([]string{
				`event: message_start`,
				`data: {"type":"message_start","message":{"id":"msg_image","type":"message","role":"assistant","content":[],"model":"gemini-3-pro-image","usage":{"input_tokens":2}}}`,
				``,
				`event: content_block_start`,
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				``,
				`event: content_block_delta`,
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"done"}}`,
				``,
				`event: content_block_stop`,
				`data: {"type":"content_block_stop","index":0}`,
				``,
				`event: message_delta`,
				`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
				``,
				`event: message_stop`,
				`data: {"type":"message_stop"}`,
				``,
			}, "\n"))),
		},
	}
	cfg := &config.Config{RunMode: config.RunModeStandard}
	svc := newGatewayPricingGuardService(cfg)
	pricing := NewPricingService(cfg, nil)
	pricing.pricingData[upstreamModel] = &ModelPriceEntry{
		InputCostPerToken:  1e-6,
		OutputCostPerToken: 2e-6,
		OutputCostPerImage: 0.04,
	}
	svc.billingService.pricingService = pricing
	svc.httpUpstream = upstream
	svc.pricingGuardRequired = true
	account := &Account{
		ID:          9085,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{
			"api_key":  "gemini-test-key",
			"base_url": "https://gemini-compat.test",
			"model_mapping": map[string]any{
				"public-*": "gemini-3-pro-image",
				"gemini-*": "gemini-unpriced-second-pass",
			},
		},
	}
	c, _ := newGeminiMediaTestContext(t, "/v1/responses", body)

	result, err := svc.ForwardAsResponses(context.Background(), c, account, body, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, requestedModel, result.Model)
	require.Equal(t, upstreamModel, result.UpstreamModel)
	require.Equal(t, upstreamModel, result.BillingModel)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, ImageBillingSize2K, result.ImageSize)
	require.Equal(t, 1, upstream.calls)
	require.NotNil(t, upstream.lastReq)
	require.Contains(t, upstream.lastReq.URL.String(), "/v1/messages")
}

func TestGeminiCompatSimpleModeRejectsUnpricedFinalImageModelBeforeUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"draw"}]}]}`)
	upstream := &geminiCompatHTTPUpstreamStub{response: geminiImageSuccessResponse()}
	// Simple Mode skips charging, but it must not turn an unknown upstream
	// price into permission to incur that upstream cost.
	cfg := &config.Config{RunMode: config.RunModeSimple}
	svc := &GeminiMessagesCompatService{
		httpUpstream:         upstream,
		gatewayPricingGuard:  newGatewayPricingGuardService(cfg),
		cfg:                  cfg,
		pricingGuardRequired: true,
	}
	account := &Account{
		ID:       9082,
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "gemini-test-key",
			"model_mapping": map[string]any{
				"public-image-alias": "gemini-3-pro-image",
			},
		},
	}
	c, _ := newGeminiMediaTestContext(
		t,
		"/v1beta/models/public-image-alias:generateContent",
		body,
	)

	_, err := svc.ForwardNative(
		context.Background(),
		c,
		account,
		"public-image-alias",
		"generateContent",
		false,
		body,
	)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, `upstream_model="gemini-3-pro-image"`)
	require.Zero(t, upstream.calls)
	require.Nil(t, upstream.lastReq)
}

func TestGeminiNativeRejectsUnpricedRoutedTokenModelBeforeSecondChannelMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		groupID         int64 = 9086
		requestedModel        = "gemini-public-alias"
		routedModel           = "gemini-native-unpriced-route"
		secondPassModel       = "gemini-priced-second-pass"
	)
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeEnforce,
		},
	}
	channelService, cache := newGatewayPricingGuardChannel(
		groupID,
		PlatformGemini,
		BillingModelSourceChannelMapped,
	)
	cache.mappingByGroupModel[channelModelKey{
		groupID: groupID, platform: PlatformGemini, model: requestedModel,
	}] = routedModel
	cache.mappingByGroupModel[channelModelKey{
		groupID: groupID, platform: PlatformGemini, model: routedModel,
	}] = secondPassModel
	pricingService := &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			secondPassModel: {
				InputCostPerToken:  1e-6,
				OutputCostPerToken: 2e-6,
			},
		},
	}
	guard := newGatewayPricingGuardService(cfg)
	guard.channelService = channelService
	guard.billingService = NewBillingService(cfg, pricingService)
	upstream := &geminiCompatHTTPUpstreamStub{}
	svc := &GeminiMessagesCompatService{
		httpUpstream:         upstream,
		gatewayPricingGuard:  guard,
		cfg:                  cfg,
		pricingGuardRequired: true,
	}
	account := &Account{
		ID:       9087,
		Platform: PlatformGemini,
		Type:     AccountTypeOAuth,
	}
	body := []byte(`{"contents":[{"role":"user","parts":[{"text":"hi"}]}]}`)
	c, _ := newGeminiMediaTestContext(
		t,
		"/v1beta/models/"+routedModel+":generateContent",
		body,
	)
	group := &Group{ID: groupID, Platform: PlatformGemini}
	groupIDCopy := groupID
	mapping := channelService.ResolveRequestChannelMapping(
		context.Background(),
		&groupIDCopy,
		requestedModel,
	)
	ctx := context.WithValue(context.Background(), ctxkey.Group, group)
	ctx = WithResolvedChannelPricingIdentity(ctx, requestedModel, mapping)

	_, err := svc.ForwardNative(
		ctx,
		c,
		account,
		routedModel,
		"generateContent",
		false,
		body,
	)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, `requested_model="`+requestedModel+`"`)
	require.ErrorContains(t, err, `upstream_model="`+routedModel+`"`)
	require.Zero(t, upstream.calls)
	require.Nil(t, upstream.lastReq)
}

func TestGeminiNativeAllowsImageOnlyCatalogSKUWithoutTokenPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const imageModel = "gemini-3-pro-image"
	body := []byte(`{
		"contents":[{"role":"user","parts":[{"text":"draw"}]}],
		"generationConfig":{"imageConfig":{"imageSize":"2K"}}
	}`)
	upstream := &geminiCompatHTTPUpstreamStub{response: geminiImageSuccessResponse()}
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeEnforce,
		},
	}
	guard := newGatewayPricingGuardService(cfg)
	guard.billingService = NewBillingService(cfg, &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			imageModel: {
				TokenPricingAbsent: true,
				OutputCostPerImage: 0.04,
			},
		},
	})
	svc := &GeminiMessagesCompatService{
		httpUpstream:         upstream,
		gatewayPricingGuard:  guard,
		cfg:                  cfg,
		pricingGuardRequired: true,
	}
	account := &Account{
		ID:          9091,
		Platform:    PlatformGemini,
		Type:        AccountTypeAPIKey,
		Concurrency: 1,
		Credentials: map[string]any{"api_key": "gemini-test-key"},
	}
	c, _ := newGeminiMediaTestContext(
		t,
		"/v1beta/models/"+imageModel+":generateContent",
		body,
	)

	result, err := svc.ForwardNative(
		context.Background(),
		c,
		account,
		imageModel,
		"generateContent",
		false,
		body,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, imageModel, result.UpstreamModel)
	require.Equal(t, imageModel, result.BillingModel)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, ImageBillingSize2K, result.ImageSize)
	require.Equal(t, 1, upstream.calls)
	require.NotNil(t, upstream.lastReq)
}

func TestGeminiExplicitUnknownImageSizeFailsClosed(t *testing.T) {
	for _, imageSize := range []string{"3K", "8K", "auto", "invalid", "8192x8192"} {
		t.Run(imageSize, func(t *testing.T) {
			body := []byte(
				`{"contents":[{"role":"user","parts":[{"text":"draw"}]}],` +
					`"generationConfig":{"imageConfig":{"imageSize":"` + imageSize + `"}}}`,
			)
			identity, err := resolveGeminiImageBillingIdentity("gemini-3-pro-image", body)
			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.Zero(t, identity.Count)
		})
	}

	for _, body := range [][]byte{
		[]byte(`{"contents":[]}`),
		[]byte(`{"contents":[],"generationConfig":{"imageConfig":{}}}`),
		[]byte(`{"contents":[],"generationConfig":{"imageConfig":{"imageSize":""}}}`),
	} {
		identity, err := resolveGeminiImageBillingIdentity("gemini-3-pro-image", body)
		require.NoError(t, err)
		require.Equal(t, ImageBillingSize2K, identity.SizeTier)
		require.Equal(t, 1, identity.Count)
	}
}

func TestGeminiAmbiguousOrNonStringImageSizeFailsClosed(t *testing.T) {
	tests := [][]byte{
		[]byte(`{"generationConfig":{"imageConfig":{"imageSize":"1K","imageSize":"4K"}}}`),
		[]byte(`{"generationConfig":{"imageConfig":{"imageSize":4}}}`),
		[]byte(`{"generationConfig":{"imageConfig":{"imageSize":"1K"}},"generationConfig":{"imageConfig":{"imageSize":"4K"}}}`),
		[]byte(`{"generationConfig":{"imageConfig":{"imageSize":"1K"},"imageConfig":{"imageSize":"4K"}}}`),
	}
	for _, body := range tests {
		_, err := resolveGeminiImageBillingIdentity("gemini-3-pro-image", body)
		require.ErrorIs(t, err, ErrModelPricingUnavailable)
	}
}

func TestGeminiNativeInvalidImageSizeNeverCallsUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":"8K"}},"contents":[]}`)
	upstream := &geminiCompatHTTPUpstreamStub{response: geminiImageSuccessResponse()}
	svc := &GeminiMessagesCompatService{httpUpstream: upstream, cfg: &config.Config{}}
	account := &Account{
		ID:       9083,
		Platform: PlatformGemini,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "gemini-test-key",
			"model_mapping": map[string]any{
				"alias": "gemini-3-pro-image",
			},
		},
	}
	c, _ := newGeminiMediaTestContext(t, "/v1beta/models/alias:generateContent", body)

	_, err := svc.ForwardNative(
		context.Background(),
		c,
		account,
		"alias",
		"generateContent",
		false,
		body,
	)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Zero(t, upstream.calls)
}

func TestAntigravityNativeInvalidImageSizeNeverCallsUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"generationConfig":{"imageConfig":{"imageSize":"8K"}},"contents":[]}`)
	upstream := &geminiCompatHTTPUpstreamStub{response: geminiImageSuccessResponse()}
	svc := &AntigravityGatewayService{httpUpstream: upstream}
	account := &Account{
		ID:       9084,
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"alias": "gemini-3-pro-image",
			},
		},
	}
	c, _ := newGeminiMediaTestContext(t, "/v1beta/models/alias:generateContent", body)

	_, err := svc.ForwardGemini(
		context.Background(),
		c,
		account,
		"alias",
		"generateContent",
		false,
		body,
		false,
	)

	require.True(t, errors.Is(err, ErrModelPricingUnavailable))
	require.Zero(t, upstream.calls)
}

func TestAntigravityNativeAllowsImageOnlyCatalogSKUWithoutTokenPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		requestedModel = "public-antigravity-image"
		imageModel     = "gemini-3-pro-image"
	)
	body := []byte(`{
		"contents":[{"role":"user","parts":[{"text":"draw"}]}],
		"generationConfig":{"imageConfig":{"imageSize":"2K"}}
	}`)
	upstream := &geminiCompatHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}],\"usageMetadata\":{\"promptTokenCount\":2,\"candidatesTokenCount\":1}}}\n\n",
			)),
		},
	}
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeEnforce,
		},
	}
	billingService := NewBillingService(cfg, &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			imageModel: {
				TokenPricingAbsent: true,
				OutputCostPerImage: 0.04,
			},
		},
	})
	svc := newAntigravityPricingGuardTestService(cfg, billingService)
	svc.tokenProvider = &AntigravityTokenProvider{}
	svc.httpUpstream = upstream
	account := &Account{
		ID:          9088,
		Name:        "antigravity-image-only",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeOAuth,
		Concurrency: 1,
		Credentials: map[string]any{
			"access_token": "test-token",
			"project_id":   "test-project",
			"model_mapping": map[string]any{
				requestedModel: imageModel,
			},
		},
	}
	c, _ := newGeminiMediaTestContext(
		t,
		"/v1beta/models/"+requestedModel+":streamGenerateContent",
		body,
	)

	result, err := svc.ForwardGemini(
		context.Background(),
		c,
		account,
		requestedModel,
		"streamGenerateContent",
		true,
		body,
		false,
	)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, imageModel, result.UpstreamModel)
	require.Equal(t, imageModel, result.BillingModel)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, ImageBillingSize2K, result.ImageSize)
	require.Equal(t, 1, upstream.calls)
	require.NotNil(t, upstream.lastReq)
}

func TestAntigravityNativeImageChannelTokenModeStillRequiresTokenPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		groupID        int64 = 9089
		requestedModel       = "public-antigravity-token-billed-image"
		imageModel           = "gemini-3-pro-image"
	)
	body := []byte(`{
		"contents":[{"role":"user","parts":[{"text":"draw"}]}],
		"generationConfig":{"imageConfig":{"imageSize":"2K"}}
	}`)
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeEnforce,
		},
	}
	billingService := NewBillingService(cfg, &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			imageModel: {
				TokenPricingAbsent: true,
				OutputCostPerImage: 0.04,
			},
		},
	})
	channelService, cache := newGatewayPricingGuardChannel(
		groupID,
		PlatformAntigravity,
		BillingModelSourceUpstream,
	)
	cache.pricingByGroupModel[channelModelKey{
		groupID: groupID, platform: PlatformAntigravity, model: imageModel,
	}] = &ChannelModelPricing{
		Platform:    PlatformAntigravity,
		BillingMode: BillingModeToken,
	}
	upstream := &geminiCompatHTTPUpstreamStub{}
	svc := newAntigravityPricingGuardTestService(cfg, billingService)
	svc.channelService = channelService
	svc.httpUpstream = upstream
	account := &Account{
		ID:       9090,
		Name:     "antigravity-token-billed-image",
		Platform: PlatformAntigravity,
		Type:     AccountTypeOAuth,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				requestedModel: imageModel,
			},
		},
	}
	c, _ := newGeminiMediaTestContext(
		t,
		"/v1beta/models/"+requestedModel+":generateContent",
		body,
	)
	groupIDCopy := groupID
	c.Set("api_key", &APIKey{
		GroupID: &groupIDCopy,
		Group:   &Group{ID: groupID, Platform: PlatformAntigravity},
	})

	_, err := svc.ForwardGemini(
		context.Background(),
		c,
		account,
		requestedModel,
		"generateContent",
		false,
		body,
		false,
	)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, `upstream_model="`+imageModel+`"`)
	require.Zero(t, upstream.calls)
	require.Nil(t, upstream.lastReq)
}
