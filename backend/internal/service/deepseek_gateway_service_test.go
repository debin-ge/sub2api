package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func newDeepSeekGatewayTestContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Request.Header.Set("Authorization", "Bearer client-key")
	c.Request.Header.Set("x-api-key", "anthropic-client-key")
	c.Request.Header.Set("x-goog-api-key", "google-client-key")
	c.Request.Header.Set("Cookie", "session=client")
	c.Request.Header.Set("Proxy-Authorization", "Basic client")
	c.Request.Header.Set("User-Agent", "deepseek-test-client")
	return c, rec
}

func deepSeekGatewayTestAccount() *Account {
	return &Account{
		ID:       401,
		Platform: PlatformDeepSeek,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-deepseek-test",
		},
	}
}

func TestDeepSeekGatewayServiceForwardMessagesUsesAnthropicAPIKeyHeader(t *testing.T) {
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
			Header: http.Header{
				"Content-Type": {"application/json"},
				"X-Request-Id": {"deepseek-msg-1"},
				"X-Blocked":    {"must-not-pass"},
			},
			Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"deepseek-v4-pro","usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":3}}`)),
		}, nil
	})}
	svc := NewDeepSeekGatewayService(client, nil)
	c, rec := newDeepSeekGatewayTestContext("/v1/messages")
	body := []byte(`{"model":"deepseek-v4-pro","max_tokens":64,"thinking":{"type":"enabled","budget_tokens":32},"messages":[{"role":"user","content":"hello"}]}`)

	result, err := svc.ForwardMessages(context.Background(), c, deepSeekGatewayTestAccount(), body, "req-1")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if got := captured.URL.String(); got != "https://api.deepseek.com/anthropic/v1/messages" {
		t.Fatalf("upstream url = %q", got)
	}
	if got := captured.Header.Get("x-api-key"); got != "sk-deepseek-test" {
		t.Fatalf("x-api-key = %q", got)
	}
	if got := captured.Header.Get("Authorization"); got != "" {
		t.Fatalf("Authorization should not be sent for Anthropic API, got %q", got)
	}
	for _, key := range []string{"x-goog-api-key", "Cookie", "Proxy-Authorization"} {
		if got := captured.Header.Get(key); got != "" {
			t.Fatalf("%s should not be forwarded, got %q", key, got)
		}
	}
	if got := captured.Header.Get("User-Agent"); got != "deepseek-test-client" {
		t.Fatalf("User-Agent = %q", got)
	}
	if got := captured.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("anthropic-version = %q", got)
	}
	if got := gjson.GetBytes(capturedBody, "model").String(); got != "deepseek-v4-pro" {
		t.Fatalf("upstream model = %q body=%s", got, string(capturedBody))
	}
	if got := gjson.GetBytes(capturedBody, "thinking.type").String(); got != "enabled" {
		t.Fatalf("thinking was not preserved body=%s", string(capturedBody))
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Blocked") != "" {
		t.Fatalf("unexpected blocked header = %q", rec.Header().Get("X-Blocked"))
	}
	if result.RequestID != "deepseek-msg-1" || result.Model != "deepseek-v4-pro" || result.UpstreamModel != "deepseek-v4-pro" {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Usage.InputTokens != 11 || result.Usage.OutputTokens != 7 || result.Usage.CacheReadInputTokens != 3 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestDeepSeekGatewayServiceForwardMessagesPreservesAnthropicHeaders(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"deepseek-v4-flash","usage":{"input_tokens":1,"output_tokens":1}}`)),
		}, nil
	})}
	svc := NewDeepSeekGatewayService(client, nil)
	c, _ := newDeepSeekGatewayTestContext("/v1/messages")
	c.Request.Header.Set("anthropic-version", "2023-06-01")
	c.Request.Header.Set("anthropic-beta", "test-beta")
	body := []byte(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"}]}`)

	_, err := svc.ForwardMessages(context.Background(), c, deepSeekGatewayTestAccount(), body, "req-version")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if got := captured.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("anthropic-version = %q", got)
	}
	if got := captured.Header.Get("anthropic-beta"); got != "test-beta" {
		t.Fatalf("anthropic-beta = %q", got)
	}
}

