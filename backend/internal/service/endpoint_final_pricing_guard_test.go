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
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestForwardImagesRejectsUnpricedAccountMappedModelBeforeCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw","size":"1024x1024"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{}
	svc := newOpenAIPricingGuardService(nil)
	svc.httpUpstream = upstream
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       9201,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"model_mapping": map[string]any{
				"gpt-image-2": "gpt-image-unpriced-v99",
			},
		},
	}
	_, err = svc.ForwardImages(context.Background(), c, account, body, parsed, "")

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, `upstream_model="gpt-image-unpriced-v99"`)
	require.Nil(t, upstream.lastReq)
}

func TestForwardImagesAllowsUnpricedAliasMappedToPricedModelAndLocksBillingIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-image-public-unpriced-alias","prompt":"draw","size":"1024x1024"}`)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aGVsbG8="}]}`)),
	}}
	svc := newOpenAIPricingGuardService(nil)
	pricingService := NewPricingService(svc.cfg, nil)
	pricingService.pricingData["gpt-image-priced-final"] = &ModelPriceEntry{
		TokenPricingAbsent: true,
		OutputCostPerImage: 0.03,
	}
	svc.billingService.pricingService = pricingService
	svc.httpUpstream = upstream
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := &Account{
		ID:       9210,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "test-key",
			"base_url": "https://images.test/v1",
			"model_mapping": map[string]any{
				"gpt-image-public-unpriced-alias": "gpt-image-priced-final",
			},
		},
	}
	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")

	require.NoError(t, err)
	require.Equal(t, "gpt-image-public-unpriced-alias", result.Model)
	require.Equal(t, "gpt-image-priced-final", result.UpstreamModel)
	require.Equal(t, "gpt-image-priced-final", result.BillingModel)
	require.Equal(t, "gpt-image-priced-final", gjson.GetBytes(upstream.lastBody, "model").String())
}

func TestForwardGrokMediaRejectsUnpricedAccountMappedGenerationModelBeforeCredentials(t *testing.T) {
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		endpoint      GrokMediaEndpoint
		path          string
		body          string
		mappingSource string
		mappingTarget string
	}{
		{
			name:          "image",
			endpoint:      GrokMediaEndpointImagesGenerations,
			path:          "/v1/images/generations",
			body:          `{"model":"grok-imagine","prompt":"draw"}`,
			mappingSource: "grok-imagine-image-quality",
			mappingTarget: "vendor-image-unpriced-v99",
		},
		{
			name:          "video",
			endpoint:      GrokMediaEndpointVideosGenerations,
			path:          "/v1/videos/generations",
			body:          `{"model":"grok-imagine-video-1.5","prompt":"animate"}`,
			mappingSource: "grok-imagine-video",
			mappingTarget: "vendor-video-unpriced-v99",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")

			upstream := &httpUpstreamRecorder{}
			svc := newOpenAIPricingGuardService(nil)
			svc.httpUpstream = upstream
			account := &Account{
				ID:       9202,
				Platform: PlatformGrok,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"base_url": "https://xai.test/v1",
					"model_mapping": map[string]any{
						tt.mappingSource: tt.mappingTarget,
					},
				},
			}

			_, err := svc.ForwardGrokMedia(
				context.Background(),
				c,
				account,
				tt.endpoint,
				"",
				[]byte(tt.body),
				"application/json",
			)

			require.ErrorIs(t, err, ErrModelPricingUnavailable)
			require.ErrorContains(t, err, `upstream_model="`+tt.mappingTarget+`"`)
			require.Nil(t, upstream.lastReq)
		})
	}
}

func TestForwardGrokMediaAllowsUnpricedAliasMappedToPricedModelAndLocksBillingIdentity(t *testing.T) {
	t.Setenv(xai.EnvAllowUnsafeURLOverrides, "true")
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name         string
		endpoint     GrokMediaEndpoint
		path         string
		body         string
		pricedTarget string
		response     string
	}{
		{
			name:         "image",
			endpoint:     GrokMediaEndpointImagesGenerations,
			path:         "/v1/images/generations",
			body:         `{"model":"public-grok-image-alias","prompt":"draw"}`,
			pricedTarget: "grok-imagine-image-quality",
			response:     `{"data":[{"url":"https://images.test/result.png"}]}`,
		},
		{
			name:         "video",
			endpoint:     GrokMediaEndpointVideosGenerations,
			path:         "/v1/videos/generations",
			body:         `{"model":"public-grok-video-alias","prompt":"animate"}`,
			pricedTarget: "grok-imagine-video",
			response:     `{"request_id":"video-request-priced"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			c.Request.Header.Set("Content-Type", "application/json")
			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(strings.NewReader(tt.response)),
			}}
			svc := newOpenAIPricingGuardService(nil)
			svc.httpUpstream = upstream
			account := &Account{
				ID:       9211,
				Platform: PlatformGrok,
				Type:     AccountTypeAPIKey,
				Credentials: map[string]any{
					"api_key":  "test-key",
					"base_url": "https://xai.test/v1",
					"model_mapping": map[string]any{
						gjson.Get(tt.body, "model").String(): tt.pricedTarget,
					},
				},
			}

			result, err := svc.ForwardGrokMedia(
				context.Background(),
				c,
				account,
				tt.endpoint,
				"",
				[]byte(tt.body),
				"application/json",
			)

			require.NoError(t, err)
			require.Equal(t, gjson.Get(tt.body, "model").String(), result.Model)
			require.Equal(t, tt.pricedTarget, result.UpstreamModel)
			require.Equal(t, tt.pricedTarget, result.BillingModel)
			require.Equal(t, tt.pricedTarget, gjson.GetBytes(upstream.lastBody, "model").String())
		})
	}
}

