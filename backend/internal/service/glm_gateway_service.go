package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	glmNonStreamResponseMaxBytes = 2 << 20
	glmUpstreamDialTimeout       = compatibleGatewayDialTimeout
	glmUpstreamTLSHandshake      = compatibleGatewayTLSHandshakeTimeout
	glmUpstreamHeaderTimeout     = compatibleGatewayDefaultUpstreamTimeout
	glmUpstreamIdleConnTimeout   = compatibleGatewayIdleConnTimeout
	glmDefaultAnthropicMaxTokens = int64(4096)

	// compatStreamDrainTimeout 客户端中途断开后，兼容网关（GLM/Kimi/DeepSeek/Windsurf/MiniMax）
	// 继续读取上游以收集 usage 的最长时长；超时后取消上游请求，按已解析到的 usage 结算。
	compatStreamDrainTimeout = 30 * time.Second
	// compatStreamDrainMaxBytes 客户端断开后最多再从上游读取的字节数，防止不带 usage 的长流拖住 goroutine。
	compatStreamDrainMaxBytes = 4 << 20
)

type GLMGatewayService struct {
	httpClient           *http.Client
	responseHeaderFilter *responseheaders.CompiledHeaderFilter
}

type GLMUnsupportedContentError struct {
	Message string
}

func (e *GLMUnsupportedContentError) Error() string {
	return e.Message
}

type GLMUpstreamStatusMapping struct {
	ClientStatus int
	ErrorType    string
	Retryable    bool
}

func MapGLMUpstreamStatus(status int) GLMUpstreamStatusMapping {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return GLMUpstreamStatusMapping{
			ClientStatus: http.StatusBadGateway,
			ErrorType:    "upstream_auth_error",
			Retryable:    false,
		}
	case http.StatusTooManyRequests:
		return GLMUpstreamStatusMapping{
			ClientStatus: http.StatusTooManyRequests,
			ErrorType:    "rate_limit_error",
			Retryable:    true,
		}
	case 529:
		return GLMUpstreamStatusMapping{
			ClientStatus: http.StatusBadGateway,
			ErrorType:    "overloaded_error",
			Retryable:    true,
		}
	default:
		if status >= http.StatusInternalServerError {
			return GLMUpstreamStatusMapping{
				ClientStatus: http.StatusBadGateway,
				ErrorType:    "server_error",
				Retryable:    true,
			}
		}
		return GLMUpstreamStatusMapping{
			ClientStatus: status,
			ErrorType:    "invalid_request_error",
			Retryable:    false,
		}
	}
}

func NewGLMGatewayService(httpClient *http.Client, responseHeaderFilter *responseheaders.CompiledHeaderFilter) *GLMGatewayService {
	return NewGLMGatewayServiceWithTimeout(httpClient, responseHeaderFilter, glmUpstreamHeaderTimeout)
}

func NewGLMGatewayServiceWithTimeout(httpClient *http.Client, responseHeaderFilter *responseheaders.CompiledHeaderFilter, upstreamTimeout time.Duration) *GLMGatewayService {
	if httpClient == nil {
		httpClient = newDefaultGLMHTTPClientWithTimeout(upstreamTimeout)
	}
	return &GLMGatewayService{
		httpClient:           httpClient,
		responseHeaderFilter: responseHeaderFilter,
	}
}

func newDefaultGLMHTTPClientWithTimeout(upstreamTimeout time.Duration) *http.Client {
	return newDefaultCompatibleGatewayHTTPClient(upstreamTimeout)
}

func (s *GLMGatewayService) ForwardMessages(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("glm gateway service unavailable")
	}
	start := time.Now()

	if _, err := validateGLMAccount(account); err != nil {
		return nil, err
	}
	if err := rejectGLMAnthropicUnsupportedContent(body); err != nil {
		return nil, err
	}

	stream := gjson.GetBytes(body, "stream").Bool()
	upstreamCtx, cancelUpstream := compatUpstreamContext(ctx, stream)
	defer cancelUpstream()

	upstreamReq, originalModel, upstreamModel, err := s.buildMessagesRequest(upstreamCtx, c, account, body)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("glm upstream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if shouldReturnGLMUpstreamError(resp.StatusCode) {
		body, readErr := readGLMNonStreamResponseBody(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		return nil, &UpstreamFailoverError{
			StatusCode:      resp.StatusCode,
			ResponseBody:    body,
			ResponseHeaders: resp.Header.Clone(),
		}
	}

	if stream {
		return s.handleStreamingMessagesResponse(resp, c, originalModel, upstreamModel, start, newCompatStreamDrain(cancelUpstream))
	}
	return s.handleNonStreamingMessagesResponse(resp, c, originalModel, upstreamModel, start)
}

