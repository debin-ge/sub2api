package httputil

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type partialThenErrReader struct {
	data []byte
	err  error
}

func (r *partialThenErrReader) Read(p []byte) (int, error) {
	if len(r.data) == 0 {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func TestRequestBodyReadErrorKind(t *testing.T) {
	plainReq := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	canceledReq := plainReq.WithContext(canceledCtx)
	deadlineCtx, cancelDeadline := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelDeadline()
	deadlineReq := plainReq.WithContext(deadlineCtx)

	connReset := &net.OpError{Op: "read", Net: "tcp", Err: os.NewSyscallError("read", syscall.ECONNRESET)}
	timeoutErr := &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded}

	cases := []struct {
		name string
		req  *http.Request
		err  error
		want string
	}{
		{"max bytes wins over canceled context", canceledReq, &http.MaxBytesError{Limit: 10}, "max_bytes"},
		{"truncation wins over canceled context", canceledReq, io.ErrUnexpectedEOF, "truncated_body"},
		{"canceled request context", canceledReq, errors.New("http2: stream closed"), "client_disconnect"},
		{"deadline is not a client disconnect", deadlineReq, errors.New("boom"), "io_read"},
		{"connection reset", plainReq, connReset, "client_disconnect"},
		{"wrapped canceled", plainReq, fmt.Errorf("read: %w", context.Canceled), "client_disconnect"},
		{"truncated upload", plainReq, &BodyReadError{BytesRead: 3, Err: io.ErrUnexpectedEOF}, "truncated_body"},
		{"network timeout", plainReq, timeoutErr, "transport"},
		{"malformed chunked", plainReq, errors.New("malformed chunked encoding"), "io_read"},
		{"nil request", nil, io.ErrUnexpectedEOF, "truncated_body"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, RequestBodyReadErrorKind(tc.req, tc.err))
		})
	}
}

func TestReadRequestBodyWithPreallocReportsBytesReadOnFailure(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Body = io.NopCloser(&partialThenErrReader{data: []byte("0123456789"), err: io.ErrUnexpectedEOF})
	req.ContentLength = 100

	body, err := ReadRequestBodyWithPrealloc(req)
	require.Nil(t, body)
	var readErr *BodyReadError
	require.ErrorAs(t, err, &readErr)
	require.EqualValues(t, 10, readErr.BytesRead)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

func TestReadRequestBodyWithPreallocKeepsMaxBytesError(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(strings.Repeat("x", 64)))
	req.Body = http.MaxBytesReader(rec, req.Body, 8)

	_, err := ReadRequestBodyWithPrealloc(req)
	var maxErr *http.MaxBytesError
	require.ErrorAs(t, err, &maxErr)
	require.EqualValues(t, 8, maxErr.Limit)
}

func TestRequestBodyReadFailureStatusAndLog(t *testing.T) {
	core, logs := observer.New(zap.DebugLevel)
	newReq := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
		req.Header.Set("Content-Encoding", "gzip")
		req.ContentLength = 100
		return req.WithContext(logger.IntoContext(req.Context(), zap.New(core)))
	}

	status, message := RequestBodyReadFailure(newReq(), &BodyReadError{BytesRead: 42, Err: io.ErrUnexpectedEOF})
	require.Equal(t, StatusClientClosedRequest, status)
	require.Equal(t, requestBodyClientClosedMessage, message)

	status, message = RequestBodyReadFailure(newReq(), errors.New("malformed chunked encoding"))
	require.Equal(t, http.StatusBadRequest, status)
	require.Equal(t, "Failed to read request body", message)

	entries := logs.All()
	require.Len(t, entries, 2)
	require.Equal(t, "read request body failed", entries[0].Message)
	fields := entries[0].ContextMap()
	require.Equal(t, "truncated_body", fields["error_kind"])
	require.Equal(t, "unexpected EOF", fields["error"])
	require.Equal(t, "gzip", fields["content_encoding"])
	require.EqualValues(t, 100, fields["content_length"])
	require.EqualValues(t, 42, fields["bytes_read"])
	require.Equal(t, "io_read", entries[1].ContextMap()["error_kind"])
	require.EqualValues(t, -1, entries[1].ContextMap()["bytes_read"])
}

func TestRequestContentEncodingCategoryHidesUnknownValues(t *testing.T) {
	require.Equal(t, "other", requestContentEncodingCategory("private-payload-marker"))
	require.Equal(t, "identity", requestContentEncodingCategory(""))
	require.Equal(t, "gzip", requestContentEncodingCategory(" X-GZIP "))
}

// 真实 HTTP/1.1 连接：客户端声明 Content-Length 后只发一部分就关闭连接，
// 服务端应归类为客户端断开（499），并记录已收到的字节数。
func TestRequestBodyReadFailureOnRealClientAbort(t *testing.T) {
	type result struct {
		status    int
		kind      string
		bytesRead int64
	}
	results := make(chan result, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := ReadRequestBodyWithPrealloc(r)
		if err == nil {
			results <- result{status: http.StatusOK}
			return
		}
		var readErr *BodyReadError
		bytesRead := int64(-1)
		if errors.As(err, &readErr) {
			bytesRead = readErr.BytesRead
		}
		status, _ := RequestBodyReadFailure(r, err)
		results <- result{status: status, kind: RequestBodyReadErrorKind(r, err), bytesRead: bytesRead}
	}))
	defer srv.Close()

	conn, err := net.Dial("tcp", srv.Listener.Addr().String())
	require.NoError(t, err)
	w := bufio.NewWriter(conn)
	_, err = fmt.Fprintf(w, "POST /v1/messages HTTP/1.1\r\nHost: test\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n{\"model\":\"x\"")
	require.NoError(t, err)
	require.NoError(t, w.Flush())
	require.NoError(t, conn.Close())

	select {
	case got := <-results:
		require.Equal(t, StatusClientClosedRequest, got.status)
		require.Equal(t, "truncated_body", got.kind)
		require.EqualValues(t, len(`{"model":"x"`), got.bytesRead)
	case <-time.After(5 * time.Second):
		t.Fatal("server handler did not observe the aborted upload")
	}
}