func runOpenAIWSIngressFirstPayload(
	t *testing.T,
	svc *OpenAIGatewayService,
	account *Account,
	apiKey *APIKey,
	userAgent string,
	payload []byte,
) error {
	t.Helper()
	result := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			result <- err
			return
		}
		defer func() { _ = conn.CloseNow() }()

		recorder := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(recorder)
		c.Request = r.Clone(r.Context())
		c.Request.Header = c.Request.Header.Clone()
		c.Request.Header.Set("User-Agent", userAgent)
		if apiKey != nil {
			c.Set("api_key", apiKey)
		}
		readCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		messageType, firstMessage, readErr := conn.Read(readCtx)
		cancel()
		if readErr != nil {
			result <- readErr
			return
		}
		if messageType != websocket.MessageText && messageType != websocket.MessageBinary {
			result <- errors.New("unsupported websocket client message type")
			return
		}
		result <- svc.ProxyResponsesWebSocketFromClient(
			r.Context(),
			c,
			conn,
			account,
			"test-token",
			firstMessage,
			nil,
		)
	}))
	defer server.Close()

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 3*time.Second)
	client, _, err := websocket.Dial(dialCtx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	cancelDial()
	require.NoError(t, err)
	defer func() { _ = client.CloseNow() }()

	writeCtx, cancelWrite := context.WithTimeout(context.Background(), 3*time.Second)
	err = client.Write(writeCtx, websocket.MessageText, payload)
	cancelWrite()
	require.NoError(t, err)

	select {
	case err := <-result:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("waiting for websocket ingress result timed out")
		return nil
	}
}

func TestResponsesWSFirstTurnRejectsUnpricedCodexInjectedImageTool(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newOpenAIPricingGuardService(nil)
	svc.cfg.Gateway.CodexImageGenerationBridgeEnabled = true
	svc.cfg.Gateway.OpenAIWS.Enabled = true
	svc.cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	svc.cfg.Gateway.OpenAIWS.OAuthEnabled = true
	svc.cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	captureDialer := &openAIWSCaptureDialer{conn: &openAIWSCaptureConn{}}
	pool := newOpenAIWSConnPool(svc.cfg)
	pool.setClientDialerForTest(captureDialer)
	svc.openaiWSResolver = NewOpenAIWSProtocolResolver(svc.cfg)
	svc.openaiWSPool = pool
	defer svc.CloseOpenAIWSPool()
	groupID := int64(9203)
	apiKey := &APIKey{
		ID:      9204,
		GroupID: &groupID,
		Group: &Group{
			ID:                   groupID,
			AllowImageGeneration: true,
		},
	}
	account := &Account{
		ID:       9205,
		Platform: PlatformOpenAI,
		Type:     AccountTypeOAuth,
		Extra: map[string]any{
			"codex_image_generation_bridge":                true,
			"openai_oauth_responses_websockets_v2_enabled": true,
		},
	}

	err := runOpenAIWSIngressFirstPayload(
		t,
		svc,
		account,
		apiKey,
		"codex_cli_rs/0.99.0",
		[]byte(`{"type":"response.create","model":"gpt-5.5","input":"draw a cat"}`),
	)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.ErrorContains(t, err, "image model pricing is unavailable")
	require.Zero(t, captureDialer.DialCount(), "injected unpriced image tool must be rejected before upstream websocket dial")
}

func TestResponsesWSPassiveImageNamespaceDoesNotTriggerMediaPricingGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := newOpenAIPricingGuardService(nil)
	groupID := int64(9206)
	apiKey := &APIKey{
		ID:      9207,
		GroupID: &groupID,
		Group: &Group{
			ID:                   groupID,
			AllowImageGeneration: true,
		},
	}
	account := &Account{
		ID:       9208,
		Platform: PlatformOpenAI,
		Type:     AccountTypeAPIKey,
	}

	err := runOpenAIWSIngressFirstPayload(
		t,
		svc,
		account,
		apiKey,
		"ordinary-client/1.0",
		[]byte(`{
			"type":"response.create",
			"model":"gpt-5.5",
			"tools":[{
				"type":"namespace",
				"name":"image_gen",
				"tools":[{"type":"function","name":"imagegen"}]
			}]
		}`),
	)

	require.Error(t, err)
	require.NotErrorIs(t, err, ErrModelPricingUnavailable)
}
