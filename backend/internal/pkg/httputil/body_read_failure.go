package httputil

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"

	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// StatusClientClosedRequest 是 nginx 约定的非标准状态码 499：客户端在服务端
// 写出响应前关闭了请求。连接已断开时客户端收不到它，主要用于访问日志与
// 运维错误统计，把「客户端上传途中放弃」与真正的 400 区分开。
const StatusClientClosedRequest = 499

const (
	requestBodyReadFailedMessage   = "Failed to read request body"
	requestBodyClientClosedMessage = "Client closed request before the request body was fully received"
)

// BodyReadError 包装读取请求体时的底层 I/O 错误，附带出错前已收到的字节数，
// 用于区分「一个字节都没收到」与「上传到一半断开」。通过 Unwrap 保持
// errors.Is / errors.As（如 *http.MaxBytesError）语义不变。
type BodyReadError struct {
	BytesRead int64
	Err       error
}

func (e *BodyReadError) Error() string { return e.Err.Error() }

func (e *BodyReadError) Unwrap() error { return e.Err }

// RequestBodyReadFailure 处理「读取请求体失败且不是超过大小上限」的情况：
// 记录不含请求内容的底层原因，并返回应写给客户端的状态码与文案。
// 客户端在上传途中断开归为 499，其余仍为 400 "Failed to read request body"。
// 超限（*http.MaxBytesError）应由调用方在此之前按 413 处理。
func RequestBodyReadFailure(req *http.Request, err error) (int, string) {
	kind := RequestBodyReadErrorKind(req, err)
	logRequestBodyReadFailure(req, err, kind)
	if isClientClosedBodyReadKind(kind) {
		return StatusClientClosedRequest, requestBodyClientClosedMessage
	}
	return http.StatusBadRequest, requestBodyReadFailedMessage
}

// RequestBodyReadErrorKind 把读取请求体失败归类为有限的几种原因，不含请求内容：
//   - truncated_body：连接在声明的长度/分块结束前关闭（io.ErrUnexpectedEOF）；
//   - client_disconnect：连接被重置，或请求 context 已取消（含 HTTP/2 RST_STREAM）；
//   - max_bytes：超过大小上限；
//   - transport：其它网络层错误；
//   - io_read：其它读取错误（如畸形的分块编码）。
func RequestBodyReadErrorKind(req *http.Request, err error) string {
	if err == nil {
		return "none"
	}
	var maxErr *http.MaxBytesError
	if errors.As(err, &maxErr) {
		return "max_bytes"
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) {
		return "client_disconnect"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "truncated_body"
	}
	// 放在具体错误类型之后：连接断开后 net/http 也会取消请求 context，先判
	// context 会把 truncated_body 吞掉。HTTP/2 RST_STREAM 只能靠这一步识别。
	if req != nil && errors.Is(req.Context().Err(), context.Canceled) {
		return "client_disconnect"
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return "transport"
	}
	return "io_read"
}

func isClientClosedBodyReadKind(kind string) bool {
	return kind == "client_disconnect" || kind == "truncated_body"
}

func logRequestBodyReadFailure(req *http.Request, err error, kind string) {
	if err == nil {
		return
	}

	log := logger.L()
	contentLength := int64(-1)
	contentEncoding := "identity"
	if req != nil {
		log = logger.FromContext(req.Context())
		contentLength = req.ContentLength
		contentEncoding = requestContentEncodingCategory(req.Header.Get("Content-Encoding"))
	}
	bytesRead := int64(-1)
	var readErr *BodyReadError
	if errors.As(err, &readErr) {
		bytesRead = readErr.BytesRead
	}

	log.Warn("read request body failed",
		zap.String("error_kind", kind),
		zap.String("error", truncateBodyReadError(err.Error())),
		zap.String("content_encoding", contentEncoding),
		zap.Int64("content_length", contentLength),
		zap.Int64("bytes_read", bytesRead),
	)
}

// 底层读取错误来自 net/http 与系统调用，不含请求体内容；截断仅防止异常超长。
func truncateBodyReadError(msg string) string {
	const maxLen = 256
	if len(msg) > maxLen {
		return msg[:maxLen]
	}
	return msg
}

func requestContentEncodingCategory(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "identity":
		return "identity"
	case "gzip", "x-gzip":
		return "gzip"
	case "zstd":
		return "zstd"
	case "deflate":
		return "deflate"
	default:
		return "other"
	}
}
