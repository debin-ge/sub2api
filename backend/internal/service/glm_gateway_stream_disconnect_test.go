package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// failAfterNWritesResponseWriter 第 k 次写入起返回错误，模拟客户端中途断开。
type failAfterNWritesResponseWriter struct {
	gin.ResponseWriter
	failFrom int
	writes   int
	flushes  int
}

func (w *failAfterNWritesResponseWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.writes >= w.failFrom {
		return 0, errors.New("write failed: client gone")
	}
	return w.ResponseWriter.Write(data)
}

func (w *failAfterNWritesResponseWriter) WriteString(s string) (int, error) {
	return w.Write([]byte(s))
}

func (w *failAfterNWritesResponseWriter) Flush() {
	w.flushes++
	w.ResponseWriter.Flush()
}

// blockUntilCancelReader 读完 data 后阻塞，直到 unblock 关闭再返回 context.Canceled，
// 模拟上游在客户端断开后长时间不结束的流。
type blockUntilCancelReader struct {
	data    []byte
	sent    bool
	unblock <-chan struct{}
}

func (r *blockUntilCancelReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, r.data), nil
	}
	<-r.unblock
	return 0, context.Canceled
}

func (r *blockUntilCancelReader) Close() error { return nil }

// endlessSSEReader 先输出 head，然后无限产生不带 usage 的 SSE 行。
type endlessSSEReader struct {
	head []byte
	sent bool
}

func (r *endlessSSEReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, r.head), nil
	}
	line := []byte("data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"x\"}}\n\n")
	n := 0
	for n+len(line) <= len(p) {
		n += copy(p[n:], line)
	}
	if n == 0 {
		n = copy(p, line)
	}
	return n, nil
}

func (r *endlessSSEReader) Close() error { return nil }

func glmStreamMessagesBody() []byte {
	return []byte(`{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":"hello"}]}`)
}

const glmDisconnectStreamFixture = "event: message_start\n" +
	"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":5,\"cache_creation_input_tokens\":2}}}\n\n" +
	"event: content_block_delta\n" +
	"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n" +
	"event: content_block_delta\n" +
	"data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\" there\"}}\n\n" +
	"event: message_delta\n" +
	"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":9}}\n\n" +
	"data: [DONE]\n\n"

func TestCompatUpstreamContextDetachesOnlyStreams(t *testing.T) {
	type key struct{}
	parent, cancelParent := context.WithCancel(context.WithValue(context.Background(), key{}, "v"))
	defer cancelParent()

	nonStream, cancelNonStream := compatUpstreamContext(parent, false)
	defer cancelNonStream()
	if nonStream != parent {
		t.Fatalf("non-stream upstream ctx should be the request ctx itself")
	}

	streamCtx, cancelStream := compatUpstreamContext(parent, true)
	defer cancelStream()
	if streamCtx.Value(key{}) != "v" {
		t.Fatalf("stream upstream ctx must keep request values")
	}
	cancelParent()
	if streamCtx.Err() != nil {
		t.Fatalf("stream upstream ctx must not be cancelled by request ctx: %v", streamCtx.Err())
	}
	cancelStream()
	if !errors.Is(streamCtx.Err(), context.Canceled) {
		t.Fatalf("stream upstream ctx should be cancellable by drain: %v", streamCtx.Err())
	}
}

