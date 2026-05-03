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

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newMiniMaxGatewayTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	c.Request.Header.Set("Authorization", "Bearer client-key")
	c.Request.Header.Set("x-api-key", "anthropic-client-key")
	c.Request.Header.Set("x-goog-api-key", "google-client-key")
	c.Request.Header.Set("Cookie", "session=client")
	c.Request.Header.Set("Proxy-Authorization", "Basic client")
	return c, rec
}

func miniMaxGatewayTestAccount(baseURL string) *Account {
	credentials := map[string]any{
		"api_key": "sk-cp-test",
		"model_mapping": map[string]any{
			"claude-sonnet-4-5": "MiniMax-M2.7",
		},
	}
	if baseURL != "" {
		credentials["base_url_anthropic"] = baseURL
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

func miniMaxMessagesBody(stream bool) []byte {
	if stream {
		return []byte(`{"model":"claude-sonnet-4-5","stream":true,"max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
	}
	return []byte(`{"model":"claude-sonnet-4-5","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)
}

func TestMiniMaxGatewayServiceForwardMessagesBuildsSafeUpstreamRequest(t *testing.T) {
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
			StatusCode: http.StatusCreated,
			Header: http.Header{
				"Content-Type": {"application/json"},
				"X-Request-Id": {"up-req-1"},
				"X-Blocked":    {"must-not-pass"},
			},
			Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"MiniMax-M2.7","usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":3}}`)),
		}, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, rec := newMiniMaxGatewayTestContext()

	result, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), miniMaxMessagesBody(false), " req-1 ")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if captured == nil {
		t.Fatalf("expected upstream request")
	}

	if got := captured.URL.String(); got != "https://api.minimax.io/anthropic/v1/messages" {
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

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) == "" {
		t.Fatalf("expected response body")
	}
	if rec.Header().Get("X-Request-Id") != "up-req-1" {
		t.Fatalf("x-request-id = %q", rec.Header().Get("X-Request-Id"))
	}
	if rec.Header().Get("X-Blocked") != "" {
		t.Fatalf("unexpected blocked header = %q", rec.Header().Get("X-Blocked"))
	}
	if result.RequestID != "up-req-1" || result.Model != "claude-sonnet-4-5" || result.UpstreamModel != "MiniMax-M2.7" {
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

func TestMiniMaxGatewayServiceRejectsUnsupportedImageBeforeForwarding(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected upstream request")
		return nil, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, _ := newMiniMaxGatewayTestContext()
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":64,"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`)

	_, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), body, "req-1")
	if err == nil {
		t.Fatalf("expected unsupported content error")
	}
	var unsupported *MiniMaxUnsupportedContentError
	if !errors.As(err, &unsupported) {
		t.Fatalf("error type = %T %v", err, err)
	}
	if cache.reserveCalls != 0 {
		t.Fatalf("unsupported request should not reserve quota, got %d", cache.reserveCalls)
	}
}

func TestMiniMaxGatewayServiceRollsBackQuotaOnUpstreamRequestError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("dial failed")
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, _ := newMiniMaxGatewayTestContext()

	_, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), miniMaxMessagesBody(false), " req-rollback ")
	if err == nil {
		t.Fatalf("expected upstream error")
	}
	if cache.reserveCalls != 1 {
		t.Fatalf("reserve calls = %d", cache.reserveCalls)
	}
	if cache.rollbackCalls != 1 || cache.rollbackAccountID != 101 || cache.rollbackRequestID != "req-rollback" {
		t.Fatalf("rollback call = calls %d accountID %d requestID %q", cache.rollbackCalls, cache.rollbackAccountID, cache.rollbackRequestID)
	}
}

func TestMiniMaxGatewayServiceForwardsStreamingResponseAndParsesUsage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := gjson.GetBytes(mustReadBody(t, req.Body), "stream").Bool(); !got {
			t.Fatalf("expected upstream stream body")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": {"text/event-stream"},
				"X-Request-Id": {"stream-req-1"},
			},
			Body: io.NopCloser(bytes.NewBufferString(
				"event: message_start\n" +
					"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"cache_creation_input_tokens\":2}}}\n\n" +
					"event: message_delta\n" +
					"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":9}}\n\n" +
					"data: [DONE]\n\n",
			)),
		}, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, rec := newMiniMaxGatewayTestContext()

	result, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), miniMaxMessagesBody(true), "req-stream")
	if err != nil {
		t.Fatalf("ForwardMessages stream error = %v", err)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/event-stream") {
		t.Fatalf("content-type = %q", rec.Header().Get("Content-Type"))
	}
	if !strings.Contains(rec.Body.String(), "message_start") || !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("stream body = %q", rec.Body.String())
	}
	if !result.Stream || result.RequestID != "stream-req-1" {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage.InputTokens != 5 || result.Usage.OutputTokens != 9 || result.Usage.CacheCreationInputTokens != 2 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func mustReadBody(t *testing.T, r io.Reader) []byte {
	t.Helper()
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return body
}
