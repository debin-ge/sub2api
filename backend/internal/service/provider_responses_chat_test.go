package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

func TestForwardProviderResponsesViaChatBuildsChatAndWritesResponses(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"deepseek-chat","input":"hello","reasoning":{"effort":"medium"}}`)
	var capturedBody []byte
	httpClient := &http.Client{Transport: providerResponsesRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		var err error
		capturedBody, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}, "X-Request-Id": []string{"chat-resp-1"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"chat_1","model":"deepseek-v4-flash","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`)),
		}, nil
	})}

	c, rec := newProviderResponsesChatTestContext("/v1/responses", body)
	result, err := forwardProviderResponsesViaChat(context.Background(), c, providerResponsesChatConfig{
		ServiceName: "test-chat",
		HTTPClient:  httpClient,
		Account:     &Account{ID: 1, Platform: PlatformDeepSeek},
		Body:        body,
		BuildRequest: func(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://upstream.example/chat/completions", bytes.NewReader(body))
			return req, "deepseek-chat", "deepseek-v4-flash", err
		},
		ShouldReturnUpstreamError: func(status int) bool { return status >= http.StatusBadRequest },
		ReadErrorBody:             readGLMNonStreamResponseBody,
		ParseUsage:                parseDeepSeekOpenAIUsage,
		ParseStreamingUsage:       parseDeepSeekOpenAIStreamingUsage,
	})

	if err != nil {
		t.Fatalf("forwardProviderResponsesViaChat error = %v", err)
	}
	if result == nil {
		t.Fatal("result is nil")
	}
	if result.RequestID != "chat-resp-1" {
		t.Fatalf("RequestID = %q, want chat-resp-1", result.RequestID)
	}
	if result.Model != "deepseek-chat" || result.UpstreamModel != "deepseek-v4-flash" {
		t.Fatalf("result metadata = %+v", result)
	}
	if result.ReasoningEffort == nil || *result.ReasoningEffort != "medium" {
		t.Fatalf("ReasoningEffort = %v, want medium", result.ReasoningEffort)
	}
	if result.Stream {
		t.Fatal("Stream = true, want false")
	}
	if result.Usage.InputTokens != 4 || result.Usage.OutputTokens != 2 {
		t.Fatalf("Usage = %+v, want input=4 output=2", result.Usage)
	}
	if got := gjson.GetBytes(capturedBody, "model").String(); got != "deepseek-chat" {
		t.Fatalf("upstream model = %q body=%s", got, string(capturedBody))
	}
	if got := gjson.GetBytes(capturedBody, "messages.0.role").String(); got != "user" {
		t.Fatalf("first message role = %q body=%s", got, string(capturedBody))
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := gjson.Get(rec.Body.String(), "object").String(); got != "response" {
		t.Fatalf("response object = %q body=%s", got, rec.Body.String())
	}
	if got := gjson.Get(rec.Body.String(), "model").String(); got != "deepseek-chat" {
		t.Fatalf("response model = %q body=%s", got, rec.Body.String())
	}
	if got := gjson.Get(rec.Body.String(), "output.0.content.0.text").String(); got != "hello" {
		t.Fatalf("response text = %q body=%s", got, rec.Body.String())
	}
}

func TestForwardProviderResponsesViaChatStreamsResponsesEvents(t *testing.T) {
	t.Parallel()

	body := []byte(`{"model":"deepseek-chat","input":"hello","stream":true}`)
	httpClient := &http.Client{Transport: providerResponsesRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}, "X-Request-Id": []string{"chat-stream-1"}},
			Body: io.NopCloser(strings.NewReader(strings.Join([]string{
				`data: {"id":"chat_stream","object":"chat.completion.chunk","model":"deepseek-v4-flash","choices":[{"index":0,"delta":{"role":"assistant"}}]}`,
				``,
				`data: {"id":"chat_stream","object":"chat.completion.chunk","model":"deepseek-v4-flash","choices":[{"index":0,"delta":{"content":"hello"}}]}`,
				``,
				`data: {"id":"chat_stream","object":"chat.completion.chunk","model":"deepseek-v4-flash","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`,
				``,
				`data: [DONE]`,
				``,
			}, "\n"))),
		}, nil
	})}

	c, rec := newProviderResponsesChatTestContext("/v1/responses", body)
	result, err := forwardProviderResponsesViaChat(context.Background(), c, providerResponsesChatConfig{
		ServiceName: "test-chat",
		HTTPClient:  httpClient,
		Account:     &Account{ID: 1, Platform: PlatformDeepSeek},
		Body:        body,
		BuildRequest: func(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://upstream.example/chat/completions", bytes.NewReader(body))
			return req, "deepseek-chat", "deepseek-v4-flash", err
		},
		ShouldReturnUpstreamError: func(status int) bool { return status >= http.StatusBadRequest },
		ReadErrorBody:             readGLMNonStreamResponseBody,
		ParseUsage:                parseDeepSeekOpenAIUsage,
		ParseStreamingUsage:       parseDeepSeekOpenAIStreamingUsage,
	})

	if err != nil {
		t.Fatalf("forwardProviderResponsesViaChat error = %v", err)
	}
	if result == nil || !result.Stream {
		t.Fatalf("result.Stream = %v, want true", result != nil && result.Stream)
	}
	if result.Usage.InputTokens != 5 || result.Usage.OutputTokens != 3 {
		t.Fatalf("Usage = %+v, want input=5 output=3", result.Usage)
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

func newProviderResponsesChatTestContext(path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, rec
}
