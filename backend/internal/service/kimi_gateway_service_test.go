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

func newKimiGatewayTestContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Request.Header.Set("Authorization", "Bearer client-key")
	c.Request.Header.Set("x-api-key", "anthropic-client-key")
	c.Request.Header.Set("x-goog-api-key", "google-client-key")
	c.Request.Header.Set("Cookie", "session=client")
	c.Request.Header.Set("Proxy-Authorization", "Basic client")
	c.Request.Header.Set("User-Agent", "kimi-test-client")
	return c, rec
}

func kimiGatewayTestAccount() *Account {
	return &Account{
		ID:       301,
		Platform: PlatformKimi,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-kimi-test",
		},
	}
}

func TestKimiGatewayServiceForwardMessagesBuildsSafeUpstreamRequest(t *testing.T) {
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
				"X-Request-Id": {"kimi-msg-1"},
				"X-Blocked":    {"must-not-pass"},
			},
			Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"kimi-for-coding","usage":{"input_tokens":11,"output_tokens":7}}`)),
		}, nil
	})}
	svc := NewKimiGatewayService(client, nil)
	c, rec := newKimiGatewayTestContext("/v1/messages")
	body := []byte(`{"model":"kimi-for-coding","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)

	result, err := svc.ForwardMessages(context.Background(), c, kimiGatewayTestAccount(), body, "req-1")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if got := captured.URL.String(); got != "https://api.kimi.com/coding/v1/messages" {
		t.Fatalf("upstream url = %q", got)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer sk-kimi-test" {
		t.Fatalf("Authorization = %q", got)
	}
	for _, key := range []string{"x-api-key", "x-goog-api-key", "Cookie", "Proxy-Authorization"} {
		if got := captured.Header.Get(key); got != "" {
			t.Fatalf("%s should not be forwarded, got %q", key, got)
		}
	}
	if got := captured.Header.Get("User-Agent"); got != "kimi-test-client" {
		t.Fatalf("User-Agent = %q", got)
	}
	if got := captured.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("anthropic-version = %q", got)
	}
	if got := gjson.GetBytes(capturedBody, "model").String(); got != "kimi-for-coding" {
		t.Fatalf("upstream model = %q body=%s", got, string(capturedBody))
	}
	if got := gjson.GetBytes(capturedBody, "max_tokens").Int(); got != 64 {
		t.Fatalf("max_tokens = %d body=%s", got, string(capturedBody))
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Blocked") != "" {
		t.Fatalf("unexpected blocked header = %q", rec.Header().Get("X-Blocked"))
	}
	if result.RequestID != "kimi-msg-1" || result.Model != "kimi-for-coding" || result.UpstreamModel != "kimi-for-coding" {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Usage.InputTokens != 11 || result.Usage.OutputTokens != 7 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestKimiGatewayServiceForwardMessagesDefaultsMissingAnthropicFields(t *testing.T) {
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
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"kimi-for-coding","usage":{"input_tokens":1,"output_tokens":1}}`)),
		}, nil
	})}
	svc := NewKimiGatewayService(client, nil)
	c, _ := newKimiGatewayTestContext("/v1/messages")
	body := []byte(`{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`)

	_, err := svc.ForwardMessages(context.Background(), c, kimiGatewayTestAccount(), body, "req-defaults")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if got := captured.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("anthropic-version = %q", got)
	}
	if got := gjson.GetBytes(capturedBody, "max_tokens").Int(); got != kimiDefaultAnthropicMaxTokens {
		t.Fatalf("default max_tokens = %d body=%s", got, string(capturedBody))
	}
}

func TestKimiGatewayServiceForwardMessagesPreservesAnthropicVersion(t *testing.T) {
	var captured *http.Request
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		captured = req
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"kimi-for-coding","usage":{"input_tokens":1,"output_tokens":1}}`)),
		}, nil
	})}
	svc := NewKimiGatewayService(client, nil)
	c, _ := newKimiGatewayTestContext("/v1/messages")
	c.Request.Header.Set("anthropic-version", "2023-06-01")
	body := []byte(`{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`)

	_, err := svc.ForwardMessages(context.Background(), c, kimiGatewayTestAccount(), body, "req-version")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if got := captured.Header.Get("anthropic-version"); got != "2023-06-01" {
		t.Fatalf("anthropic-version = %q", got)
	}
}

