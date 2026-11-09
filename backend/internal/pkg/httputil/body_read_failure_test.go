package httputil

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 真实 HTTP/1.1 连接：客户端声明 Content-Length 后只发一部分就关闭连接。
// 这是 fork 原 499 修复（6f6b958b6）要覆盖的场景，并入上游分类后仍须得到
// 499（客户端断开），不能退回 400，且要记录已收到的字节数。
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
		var readErr *RequestBodyReadError
		bytesRead := int64(-1)
		if errors.As(err, &readErr) {
			bytesRead = readErr.BytesRead
		}
		failure := DescribeRequestBodyReadFailure(r, err)
		results <- result{status: failure.Status, kind: failure.Kind, bytesRead: bytesRead}
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
		require.Equal(t, RequestBodyReadErrorClientDisconnect, got.kind)
		require.EqualValues(t, len(`{"model":"x"`), got.bytesRead)
	case <-time.After(5 * time.Second):
		t.Fatal("server handler did not observe the aborted upload")
	}
}

func TestReadRequestBodyWithPreallocReportsBytesReadOnFailure(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	req.Body = io.NopCloser(&partialThenErrReader{data: []byte("0123456789"), err: io.ErrUnexpectedEOF})
	req.ContentLength = 100

	body, err := ReadRequestBodyWithPrealloc(req)
	require.Nil(t, body)
	var readErr *RequestBodyReadError
	require.ErrorAs(t, err, &readErr)
	require.EqualValues(t, 10, readErr.BytesRead)
	require.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

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