func TestDeepSeekGatewayServiceForwardChatCompletionsUsesBearerAndParsesCacheUsage(t *testing.T) {
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
			Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"deepseek-chat-1"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chat_1","model":"deepseek-v4-flash","usage":{"prompt_tokens":22,"prompt_cache_miss_tokens":17,"prompt_cache_hit_tokens":5,"completion_tokens":13,"completion_tokens_details":{"reasoning_tokens":9}}}`)),
		}, nil
	})}
	svc := NewDeepSeekGatewayService(client, nil)
	c, _ := newDeepSeekGatewayTestContext("/v1/chat/completions")
	body := []byte(`{"model":"deepseek-v4-flash","reasoning_effort":"medium","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup"}}]}`)

	result, err := svc.ForwardChatCompletions(context.Background(), c, deepSeekGatewayTestAccount(), body, "req-chat")
	if err != nil {
		t.Fatalf("ForwardChatCompletions error = %v", err)
	}
	if got := captured.URL.String(); got != "https://api.deepseek.com/chat/completions" {
		t.Fatalf("upstream url = %q", got)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer sk-deepseek-test" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := captured.Header.Get("x-api-key"); got != "" {
		t.Fatalf("x-api-key should not be sent for OpenAI API, got %q", got)
	}
	if got := gjson.GetBytes(capturedBody, "model").String(); got != "deepseek-v4-flash" {
		t.Fatalf("upstream model = %q body=%s", got, string(capturedBody))
	}
	if got := gjson.GetBytes(capturedBody, "reasoning_effort").String(); got != "medium" {
		t.Fatalf("reasoning_effort was not preserved body=%s", string(capturedBody))
	}
	if result.RequestID != "deepseek-chat-1" || result.Model != "deepseek-v4-flash" || result.UpstreamModel != "deepseek-v4-flash" {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Usage.InputTokens != 17 || result.Usage.OutputTokens != 13 || result.Usage.CacheReadInputTokens != 5 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestDeepSeekGatewayServiceForwardChatCompletionsFallsBackToPromptTokensWhenNoCacheBreakdown(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chat_1","model":"deepseek-v4-pro","usage":{"prompt_tokens":21,"completion_tokens":8}}`)),
		}, nil
	})}
	svc := NewDeepSeekGatewayService(client, nil)
	c, _ := newDeepSeekGatewayTestContext("/v1/chat/completions")
	body := []byte(`{"model":"deepseek-v4-pro","messages":[{"role":"user","content":"hello"}]}`)

	result, err := svc.ForwardChatCompletions(context.Background(), c, deepSeekGatewayTestAccount(), body, "req-chat")
	if err != nil {
		t.Fatalf("ForwardChatCompletions error = %v", err)
	}
	if result.Usage.InputTokens != 21 || result.Usage.OutputTokens != 8 || result.Usage.CacheReadInputTokens != 0 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestDeepSeekGatewayServiceRejectsUnsupportedModelsBeforeForwarding(t *testing.T) {
	tests := []string{"deepseek-chat", "deepseek-reasoner", "claude-sonnet-4-5", "gpt-5.4", " deepseek-v4-flash "}
	for _, model := range tests {
		t.Run(model, func(t *testing.T) {
			forwarded := false
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				forwarded = true
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			})}
			svc := NewDeepSeekGatewayService(client, nil)
			c, _ := newDeepSeekGatewayTestContext("/v1/messages")
			body := []byte(`{"model":` + deepSeekTestQuote(model) + `,"messages":[{"role":"user","content":"hello"}]}`)

			_, err := svc.ForwardMessages(context.Background(), c, deepSeekGatewayTestAccount(), body, "req-unsupported")
			if err == nil {
				t.Fatalf("expected unsupported model error")
			}
			var unsupported *DeepSeekUnsupportedContentError
			if !errors.As(err, &unsupported) {
				t.Fatalf("error type = %T %v", err, err)
			}
			if forwarded {
				t.Fatalf("unsupported DeepSeek model should not be forwarded upstream")
			}
		})
	}
}