// 客户端第 k 次写入失败：停止写入，继续读上游直到 EOF，返回带 usage 与 ClientDisconnect 的结果。
func TestGLMStreamingMessagesClientDisconnectKeepsDrainingUsage(t *testing.T) {
	reqCtx, cancelReq := context.WithCancel(context.Background())
	defer cancelReq()
	var upstreamCtx context.Context
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		upstreamCtx = req.Context()
		// 模拟客户端在上游响应到达前断开：脱钩后的上游上下文不得随之取消。
		cancelReq()
		if upstreamCtx.Err() != nil {
			t.Fatalf("stream upstream ctx cancelled by request ctx: %v", upstreamCtx.Err())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"glm-disc"}},
			Body:       io.NopCloser(bytes.NewBufferString(glmDisconnectStreamFixture)),
		}, nil
	})}
	svc := NewGLMGatewayService(client, nil)
	c, rec := newGLMGatewayTestContext("/v1/messages")
	writer := &failAfterNWritesResponseWriter{ResponseWriter: c.Writer, failFrom: 4}
	c.Writer = writer

	result, err := svc.ForwardMessages(reqCtx, c, glmGatewayTestAccount(), glmStreamMessagesBody(), "req-disc")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v, want nil", err)
	}
	if result == nil || !result.ClientDisconnect || !result.Stream {
		t.Fatalf("result = %+v, want stream result with ClientDisconnect", result)
	}
	if result.Usage.InputTokens != 5 || result.Usage.OutputTokens != 9 || result.Usage.CacheCreationInputTokens != 2 {
		t.Fatalf("usage = %+v, want fully parsed usage after disconnect", result.Usage)
	}
	if result.RequestID != "glm-disc" {
		t.Fatalf("request id = %q", result.RequestID)
	}
	// 断开前写出的行保留，断开后不再写入。
	if !strings.Contains(rec.Body.String(), "message_start") {
		t.Fatalf("expected pre-disconnect lines to be written, body = %q", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "message_delta") || strings.Contains(rec.Body.String(), "[DONE]") {
		t.Fatalf("expected no writes after disconnect, body = %q", rec.Body.String())
	}
	if writer.writes != writer.failFrom {
		t.Fatalf("writes = %d, want exactly %d (stop writing after first failure)", writer.writes, writer.failFrom)
	}
	// 上游请求上下文与请求上下文脱钩（Forward 返回后由 defer cancel 收尾，故只在返回前断言未被取消）。
	if upstreamCtx == nil || upstreamCtx == reqCtx {
		t.Fatalf("stream upstream request must use a detached context")
	}
}

// 请求上下文已取消（客户端已断开）：不写任何字节，但仍读完 usage。
func TestGLMStreamingMessagesRequestContextCancelledStopsWritingButBills(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Context().Err() != nil {
			t.Fatalf("upstream request must not inherit request cancellation")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body:       io.NopCloser(bytes.NewBufferString(glmDisconnectStreamFixture)),
		}, nil
	})}
	svc := NewGLMGatewayService(client, nil)
	c, rec := newGLMGatewayTestContext("/v1/messages")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	c.Request = c.Request.WithContext(cancelled)

	result, err := svc.ForwardMessages(cancelled, c, glmGatewayTestAccount(), glmStreamMessagesBody(), "req-cancelled")
	if err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
	if result == nil || !result.ClientDisconnect {
		t.Fatalf("result = %+v, want ClientDisconnect", result)
	}
	if result.Usage.InputTokens != 5 || result.Usage.OutputTokens != 9 {
		t.Fatalf("usage = %+v", result.Usage)
	}
	if rec.Body.Len() != 0 {
		t.Fatalf("expected no body written to a gone client, got %q", rec.Body.String())
	}
}

// Chat Completions 共享循环：写失败后仍解析到末尾 usage。
func TestGLMStreamingChatCompletionsClientDisconnectKeepsDrainingUsage(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"text/event-stream"}},
			Body: io.NopCloser(bytes.NewBufferString(
				"data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n" +
					"data: {\"choices\":[{\"delta\":{\"content\":\" there\"}}]}\n\n" +
					"data: {\"usage\":{\"prompt_tokens\":21,\"completion_tokens\":8,\"prompt_tokens_details\":{\"cached_tokens\":4}}}\n\n" +
					"data: [DONE]\n\n",
			)),
		}, nil
	})}
	svc := NewGLMGatewayService(client, nil)
	c, _ := newGLMGatewayTestContext("/v1/chat/completions")
	c.Writer = &failAfterNWritesResponseWriter{ResponseWriter: c.Writer, failFrom: 2}
	body := []byte(`{"model":"glm-4.5-air","stream":true,"messages":[{"role":"user","content":"hello"}]}`)

	result, err := svc.ForwardChatCompletions(context.Background(), c, glmGatewayTestAccount(), body, "req-chat-disc")
	if err != nil {
		t.Fatalf("ForwardChatCompletions error = %v", err)
	}
	if result == nil || !result.ClientDisconnect {
		t.Fatalf("result = %+v", result)
	}
	if result.Usage.InputTokens != 21 || result.Usage.OutputTokens != 8 || result.Usage.CacheReadInputTokens != 4 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

