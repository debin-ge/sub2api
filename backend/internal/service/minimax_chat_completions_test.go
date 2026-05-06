package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
)

func miniMaxChatCompletionsTestAccount(baseURL string) *Account {
	credentials := map[string]any{
		"api_key": "sk-cp-test",
		"model_mapping": map[string]any{
			"claude-sonnet-4-5": "MiniMax-M2.7",
		},
	}
	if baseURL != "" {
		credentials["base_url_openai"] = baseURL
	}
	return &Account{
		ID:          101,
		Platform:    PlatformMiniMax,
		Type:        AccountTypeAPIKey,
		Credentials: credentials,
		Extra: map[string]any{
			"text_5h_limit": 2,
		},
	}
}

func miniMaxChatCompletionsBody(stream bool) []byte {
	if stream {
		return []byte(`{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":"hello"}],"stream_options":{"include_usage":true}}`)
	}
	return []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)
}

func TestMiniMaxChatCompletionsURL(t *testing.T) {
	account := miniMaxChatCompletionsTestAccount("https://api.minimax.io/v1/")

	got, err := buildMiniMaxChatCompletionsURL(account)
	if err != nil {
		t.Fatalf("buildMiniMaxChatCompletionsURL error = %v", err)
	}
	if got != "https://api.minimax.io/v1/chat/completions" {
		t.Fatalf("chat completions url = %q", got)
	}
}

func TestMiniMaxChatCompletionsURLAllowsChinaRegionHost(t *testing.T) {
	account := miniMaxChatCompletionsTestAccount("https://api.minimaxi.com/v1/")

	got, err := buildMiniMaxChatCompletionsURL(account)
	if err != nil {
		t.Fatalf("buildMiniMaxChatCompletionsURL error = %v", err)
	}
	if got != "https://api.minimaxi.com/v1/chat/completions" {
		t.Fatalf("chat completions url = %q", got)
	}
}

func TestMiniMaxGatewayServiceForwardChatCompletionsBuildsSafeUpstreamRequest(t *testing.T) {
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
				"X-Request-Id": {"up-chat-req-1"},
				"X-Blocked":    {"must-not-pass"},
			},
			Body: io.NopCloser(strings.NewReader(`{"id":"chatcmpl_1","model":"MiniMax-M2.7","choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":3}}}`)),
		}, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, rec := newMiniMaxGatewayTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, miniMaxChatCompletionsTestAccount(""), miniMaxChatCompletionsBody(false), " req-1 ")
	if err != nil {
		t.Fatalf("ForwardChatCompletions error = %v", err)
	}
	if captured == nil {
		t.Fatalf("expected upstream request")
	}

	if got := captured.URL.String(); got != "https://api.minimax.io/v1/chat/completions" {
		t.Fatalf("upstream url = %q", got)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer sk-cp-test" {
		t.Fatalf("Authorization = %q", got)
	}
	for _, key := range []string{"x-api-key", "x-goog-api-key", "Cookie", "Proxy-Authorization"} {
		if got := captured.Header.Get(key); got != "" {
			t.Fatalf("%s should not be forwarded, got %q", key, got)
		}
	}
	if got := captured.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := captured.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("Accept = %q", got)
	}
	if got := gjson.GetBytes(capturedBody, "model").String(); got != "MiniMax-M2.7" {
		t.Fatalf("upstream model = %q body=%s", got, string(capturedBody))
	}

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Request-Id") != "up-chat-req-1" {
		t.Fatalf("x-request-id = %q", rec.Header().Get("X-Request-Id"))
	}
	if rec.Header().Get("X-Blocked") != "" {
		t.Fatalf("unexpected blocked header = %q", rec.Header().Get("X-Blocked"))
	}
	if result.RequestID != "up-chat-req-1" || result.Model != "claude-sonnet-4-5" || result.UpstreamModel != "MiniMax-M2.7" {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Stream {
		t.Fatalf("expected non-stream result")
	}
	if result.Usage.InputTokens != 11 || result.Usage.OutputTokens != 7 || result.Usage.CacheReadInputTokens != 3 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if cache.reserveCalls != 1 || cache.requestID != "req-1" {
		t.Fatalf("reserve calls=%d requestID=%q", cache.reserveCalls, cache.requestID)
	}
	if cache.rollbackCalls != 0 {
		t.Fatalf("unexpected rollback calls=%d", cache.rollbackCalls)
	}
}

func TestMiniMaxGatewayServiceForwardChatCompletionsRelaysStreamingUsage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": {"text/event-stream"},
			},
			Body: io.NopCloser(strings.NewReader("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":8,\"prompt_tokens_details\":{\"cached_tokens\":2}}}\n\ndata: [DONE]\n\n")),
		}, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, rec := newMiniMaxGatewayTestContext()

	result, err := svc.ForwardChatCompletions(context.Background(), c, miniMaxChatCompletionsTestAccount(""), miniMaxChatCompletionsBody(true), "req-1")
	if err != nil {
		t.Fatalf("ForwardChatCompletions error = %v", err)
	}

	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content-type = %q", got)
	}
	if !strings.Contains(rec.Body.String(), "data: [DONE]") {
		t.Fatalf("stream body = %q", rec.Body.String())
	}
	if !result.Stream {
		t.Fatalf("expected stream result")
	}
	if result.Usage.InputTokens != 5 || result.Usage.OutputTokens != 8 || result.Usage.CacheReadInputTokens != 2 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if cache.rollbackCalls != 0 {
		t.Fatalf("unexpected rollback calls=%d", cache.rollbackCalls)
	}
}

func TestMiniMaxGatewayServiceRejectsUnsafeChatCompletionsBaseURLBeforeSendingKey(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected upstream request")
		return nil, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, _ := newMiniMaxGatewayTestContext()

	_, err := svc.ForwardChatCompletions(context.Background(), c, miniMaxChatCompletionsTestAccount("http://127.0.0.1:8080/v1"), miniMaxChatCompletionsBody(false), "req-1")
	if err == nil {
		t.Fatalf("expected unsafe base url error")
	}
	if cache.reserveCalls != 0 {
		t.Fatalf("unsafe base url should not reserve quota, got %d", cache.reserveCalls)
	}
}
