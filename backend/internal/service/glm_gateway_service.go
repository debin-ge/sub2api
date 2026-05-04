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
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const glmNonStreamResponseMaxBytes = 2 << 20

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
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &GLMGatewayService{
		httpClient:           httpClient,
		responseHeaderFilter: responseHeaderFilter,
	}
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

	upstreamReq, originalModel, upstreamModel, err := s.buildMessagesRequest(ctx, c, account, body)
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

	if gjson.GetBytes(body, "stream").Bool() {
		return s.handleStreamingMessagesResponse(resp, c, originalModel, upstreamModel, start)
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

	upstreamReq, originalModel, upstreamModel, err := s.buildChatCompletionsRequest(ctx, c, account, body)
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

	if gjson.GetBytes(body, "stream").Bool() {
		return s.handleStreamingChatCompletionsResponse(resp, c, originalModel, upstreamModel, start)
	}
	return s.handleNonStreamingChatCompletionsResponse(resp, c, originalModel, upstreamModel, start)
}

func shouldReturnGLMUpstreamError(status int) bool {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return true
	}
	return MapGLMUpstreamStatus(status).Retryable
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

	upstreamBody, originalModel, upstreamModel, err := rewriteGLMModel(body, account)
	if err != nil {
		return nil, "", "", err
	}

	upstreamURL := strings.TrimRight(account.GetGLMAnthropicBaseURL(), "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, "", "", err
	}
	setGLMUpstreamHeaders(req, c, apiKey)
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
	payload, err := decodeGLMPayload(body)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse glm request: %w", err)
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, "", "", fmt.Errorf("glm request model is required")
	}
	upstreamModel := account.GetGLMMappedModel(model)
	payload["model"] = upstreamModel

	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, "", "", fmt.Errorf("rewrite glm request model: %w", err)
	}
	return rewritten, model, upstreamModel, nil
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

func (s *GLMGatewayService) handleStreamingMessagesResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time) (*ForwardResult, error) {
	usage := &ClaudeUsage{}
	if c != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Status(resp.StatusCode)
	}

	reader := bufio.NewReaderSize(resp.Body, 64*1024)
	gatewayUsageParser := &GatewayService{}
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			if len(line) > defaultMaxLineSize {
				return nil, fmt.Errorf("glm upstream stream line too large")
			}
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if data != "" && data != "[DONE]" {
					gatewayUsageParser.parseSSEUsage(data, usage)
				}
			}
			if c != nil {
				if _, err := io.WriteString(c.Writer, line); err != nil {
					return nil, err
				}
				if strings.TrimRight(line, "\r\n") == "" {
					if flusher, ok := c.Writer.(http.Flusher); ok {
						flusher.Flush()
					}
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, readErr
		}
	}

	return &ForwardResult{
		RequestID:     resp.Header.Get("x-request-id"),
		Usage:         *usage,
		Model:         originalModel,
		UpstreamModel: upstreamModel,
		Stream:        true,
		Duration:      time.Since(start),
	}, nil
}

func (s *GLMGatewayService) handleStreamingChatCompletionsResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time) (*ForwardResult, error) {
	usage := &ClaudeUsage{}
	if c != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Status(resp.StatusCode)
	}

	reader := bufio.NewReaderSize(resp.Body, 64*1024)
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			if len(line) > defaultMaxLineSize {
				return nil, fmt.Errorf("glm upstream stream line too large")
			}
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if data != "" && data != "[DONE]" {
					parseGLMOpenAIStreamingUsage(data, usage)
				}
			}
			if c != nil {
				if _, err := io.WriteString(c.Writer, line); err != nil {
					return nil, err
				}
				if strings.TrimRight(line, "\r\n") == "" {
					if flusher, ok := c.Writer.(http.Flusher); ok {
						flusher.Flush()
					}
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return nil, readErr
		}
	}

	return &ForwardResult{
		RequestID:     resp.Header.Get("x-request-id"),
		Usage:         *usage,
		Model:         originalModel,
		UpstreamModel: upstreamModel,
		Stream:        true,
		Duration:      time.Since(start),
	}, nil
}