func (s *GLMGatewayService) ForwardChatCompletions(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("glm gateway service unavailable")
	}
	start := time.Now()

	if _, err := validateGLMAccount(account); err != nil {
		return nil, err
	}
	if err := rejectGLMOpenAIUnsupportedContent(body); err != nil {
		return nil, err
	}

	stream := gjson.GetBytes(body, "stream").Bool()
	upstreamCtx, cancelUpstream := compatUpstreamContext(ctx, stream)
	defer cancelUpstream()

	upstreamReq, originalModel, upstreamModel, err := s.buildChatCompletionsRequest(upstreamCtx, c, account, body)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("glm upstream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if shouldReturnGLMUpstreamError(resp.StatusCode) {
		body, readErr := readGLMNonStreamResponseBody(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		return nil, &UpstreamFailoverError{
			StatusCode:      resp.StatusCode,
			ResponseBody:    body,
			ResponseHeaders: resp.Header.Clone(),
		}
	}

	if stream {
		return s.handleStreamingChatCompletionsResponse(resp, c, originalModel, upstreamModel, start, newCompatStreamDrain(cancelUpstream))
	}
	return s.handleNonStreamingChatCompletionsResponse(resp, c, originalModel, upstreamModel, start)
}

func (s *GLMGatewayService) ForwardResponses(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("glm gateway service unavailable")
	}
	if _, err := validateGLMAccount(account); err != nil {
		return nil, err
	}
	return forwardProviderResponsesViaAnthropic(ctx, c, providerResponsesAnthropicConfig{
		ServiceName:               "glm",
		HTTPClient:                s.httpClient,
		Account:                   account,
		Body:                      body,
		BuildRequest:              s.buildMessagesRequest,
		ShouldReturnUpstreamError: shouldReturnGLMUpstreamError,
		ReadErrorBody:             readGLMNonStreamResponseBody,
		ResponseHeaderFilter:      s.responseHeaderFilter,
	})
}

func shouldReturnGLMUpstreamError(status int) bool {
	return status >= http.StatusBadRequest
}

func validateGLMAccount(account *Account) (string, error) {
	if account == nil || !account.IsGLMCodingPlan() {
		return "", fmt.Errorf("invalid glm account")
	}
	apiKey := account.GetGLMAPIKey()
	if apiKey == "" {
		return "", fmt.Errorf("glm api key is required")
	}
	return apiKey, nil
}

func (s *GLMGatewayService) buildMessagesRequest(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
	apiKey, err := validateGLMAccount(account)
	if err != nil {
		return nil, "", "", err
	}

	upstreamBody, originalModel, upstreamModel, err := rewriteGLMAnthropicMessagesBody(body, account)
	if err != nil {
		return nil, "", "", err
	}

	upstreamURL := strings.TrimRight(account.GetGLMAnthropicBaseURL(), "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, "", "", err
	}
	setGLMUpstreamHeaders(req, c, apiKey)
	applyInternalRelayHeaderFromContext(ctx, account, req.Header)
	return req, originalModel, upstreamModel, nil
}

func (s *GLMGatewayService) buildChatCompletionsRequest(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
	apiKey, err := validateGLMAccount(account)
	if err != nil {
		return nil, "", "", err
	}

	upstreamBody, originalModel, upstreamModel, err := rewriteGLMModel(body, account)
	if err != nil {
		return nil, "", "", err
	}

	upstreamURL := strings.TrimRight(account.GetGLMOpenAIBaseURL(), "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, "", "", err
	}
	setGLMUpstreamHeaders(req, c, apiKey)
	applyInternalRelayHeaderFromContext(ctx, account, req.Header)
	return req, originalModel, upstreamModel, nil
}

func setGLMUpstreamHeaders(req *http.Request, c *gin.Context, apiKey string) {
	req.Header.Del("authorization")
	req.Header.Del("x-api-key")
	req.Header.Del("x-goog-api-key")
	req.Header.Del("cookie")
	req.Header.Del("proxy-authorization")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c != nil {
		req.Header.Set("User-Agent", strings.TrimSpace(c.GetHeader("User-Agent")))
		if req.Header.Get("User-Agent") == "" {
			req.Header.Del("User-Agent")
		}
	}
}

