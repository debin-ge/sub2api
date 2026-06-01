package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type providerResponsesRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn providerResponsesRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestForwardProviderResponsesViaAnthropicBuildsMessagesAndWritesResponses(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"model":"client-model",
		"input":[
			{"role":"system","content":[{"type":"input_text","text":"answer briefly"}]},
			{"role":"user","content":[{"type":"input_text","text":"hello"}]}
		],
		"reasoning":{"effort":"medium"}
	}`)
	var capturedBody []byte
	httpClient := &http.Client{Transport: providerResponsesRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		return providerResponsesAnthropicSSE("req_success", "upstream-model", "hello from upstream"), nil
	})}

	c, rec := newProviderResponsesAnthropicTestContext("/v1/responses", body)
	result, err := forwardProviderResponsesViaAnthropic(context.Background(), c, providerResponsesAnthropicConfig{
		ServiceName: "test-provider",
		HTTPClient:  httpClient,
		Account:     &Account{ID: 42},
		Body:        body,
		BuildRequest: func(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://provider.test/v1/messages", bytes.NewReader(body))
			return req, "client-model", "upstream-model", err
		},
		ShouldReturnUpstreamError: func(status int) bool { return status >= 400 },
	})

	if err != nil {
		t.Fatalf("forwardProviderResponsesViaAnthropic returned error: %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.RequestID != "req_success" {
		t.Fatalf("RequestID = %q, want req_success", result.RequestID)
	}
	if result.Model != "client-model" {
		t.Fatalf("Model = %q, want client-model", result.Model)
	}
	if result.UpstreamModel != "upstream-model" {
		t.Fatalf("UpstreamModel = %q, want upstream-model", result.UpstreamModel)
	}
	if result.ReasoningEffort == nil || *result.ReasoningEffort != "medium" {
		t.Fatalf("ReasoningEffort = %v, want medium", result.ReasoningEffort)
	}
	if result.Stream {
		t.Fatal("Stream = true, want false")
	}
	if result.Usage.InputTokens != 11 || result.Usage.OutputTokens != 5 {
		t.Fatalf("Usage = %+v, want input=11 output=5", result.Usage)
	}

	if !gjson.GetBytes(capturedBody, "stream").Bool() {
		t.Fatalf("converted upstream body stream = false, want true: %s", capturedBody)
	}
	if got := gjson.GetBytes(capturedBody, "model").String(); got != "client-model" {
		t.Fatalf("converted upstream model = %q, want client-model", got)
	}
	if got := gjson.GetBytes(capturedBody, "messages.0.role").String(); got != "user" {
		t.Fatalf("converted upstream first role = %q, want user: %s", got, capturedBody)
	}
	if got := gjson.GetBytes(capturedBody, "system").String(); got != "answer briefly" {
		t.Fatalf("converted upstream system = %q, want answer briefly: %s", got, capturedBody)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := gjson.Get(rec.Body.String(), "model").String(); got != "client-model" {
		t.Fatalf("response model = %q, want client-model; body=%s", got, rec.Body.String())
	}
	if got := gjson.Get(rec.Body.String(), "output.0.content.0.text").String(); got != "hello from upstream" {
		t.Fatalf("response text = %q, want hello from upstream; body=%s", got, rec.Body.String())
	}
}

func TestForwardProviderResponsesViaAnthropicStreamsResponsesEvents(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"client-model","input":"hello","stream":true}`)
	httpClient := &http.Client{Transport: providerResponsesRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return providerResponsesAnthropicSSE("req_stream", "upstream-model", "stream text"), nil
	})}

	c, rec := newProviderResponsesAnthropicTestContext("/responses", body)
	result, err := forwardProviderResponsesViaAnthropic(context.Background(), c, providerResponsesAnthropicConfig{
		ServiceName: "test-provider",
		HTTPClient:  httpClient,
		Account:     &Account{ID: 42},
		Body:        body,
		BuildRequest: func(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://provider.test/v1/messages", bytes.NewReader(body))
			return req, "client-model", "upstream-model", err
		},
		ShouldReturnUpstreamError: func(status int) bool { return status >= 400 },
	})

	if err != nil {
		t.Fatalf("forwardProviderResponsesViaAnthropic returned error: %v", err)
	}
	if result == nil || !result.Stream {
		t.Fatalf("result.Stream = %v, want true", result != nil && result.Stream)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream", got)
	}
	out := rec.Body.String()
	if !strings.Contains(out, "event: response.created") ||
		!strings.Contains(out, "event: response.output_text.delta") ||
		!strings.Contains(out, "event: response.completed") {
		t.Fatalf("stream body missing Responses SSE events: %s", out)
	}
}

