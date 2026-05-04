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

func newGLMGatewayTestContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, nil)
	c.Request.Header.Set("Authorization", "Bearer client-key")
	c.Request.Header.Set("x-api-key", "anthropic-client-key")
	c.Request.Header.Set("x-goog-api-key", "google-client-key")
	c.Request.Header.Set("Cookie", "session=client")
	c.Request.Header.Set("Proxy-Authorization", "Basic client")
	return c, rec
}

func glmGatewayTestAccount() *Account {
	return &Account{
		ID:       201,
		Platform: PlatformGLM,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-glm-test",
		},
	}
}

func TestGLMGatewayServiceForwardMessagesBuildsSafeUpstreamRequest(t *testing.T) {
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
				"X-Request-Id": {"glm-msg-1"},
				"X-Blocked":    {"must-not-pass"},
			},
			Body: io.NopCloser(strings.NewReader(`{"id":"msg_1","model":"GLM-5.1","usage":{"input_tokens":11,"output_tokens":7,"cache_read_input_tokens":3}}`)),
		}, nil
	})}
	svc := NewGLMGatewayService(client, nil)
	c, rec := newGLMGatewayTestContext("/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-5","max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)

	result, err := svc.ForwardMessages(context.Background(), c, glmGatewayTestAccount(), body, "req-1")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if captured == nil {
		t.Fatalf("expected upstream request")
	}
	if got := captured.URL.String(); got != "https://open.bigmodel.cn/api/anthropic/v1/messages" {
		t.Fatalf("upstream url = %q", got)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer sk-glm-test" {
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
	if got := gjson.GetBytes(capturedBody, "model").String(); got != "GLM-5.1" {
		t.Fatalf("upstream model = %q body=%s", got, string(capturedBody))
	}

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Request-Id") != "glm-msg-1" {
		t.Fatalf("x-request-id = %q", rec.Header().Get("X-Request-Id"))
	}
	if rec.Header().Get("X-Blocked") != "" {
		t.Fatalf("unexpected blocked header = %q", rec.Header().Get("X-Blocked"))
	}
	if result.RequestID != "glm-msg-1" || result.Model != "claude-sonnet-4-5" || result.UpstreamModel != "GLM-5.1" {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Stream {
		t.Fatalf("expected non-stream result")
	}
	if result.Usage.InputTokens != 11 || result.Usage.OutputTokens != 7 || result.Usage.CacheReadInputTokens != 3 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestGLMGatewayServiceForwardChatCompletionsBuildsRequestAndParsesUsage(t *testing.T) {
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
				"X-Request-Id": {"glm-chat-1"},
			},
			Body: io.NopCloser(strings.NewReader(`{"id":"chat_1","model":"GLM-4.5-air","usage":{"prompt_tokens":17,"completion_tokens":13,"prompt_tokens_details":{"cached_tokens":5}}}`)),
		}, nil
	})}
	svc := NewGLMGatewayService(client, nil)
	c, _ := newGLMGatewayTestContext("/v1/chat/completions")
	body := []byte(`{"model":"glm-4.5-air","messages":[{"role":"user","content":"hello"}],"tools":[{"type":"function","function":{"name":"lookup"}}]}`)

	result, err := svc.ForwardChatCompletions(context.Background(), c, glmGatewayTestAccount(), body, "req-chat")
	if err != nil {
		t.Fatalf("ForwardChatCompletions error = %v", err)
	}
	if got := captured.URL.String(); got != "https://open.bigmodel.cn/api/coding/paas/v4/chat/completions" {
		t.Fatalf("upstream url = %q", got)
	}
	if got := captured.Header.Get("Authorization"); got != "Bearer sk-glm-test" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := gjson.GetBytes(capturedBody, "model").String(); got != "GLM-4.5-air" {
		t.Fatalf("upstream model = %q body=%s", got, string(capturedBody))
	}
	if !gjson.GetBytes(capturedBody, "tools.0.function.name").Exists() {
		t.Fatalf("tools not preserved: %s", string(capturedBody))
	}
	if result.RequestID != "glm-chat-1" || result.Model != "glm-4.5-air" || result.UpstreamModel != "GLM-4.5-air" {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.Usage.InputTokens != 17 || result.Usage.OutputTokens != 13 || result.Usage.CacheReadInputTokens != 5 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestGLMGatewayServiceAllowsAnthropicToolAndThinkingContent(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","usage":{"input_tokens":1,"output_tokens":2}}`)),
		}, nil
	})}
	svc := NewGLMGatewayService(client, nil)
	c, _ := newGLMGatewayTestContext("/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[
		{"role":"assistant","content":[{"type":"thinking","thinking":"x"},{"type":"redacted_thinking","data":"x"},{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"ok"},{"type":"text","text":"done"}]}
	]}`)

	if _, err := svc.ForwardMessages(context.Background(), c, glmGatewayTestAccount(), body, "req-allowed"); err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if calls != 1 {
		t.Fatalf("upstream calls = %d", calls)
	}
}

func TestRejectGLMUnsupportedContentAnthropicRejectsMultimodalBeforeForwarding(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "image", body: `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AAAA"}}]}]}`},
		{name: "document", body: `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":[{"type":"document","source":{"type":"text","data":"doc"}}]}]}`},
		{name: "file", body: `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":[{"type":"file","file_id":"file_1"}]}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				t.Fatalf("unexpected upstream request")
				return nil, nil
			})}
			svc := NewGLMGatewayService(client, nil)
			c, _ := newGLMGatewayTestContext("/v1/messages")

			_, err := svc.ForwardMessages(context.Background(), c, glmGatewayTestAccount(), []byte(tc.body), "req-reject")
			if err == nil {
				t.Fatalf("expected unsupported content error")
			}
			var unsupported *GLMUnsupportedContentError
			if !errors.As(err, &unsupported) {
				t.Fatalf("error type = %T %v", err, err)
			}
		})
	}
}

func TestRejectGLMUnsupportedContentOpenAIRejectsMultimodalBeforeForwarding(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "image_url", body: `{"model":"glm-4.5-air","messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`},
		{name: "file", body: `{"model":"glm-4.5-air","messages":[{"role":"user","content":[{"type":"file","file":{"file_id":"file_1"}}]}]}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				t.Fatalf("unexpected upstream request")
				return nil, nil
			})}
			svc := NewGLMGatewayService(client, nil)
			c, _ := newGLMGatewayTestContext("/v1/chat/completions")

			_, err := svc.ForwardChatCompletions(context.Background(), c, glmGatewayTestAccount(), []byte(tc.body), "req-reject")
			if err == nil {
				t.Fatalf("expected unsupported content error")
			}
			var unsupported *GLMUnsupportedContentError
			if !errors.As(err, &unsupported) {
				t.Fatalf("error type = %T %v", err, err)
			}
		})
	}
}

func TestMapGLMUpstreamStatus(t *testing.T) {
	tests := []struct {
		status       int
		clientStatus int
		errorType    string
		retryable    bool
	}{
		{status: http.StatusUnauthorized, clientStatus: http.StatusBadGateway, errorType: "upstream_auth_error", retryable: false},
		{status: http.StatusForbidden, clientStatus: http.StatusBadGateway, errorType: "upstream_auth_error", retryable: false},
		{status: http.StatusTooManyRequests, clientStatus: http.StatusTooManyRequests, errorType: "rate_limit_error", retryable: true},
		{status: 529, clientStatus: http.StatusBadGateway, errorType: "overloaded_error", retryable: true},
		{status: http.StatusServiceUnavailable, clientStatus: http.StatusBadGateway, errorType: "server_error", retryable: true},
		{status: http.StatusBadRequest, clientStatus: http.StatusBadRequest, errorType: "invalid_request_error", retryable: false},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			got := MapGLMUpstreamStatus(tc.status)
			if got.ClientStatus != tc.clientStatus || got.ErrorType != tc.errorType || got.Retryable != tc.retryable {
				t.Fatalf("MapGLMUpstreamStatus(%d) = %+v", tc.status, got)
			}
		})
	}
}

func TestGLMGatewayServiceUpstreamRetryableStatusesReturnFailoverError(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, 529, http.StatusServiceUnavailable, http.StatusUnauthorized, http.StatusForbidden} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Header:     http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"err-req"}},
					Body:       io.NopCloser(strings.NewReader(`{"error":{"message":"upstream failed"}}`)),
				}, nil
			})}
			svc := NewGLMGatewayService(client, nil)
			c, _ := newGLMGatewayTestContext("/v1/messages")
			body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

			_, err := svc.ForwardMessages(context.Background(), c, glmGatewayTestAccount(), body, "req-err")
			if err == nil {
				t.Fatalf("expected failover error")
			}
			var failoverErr *UpstreamFailoverError
			if !errors.As(err, &failoverErr) {
				t.Fatalf("error type = %T %v", err, err)
			}
			if failoverErr.StatusCode != status || !bytes.Contains(failoverErr.ResponseBody, []byte("upstream failed")) {
				t.Fatalf("failover error = %+v body=%s", failoverErr, string(failoverErr.ResponseBody))
			}
			if got := failoverErr.ResponseHeaders.Get("X-Request-Id"); got != "err-req" {
				t.Fatalf("response headers = %v", failoverErr.ResponseHeaders)
			}
		})
	}
}

func TestGLMGatewayServiceForwardsStreamingMessagesAndParsesUsage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := gjson.GetBytes(mustReadBody(t, req.Body), "stream").Bool(); !got {
			t.Fatalf("expected upstream stream body")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": {"text/event-stream"},
				"X-Request-Id": {"glm-stream-msg"},
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
	svc := NewGLMGatewayService(client, nil)
	c, rec := newGLMGatewayTestContext("/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":"hello"}]}`)

	result, err := svc.ForwardMessages(context.Background(), c, glmGatewayTestAccount(), body, "req-stream")
	if err != nil {
		t.Fatalf("ForwardMessages stream error = %v", err)
	}
	if !strings.Contains(rec.Body.String(), "message_start") || !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("stream body = %q", rec.Body.String())
	}
	if !result.Stream || result.RequestID != "glm-stream-msg" {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage.InputTokens != 5 || result.Usage.OutputTokens != 9 || result.Usage.CacheCreationInputTokens != 2 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestGLMGatewayServiceForwardsStreamingChatCompletionsAndParsesUsage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if got := gjson.GetBytes(mustReadBody(t, req.Body), "stream").Bool(); !got {
			t.Fatalf("expected upstream stream body")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": {"text/event-stream"},
				"X-Request-Id": {"glm-stream-chat"},
			},
			Body: io.NopCloser(bytes.NewBufferString(
				"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
					"data: {\"usage\":{\"prompt_tokens\":21,\"completion_tokens\":8,\"prompt_tokens_details\":{\"cached_tokens\":4}}}\n\n" +
					"data: [DONE]\n\n",
			)),
		}, nil
	})}
	svc := NewGLMGatewayService(client, nil)
	c, rec := newGLMGatewayTestContext("/v1/chat/completions")
	body := []byte(`{"model":"glm-4.5-air","stream":true,"messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`)

	result, err := svc.ForwardChatCompletions(context.Background(), c, glmGatewayTestAccount(), body, "req-stream-chat")
	if err != nil {
		t.Fatalf("ForwardChatCompletions stream error = %v", err)
	}
	if !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("stream body = %q", rec.Body.String())
	}
	if !result.Stream || result.RequestID != "glm-stream-chat" {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage.InputTokens != 21 || result.Usage.OutputTokens != 8 || result.Usage.CacheReadInputTokens != 4 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}
