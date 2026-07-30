package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIGatewayService_APIKeyPassthrough_ImageIntentPreservesGateAndBilling(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"gpt-5.4","stream":false,"tools":[{"type":"image_generation","model":"gpt-image-2","size":"2048x1152"}],"input":"draw"}`)

	t.Run("disabled group rejects before upstream", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{}
		svc := newOpenAIImageGenerationControlTestService(upstream)
		c, recorder := newOpenAIImageGenerationControlTestContext(false, "curl/8.0")
		account := newOpenAIImageGenerationControlTestAccount()
		account.Extra = map[string]any{"openai_passthrough": true}

		result, err := svc.Forward(context.Background(), c, account, body)

		require.Error(t, err)
		require.Nil(t, result)
		require.Equal(t, http.StatusForbidden, recorder.Code)
		require.Equal(t, "permission_error", gjson.GetBytes(recorder.Body.Bytes(), "error.type").String())
		require.Nil(t, upstream.lastReq)
	})

	t.Run("allowed group keeps image billing", func(t *testing.T) {
		upstream := &httpUpstreamRecorder{resp: &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(
				`{"output":[{"id":"ig_1","type":"image_generation_call","result":"final-image","size":"2048x1152"}],"usage":{"input_tokens":1,"output_tokens":2}}`,
			)),
		}}
		svc := newOpenAIImageGenerationControlTestService(upstream)
		c, _ := newOpenAIImageGenerationControlTestContext(true, "curl/8.0")
		account := newOpenAIImageGenerationControlTestAccount()
		account.Extra = map[string]any{"openai_passthrough": true}

		result, err := svc.Forward(context.Background(), c, account, body)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, upstream.lastReq)
		require.Equal(t, body, upstream.lastBody)
		require.Equal(t, 1, result.ImageCount)
		require.Equal(t, "gpt-image-2", result.BillingModel)
		require.Equal(t, "2K", result.ImageSize)
		require.Equal(t, "2048x1152", result.ImageInputSize)
	})
}

func TestOpenAIGatewayService_APIKeyPassthrough_PassiveImageNamespaceDoesNotRequireMediaPricing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(strings.NewReader(
			`{"id":"resp_namespace","output":[],"usage":{"input_tokens":1,"output_tokens":2}}`,
		)),
	}}
	svc := newOpenAIImageGenerationControlTestService(upstream)
	svc.billingService = nil
	c, _ := newOpenAIImageGenerationControlTestContext(true, "codex_cli_rs/0.144.1")
	account := newOpenAIImageGenerationControlTestAccount()
	account.Extra = map[string]any{"openai_passthrough": true}
	body := []byte(`{
		"model":"gpt-5.5",
		"stream":false,
		"tools":[{"type":"namespace","name":"image_gen","tools":[{"type":"function","name":"imagegen"}]}],
		"input":"write code"
	}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotNil(t, upstream.lastReq)
	require.Zero(t, result.ImageCount)
}

func TestOpenAIGatewayService_APIKeyPassthrough_NativeImageToolStillRequiresMediaPricing(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"output":[]}`)),
	}}
	svc := newOpenAIImageGenerationControlTestService(upstream)
	svc.billingService = nil
	c, recorder := newOpenAIImageGenerationControlTestContext(true, "curl/8.0")
	account := newOpenAIImageGenerationControlTestAccount()
	account.Extra = map[string]any{"openai_passthrough": true}
	body := []byte(`{"model":"gpt-5.5","stream":false,"tools":[{"type":"image_generation","model":"unpriced-image-model"}],"input":"draw"}`)

	result, err := svc.Forward(context.Background(), c, account, body)

	require.ErrorIs(t, err, ErrModelPricingUnavailable)
	require.Nil(t, result)
	require.Nil(t, upstream.lastReq)
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
}
