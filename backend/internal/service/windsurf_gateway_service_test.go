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

func newWindsurfGatewayTestContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Request.Header.Set("Authorization", "Bearer client-key")
	c.Request.Header.Set("x-api-key", "anthropic-client-key")
	c.Request.Header.Set("x-goog-api-key", "google-client-key")
	c.Request.Header.Set("Cookie", "session=client")
	c.Request.Header.Set("Proxy-Authorization", "Basic client")
	c.Request.Header.Set("User-Agent", "windsurf-test-client")
	return c, rec
}

func windsurfGatewayTestAccount() *Account {
	return &Account{
		ID:       501,
		Platform: PlatformWindsurf,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-windsurf-test",
			"base_url": "https://proxy.example/windsurf/",
			"model_mapping": map[string]any{
				"claude-3-5-sonnet-latest": "claude-sonnet-4.6",
			},
		},
	}
}

func TestWindsurfGatewayServiceForwardMessagesUsesBearerHeaderAndSingleBaseURL(t *testing.T) {
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
				"X-Request-Id": {"windsurf-msg-1"},
				"X-Blocked":    {"must-not-pass"},
			},
			Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"claude-sonnet-4.6","usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":3}}`)),
		}, nil
	})}
	svc := NewWindsurfGatewayService(client, nil)
	c, rec := newWindsurfGatewayTestContext("/v1/messages")
	c.Request.Header.Set("anthropic-version", "2023-06-01")
	c.Request.Header.Set("anthropic-beta", "test-beta")
	body := []byte(`{"model":"claude-3-5-sonnet-latest","thinking":{"type":"enabled","budget_tokens":32},"messages":[{"role":"user","content":"hello"}]}`)

	result, err := svc.ForwardMessages(context.Background(), c, windsurfGatewayTestAccount(), body, "req-1")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if got := captured.URL.String(); got != "https://proxy.example/windsurf/v1/messages" {
		t.Fatalf("upstream url = %q", got)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer sk-windsurf-test" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := captured.Header.Get("x-api-key"); got != "" {
		t.Fatalf("x-api-key should not be sent for Windsurf Anthropic API, got %q", got)
	}
	for _, key := range []string{"x-goog-api-key", "Cookie", "Proxy-Authorization"} {
		if got := captured.Header.Get(key); got != "" {
			t.Fatalf("%s should not be forwarded, got %q", key, got)
		}
	}
	if got := captured.Header.Get("User-Agent"); got != "windsurf-test-client" {
		t.Fatalf("User-Agent = %q", got)
	}
	if got := captured.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("anthropic-version = %q", got)
	}
	if got := captured.Header.Get("anthropic-beta"); got != "test-beta" {
		t.Fatalf("anthropic-beta = %q", got)
	}
	if got := gjson.GetBytes(capturedBody, "model").String(); got != "claude-sonnet-4.6" {
		t.Fatalf("upstream model = %q body=%s", got, string(capturedBody))
	}
	if got := gjson.GetBytes(capturedBody, "max_tokens").Int(); got != 4096 {
		t.Fatalf("max_tokens = %d body=%s", got, string(capturedBody))
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
	if result.RequestID != "windsurf-msg-1" || result.Model != "claude-3-5-sonnet-latest" || result.UpstreamModel != "claude-sonnet-4.6" {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Usage.InputTokens != 11 || result.Usage.OutputTokens != 7 || result.Usage.CacheReadInputTokens != 3 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestWindsurfGatewayServiceForwardMessagesDoesNotInjectAnthropicVersion(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"claude-opus-4.6","usage":{"input_tokens":1,"output_tokens":1}}`)),
		}, nil
	})}
	svc := NewWindsurfGatewayService(client, nil)
	c, _ := newWindsurfGatewayTestContext("/v1/messages")
	body := []byte(`{"model":"claude-opus-4.6","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)

	_, err := svc.ForwardMessages(context.Background(), c, windsurfGatewayTestAccount(), body, "req-version")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if got := captured.Header.Get("anthropic-version"); got != "" {
		t.Fatalf("anthropic-version should not be injected, got %q", got)
	}
}

func TestWindsurfGatewayServiceForwardChatCompletionsUsesBearerAndParsesUsage(t *testing.T) {
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
			Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"windsurf-chat-1"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chat_1","model":"claude-sonnet-4.6","usage":{"prompt_tokens":22,"prompt_cache_miss_tokens":17,"prompt_cache_hit_tokens":5,"completion_tokens":13}}`)),
		}, nil
	})}
	svc := NewWindsurfGatewayService(client, nil)
	c, _ := newWindsurfGatewayTestContext("/v1/chat/completions")
	body := []byte(`{"model":"claude-3-5-sonnet-latest","reasoning_effort":"medium","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup"}}]}`)

	result, err := svc.ForwardChatCompletions(context.Background(), c, windsurfGatewayTestAccount(), body, "req-chat")
	if err != nil {
		t.Fatalf("ForwardChatCompletions error = %v", err)
	}
	if got := captured.URL.String(); got != "https://proxy.example/windsurf/v1/chat/completions" {
		t.Fatalf("upstream url = %q", got)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer sk-windsurf-test" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := captured.Header.Get("x-api-key"); got != "" {
		t.Fatalf("x-api-key should not be sent for OpenAI API, got %q", got)
	}
	if got := gjson.GetBytes(capturedBody, "model").String(); got != "claude-sonnet-4.6" {
		t.Fatalf("upstream model = %q body=%s", got, string(capturedBody))
	}
	if got := gjson.GetBytes(capturedBody, "reasoning_effort").String(); got != "medium" {
		t.Fatalf("reasoning_effort was not preserved body=%s", string(capturedBody))
	}
	if result.RequestID != "windsurf-chat-1" || result.Model != "claude-3-5-sonnet-latest" || result.UpstreamModel != "claude-sonnet-4.6" {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Usage.InputTokens != 17 || result.Usage.OutputTokens != 13 || result.Usage.CacheReadInputTokens != 5 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestWindsurfGatewayServiceRejectsUnsupportedContentAndModelsBeforeForwarding(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		body    string
		forward func(context.Context, *WindsurfGatewayService, *gin.Context, []byte) (*ForwardResult, error)
	}{
		{
			name: "unsupported model",
			path: "/v1/messages",
			body: `{"model":"swe-grep","messages":[{"role":"user","content":"hello"}]}`,
			forward: func(ctx context.Context, svc *WindsurfGatewayService, c *gin.Context, body []byte) (*ForwardResult, error) {
				return svc.ForwardMessages(ctx, c, windsurfGatewayTestAccount(), body, "req-reject")
			},
		},
		{
			name: "anthropic image",
			path: "/v1/messages",
			body: `{"model":"claude-sonnet-4.6","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`,
			forward: func(ctx context.Context, svc *WindsurfGatewayService, c *gin.Context, body []byte) (*ForwardResult, error) {
				return svc.ForwardMessages(ctx, c, windsurfGatewayTestAccount(), body, "req-reject")
			},
		},
		{
			name: "openai image url",
			path: "/v1/chat/completions",
			body: `{"model":"claude-sonnet-4.6","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`,
			forward: func(ctx context.Context, svc *WindsurfGatewayService, c *gin.Context, body []byte) (*ForwardResult, error) {
				return svc.ForwardChatCompletions(ctx, c, windsurfGatewayTestAccount(), body, "req-reject")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				t.Fatalf("unexpected upstream request")
				return nil, nil
			})}
			svc := NewWindsurfGatewayService(client, nil)
			c, _ := newWindsurfGatewayTestContext(tc.path)

			_, err := tc.forward(context.Background(), svc, c, []byte(tc.body))
			if err == nil {
				t.Fatalf("expected unsupported content error")
			}
			var unsupported *WindsurfUnsupportedContentError
			if !errors.As(err, &unsupported) {
				t.Fatalf("error type = %T %v", err, err)
			}
		})
	}
}