func rewriteGLMModel(body []byte, account *Account) ([]byte, string, string, error) {
	payload, model, upstreamModel, err := rewriteGLMModelPayload(body, account)
	if err != nil {
		return nil, "", "", err
	}

	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, "", "", fmt.Errorf("rewrite glm request model: %w", err)
	}
	return rewritten, model, upstreamModel, nil
}

func rewriteGLMAnthropicMessagesBody(body []byte, account *Account) ([]byte, string, string, error) {
	payload, model, upstreamModel, err := rewriteGLMModelPayload(body, account)
	if err != nil {
		return nil, "", "", err
	}
	if value, ok := payload["max_tokens"]; !ok || value == nil {
		payload["max_tokens"] = glmDefaultAnthropicMaxTokens
	}

	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, "", "", fmt.Errorf("rewrite glm messages request: %w", err)
	}
	return rewritten, model, upstreamModel, nil
}

func rewriteGLMModelPayload(body []byte, account *Account) (map[string]any, string, string, error) {
	payload, err := decodeGLMPayload(body)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse glm request: %w", err)
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, "", "", fmt.Errorf("glm request model is required")
	}
	if !account.IsGLMModelSupported(model) {
		return nil, "", "", &GLMUnsupportedContentError{Message: fmt.Sprintf("glm model %s is not supported by this account", model)}
	}
	upstreamModel := account.GetGLMMappedModel(model)
	payload["model"] = upstreamModel
	return payload, model, upstreamModel, nil
}

func decodeGLMPayload(body []byte) (map[string]any, error) {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("unexpected trailing json")
		}
		return nil, err
	}
	return payload, nil
}