func TestDeepSeekGatewayServiceRejectsMultimodalBeforeForwarding(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		body    string
		forward func(context.Context, *DeepSeekGatewayService, *gin.Context, []byte) (*ForwardResult, error)
	}{
		{
			name: "anthropic image",
			path: "/v1/messages",
			body: `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`,
			forward: func(ctx context.Context, svc *DeepSeekGatewayService, c *gin.Context, body []byte) (*ForwardResult, error) {
				return svc.ForwardMessages(ctx, c, deepSeekGatewayTestAccount(), body, "req-reject")
			},
		},
		{
			name: "openai image url",
			path: "/v1/chat/completions",
			body: `{"model":"deepseek-v4-flash","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`,
			forward: func(ctx context.Context, svc *DeepSeekGatewayService, c *gin.Context, body []byte) (*ForwardResult, error) {
				return svc.ForwardChatCompletions(ctx, c, deepSeekGatewayTestAccount(), body, "req-reject")
			},
		},
		{
			name: "anthropic search result",
			path: "/v1/messages",
			body: `{"model":"deepseek-v4-pro","messages":[{"role":"user","content":[{"type":"search_result","content":"x"}]}]}`,
			forward: func(ctx context.Context, svc *DeepSeekGatewayService, c *gin.Context, body []byte) (*ForwardResult, error) {
				return svc.ForwardMessages(ctx, c, deepSeekGatewayTestAccount(), body, "req-reject")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				t.Fatalf("unexpected upstream request")
				return nil, nil
			})}
			svc := NewDeepSeekGatewayService(client, nil)
			c, _ := newDeepSeekGatewayTestContext(tc.path)

			_, err := tc.forward(context.Background(), svc, c, []byte(tc.body))
			if err == nil {
				t.Fatalf("expected unsupported content error")
			}
			var unsupported *DeepSeekUnsupportedContentError
			if !errors.As(err, &unsupported) {
				t.Fatalf("error type = %T %v", err, err)
			}
		})
	}
}

func TestMapDeepSeekUpstreamStatus(t *testing.T) {
	tests := []struct {
		status       int
		clientStatus int
		errorType    string
		retryable    bool
	}{
		{status: http.StatusBadRequest, clientStatus: http.StatusBadRequest, errorType: "invalid_request_error", retryable: false},
		{status: http.StatusUnauthorized, clientStatus: http.StatusBadGateway, errorType: "upstream_auth_error", retryable: false},
		{status: http.StatusPaymentRequired, clientStatus: http.StatusBadGateway, errorType: "insufficient_balance", retryable: false},
		{status: http.StatusForbidden, clientStatus: http.StatusBadGateway, errorType: "upstream_auth_error", retryable: false},
		{status: http.StatusTooManyRequests, clientStatus: http.StatusTooManyRequests, errorType: "rate_limit_error", retryable: true},
		{status: http.StatusServiceUnavailable, clientStatus: http.StatusBadGateway, errorType: "overloaded_error", retryable: true},
		{status: http.StatusInternalServerError, clientStatus: http.StatusBadGateway, errorType: "server_error", retryable: true},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			got := MapDeepSeekUpstreamStatus(tc.status)
			if got.ClientStatus != tc.clientStatus || got.ErrorType != tc.errorType || got.Retryable != tc.retryable {
				t.Fatalf("MapDeepSeekUpstreamStatus(%d) = %+v", tc.status, got)
			}
		})
	}
}

func TestDeepSeekGatewayServiceForwardsStreamingChatCompletionsAndParsesCacheUsage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		stream := strings.Join([]string{
			": keep-alive",
			"",
			`data: {"choices":[{"delta":{"content":"hi"}}]}`,
			"",
			`data: {"usage":{"prompt_tokens":12,"prompt_cache_miss_tokens":9,"prompt_cache_hit_tokens":3,"completion_tokens":4}}`,
			"",
			"data: [DONE]",
			"",
		}, "\n")
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"deepseek-stream-1"}},
			Body:       io.NopCloser(strings.NewReader(stream)),
		}, nil
	})}
	svc := NewDeepSeekGatewayService(client, nil)
	c, rec := newDeepSeekGatewayTestContext("/v1/chat/completions")
	body := []byte(`{"model":"deepseek-v4-flash","stream":true,"messages":[{"role":"user","content":"hello"}]}`)

	result, err := svc.ForwardChatCompletions(context.Background(), c, deepSeekGatewayTestAccount(), body, "req-stream")
	if err != nil {
		t.Fatalf("ForwardChatCompletions error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), ": keep-alive") {
		t.Fatalf("stream keep-alive comment was not forwarded: %s", rec.Body.String())
	}
	if result.RequestID != "deepseek-stream-1" || !result.Stream {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Usage.InputTokens != 9 || result.Usage.OutputTokens != 4 || result.Usage.CacheReadInputTokens != 3 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func deepSeekTestQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}
