package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

func TestMiniMaxGatewayServiceRejectsUnsupportedNonTextBlocksBeforeForwarding(t *testing.T) {
	tests := []string{
		`{"model":"claude-sonnet-4-5","max_tokens":64,"messages":[{"role":"user","content":[{"type":"tool_use","id":"toolu_1","name":"x","input":{}}]}]}`,
		`{"model":"claude-sonnet-4-5","max_tokens":64,"messages":[{"role":"user","content":[{"type":"thinking","thinking":"secret"}]}]}`,
		`{"model":"claude-sonnet-4-5","max_tokens":64,"messages":{"role":"user","content":"hello"}}`,
	}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				t.Fatalf("unexpected upstream request")
				return nil, nil
			})}
			cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
			svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
			c, _ := newMiniMaxGatewayTestContext()

			_, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), []byte(raw), "req-1")
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
		})
	}
}

func TestMiniMaxGatewayServiceRejectsTrailingJSONBeforeForwarding(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected upstream request")
		return nil, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, _ := newMiniMaxGatewayTestContext()
	body := append(miniMaxMessagesBody(false), []byte(` {"extra":true}`)...)

	_, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), body, "req-1")
	if err == nil {
		t.Fatalf("expected parse error")
	}
	if cache.reserveCalls != 0 {
		t.Fatalf("parse error should not reserve quota, got %d", cache.reserveCalls)
	}
}

func TestMiniMaxGatewayServiceRejectsUnsafeBaseURLBeforeSendingKey(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("unexpected upstream request")
		return nil, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, _ := newMiniMaxGatewayTestContext()

	_, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount("http://127.0.0.1:8080/anthropic"), miniMaxMessagesBody(false), "req-1")
	if err == nil {
		t.Fatalf("expected unsafe base url error")
	}
	if cache.reserveCalls != 0 {
		t.Fatalf("unsafe base url should not reserve quota, got %d", cache.reserveCalls)
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

func TestMiniMaxGatewayServiceRollsBackQuotaOnUpstream5xx(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusServiceUnavailable,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"unavailable"}}`)),
		}, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, _ := newMiniMaxGatewayTestContext()

	_, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), miniMaxMessagesBody(false), "req-5xx")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if cache.rollbackCalls != 1 || cache.rollbackRequestID != "req-5xx" {
		t.Fatalf("rollback call = calls %d requestID %q", cache.rollbackCalls, cache.rollbackRequestID)
	}
}

func TestMiniMaxGatewayServiceDoesNotRollbackQuotaOnUpstream4xx(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"bad request"}}`)),
		}, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, _ := newMiniMaxGatewayTestContext()

	_, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), miniMaxMessagesBody(false), "req-4xx")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if cache.rollbackCalls != 0 {
		t.Fatalf("unexpected rollback calls=%d", cache.rollbackCalls)
	}
}

func TestMiniMaxGatewayServiceRejectsOversizedNonStreamResponseAndRollsBack(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", miniMaxNonStreamResponseMaxBytes+1))),
		}, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, _ := newMiniMaxGatewayTestContext()

	_, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), miniMaxMessagesBody(false), "req-large")
	if err == nil {
		t.Fatalf("expected oversized response error")
	}
	if cache.rollbackCalls != 1 || cache.rollbackRequestID != "req-large" {
		t.Fatalf("rollback call = calls %d requestID %q", cache.rollbackCalls, cache.rollbackRequestID)
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

func TestMiniMaxGatewayServiceStreamingRelaysOriginalLineEndings(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(bytes.NewBufferString(
				"event: message_start\r\n" +
					"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\r\n" +
					"\r\n",
			)),
		}, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, rec := newMiniMaxGatewayTestContext()

	result, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), miniMaxMessagesBody(true), "req-crlf")
	if err != nil {
		t.Fatalf("ForwardMessages stream error = %v", err)
	}
	if got := rec.Body.String(); !strings.Contains(got, "\r\n\r\n") {
		t.Fatalf("expected CRLF framing, got %q", got)
	}
	if result.Usage.InputTokens != 1 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

type errorAfterReader struct {
	data []byte
	done bool
}

func (r *errorAfterReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		return copy(p, r.data), nil
	}
	return 0, fmt.Errorf("read failed")
}

func (r *errorAfterReader) Close() error {
	return nil
}

func TestMiniMaxGatewayServiceRollsBackQuotaOnStreamReadError(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       &errorAfterReader{data: []byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":1}}}\n")},
		}, nil
	})}
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxGatewayService(client, NewMiniMaxQuotaService(cache, nil), nil)
	c, _ := newMiniMaxGatewayTestContext()

	_, err := svc.ForwardMessages(context.Background(), c, miniMaxGatewayTestAccount(""), miniMaxMessagesBody(true), "req-stream-error")
	if err == nil {
		t.Fatalf("expected stream read error")
	}
	if cache.rollbackCalls != 1 || cache.rollbackRequestID != "req-stream-error" {
		t.Fatalf("rollback call = calls %d requestID %q", cache.rollbackCalls, cache.rollbackRequestID)
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
