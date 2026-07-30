//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newAntigravityPricingGuardTestService(
	cfg *config.Config,
	billingService *BillingService,
) *AntigravityGatewayService {
	if cfg == nil {
		cfg = &config.Config{
			RunMode: config.RunModeStandard,
			Pricing: config.PricingConfig{
				StrictModelMatchMode: config.PricingGuardModeEnforce,
			},
		}
	}
	return &AntigravityGatewayService{
		settingService:       &SettingService{cfg: cfg},
		billingService:       billingService,
		pricingGuardRequired: true,
	}
}

func TestAntigravityResolvedPricingGuardFailsClosedWithoutBillingService(t *testing.T) {
	svc := newAntigravityPricingGuardTestService(nil, nil)
	err := svc.validateResolvedAntigravityTokenPricing(
		context.Background(),
		nil,
		&Account{Platform: PlatformAntigravity},
		"claude-sonnet-4-5",
		"claude-sonnet-4-5-thinking",
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestAntigravityResolvedPricingGuardRejectsInferredUnknownSKU(t *testing.T) {
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeEnforce,
		},
	}
	svc := newAntigravityPricingGuardTestService(cfg, NewBillingService(cfg, nil))

	// The legacy lookup can infer a Claude-family price, but the exact
	// pre-forward guard must not treat that inference as a configured SKU.
	_, looseErr := svc.billingService.GetModelPricing("claude-future-unpriced-v99")
	require.NoError(t, looseErr)
	err := svc.validateResolvedAntigravityTokenPricing(
		context.Background(),
		nil,
		&Account{Platform: PlatformAntigravity},
		"claude-sonnet-4-5",
		"claude-future-unpriced-v99",
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestAntigravityResolvedPricingGuardSimpleModeRejectsUnknownSKU(t *testing.T) {
	cfg := &config.Config{
		RunMode: config.RunModeSimple,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeOff,
		},
	}
	svc := newAntigravityPricingGuardTestService(cfg, NewBillingService(cfg, nil))

	err := svc.validateResolvedAntigravityTokenPricing(
		context.Background(),
		nil,
		&Account{Platform: PlatformAntigravity},
		"claude-future-unpriced-v99",
		"claude-future-unpriced-v99",
	)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestAntigravityResolvedPricingGuardAcceptsExactCatalogSKU(t *testing.T) {
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeEnforce,
		},
	}
	billingService := NewBillingService(cfg, &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			"claude-sonnet-4-5-thinking": {
				InputCostPerToken:           3e-6,
				OutputCostPerToken:          15e-6,
				CacheCreationInputTokenCost: 3.75e-6,
				CacheReadInputTokenCost:     0.3e-6,
			},
		},
	})
	svc := newAntigravityPricingGuardTestService(cfg, billingService)

	require.NoError(t, svc.validateResolvedAntigravityTokenPricing(
		context.Background(),
		nil,
		&Account{Platform: PlatformAntigravity},
		"claude-sonnet-4-5",
		"claude-sonnet-4-5-thinking",
	))
}

func TestAntigravityResolvedPricingGuardAcceptsCompleteExactChannelPrice(t *testing.T) {
	const groupID int64 = 73
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeEnforce,
		},
	}
	svc := newAntigravityPricingGuardTestService(cfg, NewBillingService(cfg, nil))
	channelService, cache := newGatewayPricingGuardChannel(
		groupID,
		PlatformAntigravity,
		BillingModelSourceUpstream,
	)
	zero := 0.0
	cache.pricingByGroupModel[channelModelKey{
		groupID:  groupID,
		platform: PlatformAntigravity,
		model:    "claude-future-channel-priced-v99",
	}] = &ChannelModelPricing{
		Platform:        PlatformAntigravity,
		Models:          []string{"claude-future-channel-priced-v99"},
		InputPrice:      &zero,
		OutputPrice:     &zero,
		CacheWritePrice: &zero,
		CacheReadPrice:  &zero,
	}
	cache.loadedAt = time.Now()
	svc.channelService = channelService

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	groupIDCopy := groupID
	c.Set("api_key", &APIKey{
		GroupID: &groupIDCopy,
		Group:   &Group{ID: groupID, Platform: PlatformAntigravity},
	})

	require.NoError(t, svc.validateResolvedAntigravityTokenPricing(
		context.Background(),
		c,
		&Account{Platform: PlatformAntigravity},
		"public-alias",
		"claude-future-channel-priced-v99",
	))
}