func rejectGLMAnthropicUnsupportedContent(body []byte) error {
	payload, err := decodeGLMPayload(body)
	if err != nil {
		return fmt.Errorf("parse glm messages request: %w", err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		return &GLMUnsupportedContentError{Message: "glm gateway requires messages array"}
	}
	if system, ok := payload["system"]; ok && !glmAnthropicContentIsSupported(system) {
		return &GLMUnsupportedContentError{Message: "glm gateway does not support multimodal content"}
	}
	for _, message := range messages {
		msg, ok := message.(map[string]any)
		if !ok {
			return &GLMUnsupportedContentError{Message: "glm gateway requires message objects"}
		}
		if !glmAnthropicContentIsSupported(msg["content"]) {
			return &GLMUnsupportedContentError{Message: "glm gateway does not support multimodal content"}
		}
	}
	return nil
}

func glmAnthropicContentIsSupported(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return true
	case []any:
		for _, item := range v {
			block, ok := item.(map[string]any)
			if !ok || !glmAnthropicBlockIsSupported(block) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func glmAnthropicBlockIsSupported(block map[string]any) bool {
	typ, _ := block["type"].(string)
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "text", "tool_use", "thinking", "redacted_thinking":
		return true
	case "tool_result":
		if nested, ok := block["content"]; ok {
			return glmAnthropicToolResultContentIsSupported(nested)
		}
		return true
	default:
		return false
	}
}

func glmAnthropicToolResultContentIsSupported(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return true
	case []any:
		for _, item := range v {
			block, ok := item.(map[string]any)
			if !ok || !glmAnthropicBlockIsSupported(block) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func rejectGLMOpenAIUnsupportedContent(body []byte) error {
	payload, err := decodeGLMPayload(body)
	if err != nil {
		return fmt.Errorf("parse glm chat completions request: %w", err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		return &GLMUnsupportedContentError{Message: "glm gateway requires messages array"}
	}
	for _, message := range messages {
		msg, ok := message.(map[string]any)
		if !ok {
			return &GLMUnsupportedContentError{Message: "glm gateway requires message objects"}
		}
		if !glmOpenAIContentIsSupported(msg["content"]) {
			return &GLMUnsupportedContentError{Message: "glm gateway does not support multimodal content"}
		}
	}
	return nil
}

func glmOpenAIContentIsSupported(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return true
	case []any:
		for _, item := range v {
			block, ok := item.(map[string]any)
			if !ok {
				return false
			}
			if typ, _ := block["type"].(string); strings.ToLower(strings.TrimSpace(typ)) != "text" {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func parseGLMClaudeUsage(body []byte) *ClaudeUsage {
	return parseClaudeUsageFromResponseBody(body)
}

func parseGLMOpenAIUsage(body []byte) *ClaudeUsage {
	usage := &ClaudeUsage{}
	if len(body) == 0 {
		return usage
	}
	mergeGLMOpenAIUsage(usage, gjson.ParseBytes(body).Get("usage"))
	return usage
}

func parseGLMOpenAIStreamingUsage(data string, usage *ClaudeUsage) {
	if usage == nil || strings.TrimSpace(data) == "" {
		return
	}
	mergeGLMOpenAIUsage(usage, gjson.Parse(data).Get("usage"))
}

func mergeGLMOpenAIUsage(usage *ClaudeUsage, usageNode gjson.Result) {
	if usage == nil || !usageNode.Exists() {
		return
	}
	if input := usageNode.Get("prompt_tokens"); input.Exists() {
		usage.InputTokens = int(input.Int())
	}
	if output := usageNode.Get("completion_tokens"); output.Exists() {
		usage.OutputTokens = int(output.Int())
	}
	if cached := usageNode.Get("prompt_tokens_details.cached_tokens"); cached.Exists() {
		usage.CacheReadInputTokens = int(cached.Int())
	}
}

func readGLMNonStreamResponseBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, glmNonStreamResponseMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > glmNonStreamResponseMaxBytes {
		return nil, fmt.Errorf("glm upstream response too large")
	}
	return data, nil
}

func (s *GLMGatewayService) handleNonStreamingMessagesResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time) (*ForwardResult, error) {
	body, err := readGLMNonStreamResponseBody(resp.Body)
	if err != nil {
		return nil, err
	}
	usage := parseGLMClaudeUsage(body)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	if c != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		c.Writer.Header().Set("Content-Type", contentType)
		c.Status(resp.StatusCode)
		if len(body) == 0 {
			c.Writer.WriteHeaderNow()
		} else if _, err := c.Writer.Write(body); err != nil {
			return nil, err
		}
	}
	return &ForwardResult{
		RequestID:     resp.Header.Get("x-request-id"),
		Usage:         *usage,
		Model:         originalModel,
		UpstreamModel: upstreamModel,
		Stream:        false,
		Duration:      time.Since(start),
	}, nil
}

func (s *GLMGatewayService) handleNonStreamingChatCompletionsResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time) (*ForwardResult, error) {
	body, err := readGLMNonStreamResponseBody(resp.Body)
	if err != nil {
		return nil, err
	}
	usage := parseGLMOpenAIUsage(body)
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	if c != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		c.Writer.Header().Set("Content-Type", contentType)
		c.Status(resp.StatusCode)
		if len(body) == 0 {
			c.Writer.WriteHeaderNow()
		} else if _, err := c.Writer.Write(body); err != nil {
			return nil, err
		}
	}
	return &ForwardResult{
		RequestID:     resp.Header.Get("x-request-id"),
		Usage:         *usage,
		Model:         originalModel,
		UpstreamModel: upstreamModel,
		Stream:        false,
		Duration:      time.Since(start),
	}, nil
}

// compatUpstreamContext 构造兼容网关的上游请求上下文。
// 流式请求脱离客户端取消（context.WithoutCancel）：客户端中途断开时仍能读完上游 usage 完成计费（SEC-001），
// 返回的 cancel 交给 compatStreamDrain 在 drain 超时后中断阻塞中的上游读取。
// 非流式请求保持原上下文，客户端断开仍会取消上游请求。
func compatUpstreamContext(ctx context.Context, stream bool) (context.Context, context.CancelFunc) {
	detached, cancel := detachStreamUpstreamContext(ctx, stream)
	if !stream {
		return detached, cancel
	}
	return context.WithCancel(detached)
}

// compatStreamDrain 限制客户端断开后继续读取上游的时长：arm 后启动定时器，
// 到期时标记超时并取消上游请求，使阻塞中的 Read 返回。
type compatStreamDrain struct {
	cancel context.CancelFunc
	// timeout 为 0 时使用 compatStreamDrainTimeout；测试可注入更短的值。
	timeout  time.Duration
	timer    *time.Timer
	timedOut atomic.Bool
}

func newCompatStreamDrain(cancel context.CancelFunc) *compatStreamDrain {
	return &compatStreamDrain{cancel: cancel, timeout: compatStreamDrainTimeout}
}

func (d *compatStreamDrain) arm() {
	if d == nil || d.timer != nil {
		return
	}
	timeout := d.timeout
	if timeout <= 0 {
		timeout = compatStreamDrainTimeout
	}
	d.timer = time.AfterFunc(timeout, func() {
		d.timedOut.Store(true)
		if d.cancel != nil {
			d.cancel()
		}
	})
}

func (d *compatStreamDrain) stop() {
	if d == nil || d.timer == nil {
		return
	}
	d.timer.Stop()
}

func (d *compatStreamDrain) expired() bool {
	return d != nil && d.timedOut.Load()
}

// compatStreamUsageParser 解析单条 SSE data 并合并进 usage。
type compatStreamUsageParser func(data string, usage *ClaudeUsage)

func compatUsageParsed(usage *ClaudeUsage) bool {
	if usage == nil {
		return false
	}
	return usage.InputTokens > 0 || usage.OutputTokens > 0 ||
		usage.CacheCreationInputTokens > 0 || usage.CacheReadInputTokens > 0 ||
		usage.ImageOutputTokens > 0
}

// forwardCompatSSEStream 把上游 SSE 流逐行透传给客户端并解析 usage，是 GLM/Kimi/DeepSeek/Windsurf/MiniMax
// 流式响应的共享循环。
//
// 返回值约定：
//   - 上游正常读到 EOF，或客户端断开后 drain 超时 / 达到字节上限：返回 (result, nil)，
//     result.ClientDisconnect 标记客户端是否中途断开；
//   - 上游读取失败但已解析到部分 usage：返回 (result, err)，result 非空，调用方须据此计费；
//   - 上游读取失败且未解析到任何 usage：返回 (nil, err)。
//
// 客户端写失败或请求上下文已取消后不再写入 / Flush，但继续读取上游直到 EOF 或 drain 上限，
// 以便把已产生的 usage 结算掉，避免客户端主动断开逃避计费（SEC-001）。
func forwardCompatSSEStream(
	resp *http.Response,
	c *gin.Context,
	filter *responseheaders.CompiledHeaderFilter,
	serviceName string,
	parseUsage compatStreamUsageParser,
	originalModel string,
	upstreamModel string,
	start time.Time,
	drain *compatStreamDrain,
) (*ForwardResult, error) {
	usage := &ClaudeUsage{}
	if c != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, filter)
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Status(resp.StatusCode)
	}
	defer drain.stop()

	clientDisconnected := false
	drainedBytes := 0
	markDisconnected := func() {
		if clientDisconnected {
			return
		}
		clientDisconnected = true
		drain.arm()
	}
	buildResult := func() *ForwardResult {
		return &ForwardResult{
			RequestID:        resp.Header.Get("x-request-id"),
			Usage:            *usage,
			Model:            originalModel,
			UpstreamModel:    upstreamModel,
			Stream:           true,
			Duration:         time.Since(start),
			ClientDisconnect: clientDisconnected,
		}
	}
	failWith := func(err error) (*ForwardResult, error) {
		if compatUsageParsed(usage) {
			return buildResult(), err
		}
		return nil, err
	}

	reader := bufio.NewReaderSize(resp.Body, 64*1024)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			if len(line) > defaultMaxLineSize {
				return failWith(fmt.Errorf("%s upstream stream line too large", serviceName))
			}
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if data != "" && data != "[DONE]" {
					parseUsage(data, usage)
				}
			}
			if c != nil && !clientDisconnected {
				if c.Request != nil && c.Request.Context().Err() != nil {
					markDisconnected()
				} else if _, err := io.WriteString(c.Writer, line); err != nil {
					markDisconnected()
				} else if strings.TrimRight(line, "\r\n") == "" {
					if flusher, ok := c.Writer.(http.Flusher); ok {
						flusher.Flush()
					}
				}
			}
			if clientDisconnected {
				drainedBytes += len(line)
				if drainedBytes > compatStreamDrainMaxBytes {
					break
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			if clientDisconnected && drain.expired() {
				break
			}
			return failWith(readErr)
		}
	}

	return buildResult(), nil
}

// handleStreamingMessagesResponse 透传 Anthropic Messages SSE 流；返回值约定见 forwardCompatSSEStream。
func (s *GLMGatewayService) handleStreamingMessagesResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time, drain *compatStreamDrain) (*ForwardResult, error) {
	gatewayUsageParser := &GatewayService{}
	return forwardCompatSSEStream(resp, c, s.responseHeaderFilter, "glm", gatewayUsageParser.parseSSEUsage, originalModel, upstreamModel, start, drain)
}

// handleStreamingChatCompletionsResponse 透传 OpenAI Chat Completions SSE 流；返回值约定见 forwardCompatSSEStream。
func (s *GLMGatewayService) handleStreamingChatCompletionsResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time, drain *compatStreamDrain) (*ForwardResult, error) {
	return forwardCompatSSEStream(resp, c, s.responseHeaderFilter, "glm", parseGLMOpenAIStreamingUsage, originalModel, upstreamModel, start, drain)
}
