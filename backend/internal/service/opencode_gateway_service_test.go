package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func newOpenCodeGatewayTestContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Request.Header.Set("Authorization", "Bearer client-key")
	c.Request.Header.Set("x-api-key", "anthropic-client-key")
	c.Request.Header.Set("x-goog-api-key", "google-client-key")
	c.Request.Header.Set("Cookie", "session=client")
	c.Request.Header.Set("Proxy-Authorization", "Basic client")
	c.Request.Header.Set("User-Agent", "opencode-test-client")
	return c, rec
}

func openCodeGatewayTestAccount() *Account {
	return &Account{
		ID:       501,
		Platform: PlatformOpenCode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-opencode-test",
			"base_url": "https://opencode2api.example/base",
			"model_mapping": map[string]any{
				"gpt-5": "opencode/gpt5-nano",
			},
		},
	}
}

func TestOpenCodeGatewayServiceForwardChatCompletionsUsesBearerAndParsesUsage(t *testing.T) {
	var captured *http.Request
	var capturedBody []byte
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"opencode-chat-1"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chat_1","model":"opencode/gpt5-nano","usage":{"prompt_tokens":17,"completion_tokens":8,"completion_tokens_details":{"reasoning_tokens":3}}}`)),
		}, nil
	})}
	svc := NewOpenCodeGatewayService(client, nil)
	c, _ := newOpenCodeGatewayTestContext("/v1/chat/completions")
	body := []byte(`{"model":"gpt-5","reasoning_effort":"high","messages":[{"role":"user","content":"hello"}]}`)

	result, err := svc.ForwardChatCompletions(context.Background(), c, openCodeGatewayTestAccount(), body, "req-chat")
	if err != nil {
		t.Fatalf("ForwardChatCompletions error = %v", err)
	}
	if got := captured.URL.String(); got != "https://opencode2api.example/base/v1/chat/completions" {
		t.Fatalf("upstream url = %q", got)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer sk-opencode-test" {
		t.Fatalf("Authorization = %q", got)
	}
	for _, key := range []string{"x-api-key", "x-goog-api-key", "Cookie", "Proxy-Authorization"} {
		if got := captured.Header.Get(key); got != "" {
			t.Fatalf("%s should not be forwarded, got %q", key, got)
		}
	}
	if got := captured.Header.Get("User-Agent"); got != "opencode-test-client" {
		t.Fatalf("User-Agent = %q", got)
	}
	if got := gjson.GetBytes(capturedBody, "model").String(); got != "opencode/gpt5-nano" {
		t.Fatalf("upstream model = %q body=%s", got, string(capturedBody))
	}
	if got := gjson.GetBytes(capturedBody, "reasoning_effort").String(); got != "high" {
		t.Fatalf("reasoning_effort was not preserved body=%s", string(capturedBody))
	}
	if result.RequestID != "opencode-chat-1" || result.Model != "gpt-5" || result.UpstreamModel != "opencode/gpt5-nano" {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Usage.InputTokens != 17 || result.Usage.OutputTokens != 8 || result.Usage.CacheCreationInputTokens != 0 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if result.ReasoningEffort == nil || *result.ReasoningEffort != "high" {
		t.Fatalf("reasoning effort = %v", result.ReasoningEffort)
	}
}

func TestOpenCodeGatewayServiceForwardResponsesUsesResponsesEndpointAndParsesUsage(t *testing.T) {
	var captured *http.Request
	var capturedBody []byte
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		if err != nil {
			t.Fatalf("read upstream body: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"opencode-resp-1"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"resp_1","model":"opencode/big-pickle","usage":{"input_tokens":21,"output_tokens":11,"output_tokens_details":{"reasoning_tokens":5}}}`)),
		}, nil
	})}
	svc := NewOpenCodeGatewayService(client, nil)
	c, _ := newOpenCodeGatewayTestContext("/v1/responses")
	body := []byte(`{"model":"opencode/big-pickle","input":"hello","reasoning":{"effort":"medium"}}`)

	result, err := svc.ForwardResponses(context.Background(), c, openCodeGatewayTestAccount(), body, "req-responses")
	if err != nil {
		t.Fatalf("ForwardResponses error = %v", err)
	}
	if got := captured.URL.String(); got != "https://opencode2api.example/base/v1/responses" {
		t.Fatalf("upstream url = %q", got)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer sk-opencode-test" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := gjson.GetBytes(capturedBody, "reasoning.effort").String(); got != "medium" {
		t.Fatalf("reasoning was not preserved body=%s", string(capturedBody))
	}
	if result.RequestID != "opencode-resp-1" || result.Model != "opencode/big-pickle" || result.UpstreamModel != "opencode/big-pickle" {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Usage.InputTokens != 21 || result.Usage.OutputTokens != 11 || result.Usage.CacheCreationInputTokens != 0 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if result.ReasoningEffort == nil || *result.ReasoningEffort != "medium" {
		t.Fatalf("reasoning effort = %v", result.ReasoningEffort)
	}
}

func TestOpenCodeGatewayServiceForwardModelsProxiesOpenAIModelList(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"opencode-models-1"}},
			Body:       io.NopCloser(strings.NewReader(`{"object":"list","data":[{"id":"opencode/big-pickle"},{"id":"gpt5-nano"}]}`)),
		}, nil
	})}
	svc := NewOpenCodeGatewayService(client, nil)
	c, rec := newOpenCodeGatewayTestContext("/v1/models")

	result, err := svc.ForwardModels(context.Background(), c, openCodeGatewayTestAccount(), "req-models")
	if err != nil {
		t.Fatalf("ForwardModels error = %v", err)
	}
	if got := captured.URL.String(); got != "https://opencode2api.example/base/v1/models" {
		t.Fatalf("upstream url = %q", got)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer sk-opencode-test" {
		t.Fatalf("Authorization = %q", got)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "opencode/big-pickle") || !strings.Contains(rec.Body.String(), "gpt5-nano") {
		t.Fatalf("models response not proxied: %s", rec.Body.String())
	}
	if result.RequestID != "opencode-models-1" {
		t.Fatalf("request id = %q", result.RequestID)
	}
}

func TestMapOpenCodeUpstreamStatus(t *testing.T) {
	tests := []struct {
		status       int
		clientStatus int
		errorType    string
		retryable    bool
	}{
		{status: http.StatusBadRequest, clientStatus: http.StatusBadRequest, errorType: "invalid_request_error", retryable: false},
		{status: http.StatusUnauthorized, clientStatus: http.StatusBadGateway, errorType: "upstream_auth_error", retryable: false},
		{status: http.StatusForbidden, clientStatus: http.StatusBadGateway, errorType: "upstream_auth_error", retryable: false},
		{status: http.StatusTooManyRequests, clientStatus: http.StatusTooManyRequests, errorType: "rate_limit_error", retryable: true},
		{status: http.StatusServiceUnavailable, clientStatus: http.StatusBadGateway, errorType: "overloaded_error", retryable: true},
		{status: http.StatusInternalServerError, clientStatus: http.StatusBadGateway, errorType: "server_error", retryable: true},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			got := MapOpenCodeUpstreamStatus(tc.status)
			if got.ClientStatus != tc.clientStatus || got.ErrorType != tc.errorType || got.Retryable != tc.retryable {
				t.Fatalf("MapOpenCodeUpstreamStatus(%d) = %+v", tc.status, got)
			}
		})
	}
}