func TestAntigravityResolvedPricingGuardRejectsInvalidChannelOverride(t *testing.T) {
	const groupID int64 = 74
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeEnforce,
		},
	}
	svc := newAntigravityPricingGuardTestService(cfg, NewBillingService(cfg, nil))
	channelService, cache := newGatewayPricingGuardChannel(
		groupID,
		PlatformAntigravity,
		BillingModelSourceUpstream,
	)
	negative := -1.0
	cache.pricingByGroupModel[channelModelKey{
		groupID:  groupID,
		platform: PlatformAntigravity,
		model:    "gemini-3.1-pro",
	}] = &ChannelModelPricing{
		Platform:   PlatformAntigravity,
		Models:     []string{"gemini-3.1-pro"},
		InputPrice: &negative,
	}
	cache.loadedAt = time.Now()
	svc.channelService = channelService

	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	groupIDCopy := groupID
	c.Set("api_key", &APIKey{
		GroupID: &groupIDCopy,
		Group:   &Group{ID: groupID, Platform: PlatformAntigravity},
	})

	err := svc.validateResolvedAntigravityTokenPricing(
		context.Background(),
		c,
		&Account{Platform: PlatformAntigravity},
		"gemini-3.1-pro",
		"gemini-3.1-pro",
	)
	require.ErrorIs(t, err, ErrModelPricingUnavailable)
}

func TestAntigravityForwardUpstreamRejectsUnpricedBodyModelBeforeHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{
		RunMode: config.RunModeStandard,
		Pricing: config.PricingConfig{
			StrictModelMatchMode: config.PricingGuardModeEnforce,
		},
	}
	pricedModel := "claude-priced-mapping-target"
	billingService := NewBillingService(cfg, &PricingService{
		pricingData: map[string]*ModelPriceEntry{
			pricedModel: {
				InputCostPerToken:  1e-6,
				OutputCostPerToken: 2e-6,
			},
		},
	})
	upstream := &geminiCompatHTTPUpstreamStub{}
	svc := newAntigravityPricingGuardTestService(cfg, billingService)
	svc.httpUpstream = upstream

	const bodyModel = "claude-unpriced-passthrough"
	account := &Account{
		ID:          75,
		Name:        "upstream-pricing-guard",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeUpstream,
		Concurrency: 1,
		Credentials: map[string]any{
			"base_url": "https://claude-compatible.test",
			"api_key":  "test-key",
			"model_mapping": map[string]any{
				bodyModel: pricedModel,
			},
		},
	}
	require.Equal(t, pricedModel, mapAntigravityModel(account, bodyModel),
		"the scheduler-facing mapping must be priced for this regression to exercise the bypass")

	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	_, err := svc.ForwardUpstream(
		context.Background(),
		c,
		account,
		[]byte(`{"model":"`+bodyModel+`","max_tokens":16,"messages":[{"role":"user","content":"hi"}]}`),
	)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, `upstream_model="`+bodyModel+`"`)
	require.Zero(t, upstream.calls)
	require.Nil(t, upstream.lastReq)
}

func TestAntigravityForwardUpstreamAllowsImageOnlyCatalogSKU(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const imageModel = "gemini-3-pro-image"
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
	upstream := &geminiCompatHTTPUpstreamStub{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"id":"msg_image","type":"message","role":"assistant","content":[],"usage":{"input_tokens":2,"output_tokens":1}}`,
			)),
		},
	}
	svc := newAntigravityPricingGuardTestService(cfg, billingService)
	svc.httpUpstream = upstream
	account := &Account{
		ID:          76,
		Name:        "upstream-image-only",
		Platform:    PlatformAntigravity,
		Type:        AccountTypeUpstream,
		Concurrency: 1,
		Credentials: map[string]any{
			"base_url": "https://claude-compatible.test",
			"api_key":  "test-key",
		},
	}
	body := []byte(`{
		"model":"` + imageModel + `",
		"max_tokens":16,
		"messages":[{"role":"user","content":"draw"}],
		"generationConfig":{"imageConfig":{"imageSize":"2K"}}
	}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))

	result, err := svc.ForwardUpstream(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, imageModel, result.Model)
	require.Equal(t, imageModel, result.UpstreamModel)
	require.Equal(t, imageModel, result.BillingModel)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, ImageBillingSize2K, result.ImageSize)
	require.Equal(t, 1, upstream.calls)
	require.NotNil(t, upstream.lastReq)
}

func TestAntigravityForwardUpstreamImageTokenModeRequiresTokenPrice(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const (
		groupID    int64 = 77
		imageModel       = "gemini-3-pro-image"
	)
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
		ID:       78,
		Name:     "upstream-token-billed-image",
		Platform: PlatformAntigravity,
		Type:     AccountTypeUpstream,
		Credentials: map[string]any{
			"base_url": "https://claude-compatible.test",
			"api_key":  "test-key",
		},
	}
	body := []byte(`{"model":"` + imageModel + `","max_tokens":16,"messages":[]}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(string(body)))
	groupIDCopy := groupID
	c.Set("api_key", &APIKey{
		GroupID: &groupIDCopy,
		Group:   &Group{ID: groupID, Platform: PlatformAntigravity},
	})

	_, err := svc.ForwardUpstream(context.Background(), c, account, body)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, `upstream_model="`+imageModel+`"`)
	require.Zero(t, upstream.calls)
	require.Nil(t, upstream.lastReq)
}