func TestKimiGatewayServiceRejectsNonKimiModelBeforeForwarding(t *testing.T) {
	forwarded := false
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		forwarded = true
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
	})}
	svc := NewKimiGatewayService(client, nil)
	c, _ := newKimiGatewayTestContext("/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	_, err := svc.ForwardMessages(context.Background(), c, kimiGatewayTestAccount(), body, "req-unsupported")

	if err == nil {
		t.Fatalf("expected unsupported model error")
	}
	var unsupported *KimiUnsupportedContentError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error type = %T %v", err, err)
	}
	if forwarded {
		t.Fatalf("unsupported Kimi model should not be forwarded upstream")
	}
}

func TestKimiGatewayServiceForwardChatCompletionsBuildsRequestAndParsesUsage(t *testing.T) {
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
			Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"kimi-chat-1"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chat_1","model":"kimi-for-coding","usage":{"prompt_tokens":17,"completion_tokens":13,"prompt_tokens_details":{"cached_tokens":5}}}`)),
		}, nil
	})}
	svc := NewKimiGatewayService(client, nil)
	c, _ := newKimiGatewayTestContext("/v1/chat/completions")
	body := []byte(`{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup"}}]}`)

	result, err := svc.ForwardChatCompletions(context.Background(), c, kimiGatewayTestAccount(), body, "req-chat")
	if err != nil {
		t.Fatalf("ForwardChatCompletions error = %v", err)
	}
	if got := captured.URL.String(); got != "https://api.kimi.com/coding/v1/chat/completions" {
		t.Fatalf("upstream url = %q", got)
	}
	if got := gjson.GetBytes(capturedBody, "model").String(); got != "kimi-for-coding" {
		t.Fatalf("upstream model = %q body=%s", got, string(capturedBody))
	}
	if result.RequestID != "kimi-chat-1" || result.Model != "kimi-for-coding" || result.UpstreamModel != "kimi-for-coding" {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Usage.InputTokens != 17 || result.Usage.OutputTokens != 13 || result.Usage.CacheReadInputTokens != 5 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestKimiGatewayServiceRejectsMultimodalBeforeForwarding(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		body    string
		forward func(context.Context, *KimiGatewayService, *gin.Context, []byte) (*ForwardResult, error)
	}{
		{
			name: "anthropic image",
			path: "/v1/messages",
			body: `{"model":"kimi-for-coding","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`,
			forward: func(ctx context.Context, svc *KimiGatewayService, c *gin.Context, body []byte) (*ForwardResult, error) {
				return svc.ForwardMessages(ctx, c, kimiGatewayTestAccount(), body, "req-reject")
			},
		},
		{
			name: "openai image url",
			path: "/v1/chat/completions",
			body: `{"model":"kimi-for-coding","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`,
			forward: func(ctx context.Context, svc *KimiGatewayService, c *gin.Context, body []byte) (*ForwardResult, error) {
				return svc.ForwardChatCompletions(ctx, c, kimiGatewayTestAccount(), body, "req-reject")
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				t.Fatalf("unexpected upstream request")
				return nil, nil
			})}
			svc := NewKimiGatewayService(client, nil)
			c, _ := newKimiGatewayTestContext(tc.path)

			_, err := tc.forward(context.Background(), svc, c, []byte(tc.body))
			if err == nil {
				t.Fatalf("expected unsupported content error")
			}
			var unsupported *KimiUnsupportedContentError
			if !errors.As(err, &unsupported) {
				t.Fatalf("error type = %T %v", err, err)
			}
		})
	}
}