// 上游读取失败：已解析到部分 usage 时返回 (result, err)；没有 usage 时返回 (nil, err)。
func TestGLMStreamingMessagesReadErrorReturnsPartialUsageWithError(t *testing.T) {
	newSvc := func(body io.ReadCloser) *GLMGatewayService {
		return NewGLMGatewayService(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"text/event-stream"}},
				Body:       body,
			}, nil
		})}, nil)
	}

	c, _ := newGLMGatewayTestContext("/v1/messages")
	result, err := newSvc(&errorAfterReader{data: []byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":7}}}\n\n")}).
		ForwardMessages(context.Background(), c, glmGatewayTestAccount(), glmStreamMessagesBody(), "req-partial")
	if err == nil {
		t.Fatalf("expected read error")
	}
	if result == nil || result.Usage.InputTokens != 7 || result.ClientDisconnect {
		t.Fatalf("result = %+v, want partial usage without ClientDisconnect", result)
	}

	c, _ = newGLMGatewayTestContext("/v1/messages")
	result, err = newSvc(&errorAfterReader{data: []byte("event: ping\ndata: {\"type\":\"ping\"}\n\n")}).
		ForwardMessages(context.Background(), c, glmGatewayTestAccount(), glmStreamMessagesBody(), "req-no-usage")
	if err == nil {
		t.Fatalf("expected read error")
	}
	if result != nil {
		t.Fatalf("result = %+v, want nil when no usage was parsed", result)
	}
}

// 客户端断开后上游迟迟不结束：drain 定时器到期取消上游读取，按已解析 usage 正常返回。
func TestForwardCompatSSEStreamDrainTimeoutStopsUpstreamRead(t *testing.T) {
	upstreamCtx, cancelUpstream := context.WithCancel(context.Background())
	defer cancelUpstream()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"glm-drain"}},
		Body: &blockUntilCancelReader{
			data:    []byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":3}}}\n\n"),
			unblock: upstreamCtx.Done(),
		},
	}
	c, _ := newGLMGatewayTestContext("/v1/messages")
	c.Writer = &failingGinResponseWriter{ResponseWriter: c.Writer}
	drain := &compatStreamDrain{cancel: cancelUpstream, timeout: 20 * time.Millisecond}

	done := make(chan struct{})
	var result *ForwardResult
	var err error
	go func() {
		defer close(done)
		result, err = (&GLMGatewayService{}).handleStreamingMessagesResponse(resp, c, "claude-sonnet-4-5", "glm-4.7", time.Now(), drain)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("stream loop did not stop after drain timeout")
	}
	if err != nil {
		t.Fatalf("expected drain timeout to end the stream without error, got %v", err)
	}
	if result == nil || !result.ClientDisconnect || result.Usage.InputTokens != 3 {
		t.Fatalf("result = %+v", result)
	}
	if !drain.expired() {
		t.Fatalf("drain should be marked expired")
	}
}

// 客户端断开后上游持续输出但没有 usage：达到字节上限即停止，不无限读取。
func TestForwardCompatSSEStreamDrainByteCapStopsUpstreamRead(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       &endlessSSEReader{head: []byte("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\n")},
	}
	c, _ := newGLMGatewayTestContext("/v1/messages")
	c.Writer = &failingGinResponseWriter{ResponseWriter: c.Writer}

	done := make(chan struct{})
	var result *ForwardResult
	var err error
	go func() {
		defer close(done)
		result, err = (&GLMGatewayService{}).handleStreamingMessagesResponse(resp, c, "claude-sonnet-4-5", "glm-4.7", time.Now(), newCompatStreamDrain(func() {}))
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatalf("stream loop did not stop at drain byte cap")
	}
	if err != nil {
		t.Fatalf("expected byte cap to end the stream without error, got %v", err)
	}
	if result == nil || !result.ClientDisconnect || result.Usage.InputTokens != 2 {
		t.Fatalf("result = %+v", result)
	}
}

// 非流式请求：上游请求沿用请求上下文，客户端断开仍会取消上游。
func TestGLMNonStreamingKeepsRequestContext(t *testing.T) {
	reqCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Context() != reqCtx {
			t.Fatalf("non-stream upstream request must keep the request context")
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"msg_1","usage":{"input_tokens":1,"output_tokens":1}}`)),
		}, nil
	})}
	svc := NewGLMGatewayService(client, nil)
	c, _ := newGLMGatewayTestContext("/v1/messages")
	body := []byte(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)
	if _, err := svc.ForwardMessages(reqCtx, c, glmGatewayTestAccount(), body, "req-non-stream"); err != nil {
		t.Fatalf("ForwardMessages error = %v", err)
	}
}