func TestForwardProviderResponsesViaAnthropicValidationErrorDoesNotCallUpstream(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"client-model","previous_response_id":"resp_123","input":"hello"}`)
	c, rec := newProviderResponsesAnthropicTestContext("/v1/responses", body)
	buildCalled := false
	result, err := forwardProviderResponsesViaAnthropic(context.Background(), c, providerResponsesAnthropicConfig{
		ServiceName: "test-provider",
		HTTPClient:  &http.Client{Transport: providerResponsesRoundTripFunc(func(req *http.Request) (*http.Response, error) { return nil, errors.New("unexpected upstream call") })},
		Account:     &Account{ID: 42},
		Body:        body,
		BuildRequest: func(context.Context, *gin.Context, *Account, []byte) (*http.Request, string, string, error) {
			buildCalled = true
			return nil, "", "", errors.New("unexpected build")
		},
		ShouldReturnUpstreamError: func(status int) bool { return status >= 400 },
	})

	if err == nil {
		t.Fatal("expected validation error")
	}
	if result != nil {
		t.Fatalf("result = %+v, want nil", result)
	}
	if buildCalled {
		t.Fatal("BuildRequest was called for validation error")
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
	if got := gjson.Get(rec.Body.String(), "error.type").String(); got != "invalid_request_error" {
		t.Fatalf("error.type = %q, want invalid_request_error; body=%s", got, rec.Body.String())
	}
	if !strings.Contains(gjson.Get(rec.Body.String(), "error.message").String(), "previous_response_id") {
		t.Fatalf("error.message missing previous_response_id: %s", rec.Body.String())
	}
}

func TestForwardProviderResponsesViaAnthropicUpstreamErrorReturnsFailoverWithoutBody(t *testing.T) {
	t.Parallel()

	for _, status := range []int{http.StatusTooManyRequests, http.StatusInternalServerError} {
		status := status
		t.Run(http.StatusText(status), func(t *testing.T) {
			t.Parallel()

			body := []byte(`{"model":"client-model","input":"hello"}`)
			httpClient := &http.Client{Transport: providerResponsesRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: status,
					Header: http.Header{
						"X-Request-Id": []string{"upstream-error"},
						"Retry-After":  []string{"2"},
					},
					Body: io.NopCloser(strings.NewReader(`{"error":{"message":"slow down"}}`)),
				}, nil
			})}

			c, rec := newProviderResponsesAnthropicTestContext("/v1/responses", body)
			result, err := forwardProviderResponsesViaAnthropic(context.Background(), c, providerResponsesAnthropicConfig{
				ServiceName: "test-provider",
				HTTPClient:  httpClient,
				Account:     &Account{ID: 42},
				Body:        body,
				BuildRequest: func(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
					req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://provider.test/v1/messages", bytes.NewReader(body))
					return req, "client-model", "upstream-model", err
				},
				ShouldReturnUpstreamError: func(status int) bool { return status == http.StatusTooManyRequests || status >= 500 },
			})

			if result != nil {
				t.Fatalf("result = %+v, want nil", result)
			}
			var failoverErr *UpstreamFailoverError
			if !errors.As(err, &failoverErr) {
				t.Fatalf("err = %T %v, want UpstreamFailoverError", err, err)
			}
			if failoverErr.StatusCode != status {
				t.Fatalf("StatusCode = %d, want %d", failoverErr.StatusCode, status)
			}
			if string(failoverErr.ResponseBody) != `{"error":{"message":"slow down"}}` {
				t.Fatalf("ResponseBody = %s", failoverErr.ResponseBody)
			}
			if got := failoverErr.ResponseHeaders.Get("Retry-After"); got != "2" {
				t.Fatalf("Retry-After = %q, want 2", got)
			}
			if rec.Body.Len() != 0 {
				t.Fatalf("client body = %q, want empty", rec.Body.String())
			}
		})
	}
}

func newProviderResponsesAnthropicTestContext(path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, rec
}

func providerResponsesAnthropicSSE(requestID, model, text string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type": []string{"text/event-stream"},
			"X-Request-Id": []string{requestID},
		},
		Body: io.NopCloser(strings.NewReader(strings.Join([]string{
			`event: message_start`,
			`data: ` + mustProviderResponsesJSON(map[string]any{
				"type": "message_start",
				"message": map[string]any{
					"id":          "msg_123",
					"type":        "message",
					"role":        "assistant",
					"content":     []any{},
					"model":       model,
					"stop_reason": "",
					"usage": map[string]any{
						"input_tokens":                11,
						"cache_read_input_tokens":     3,
						"cache_creation_input_tokens": 2,
					},
				},
			}),
			``,
			`event: content_block_start`,
			`data: ` + mustProviderResponsesJSON(map[string]any{
				"type":          "content_block_start",
				"index":         0,
				"content_block": map[string]any{"type": "text", "text": ""},
			}),
			``,
			`event: content_block_delta`,
			`data: ` + mustProviderResponsesJSON(map[string]any{
				"type":  "content_block_delta",
				"index": 0,
				"delta": map[string]any{"type": "text_delta", "text": text},
			}),
			``,
			`event: content_block_stop`,
			`data: {"type":"content_block_stop","index":0}`,
			``,
			`event: message_delta`,
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":5}}`,
			``,
			`event: message_stop`,
			`data: {"type":"message_stop"}`,
			``,
		}, "\n"))),
	}
}

func mustProviderResponsesJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(data)
}
