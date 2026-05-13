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

const deepseekDefaultAnthropicMaxTokens = int64(4096)

type DeepSeekGatewayService struct {
	httpClient           *http.Client
	responseHeaderFilter *responseheaders.CompiledHeaderFilter
}

type DeepSeekUnsupportedContentError struct {
	Message string
}

func (e *DeepSeekUnsupportedContentError) Error() string {
	return e.Message
}

type DeepSeekUpstreamStatusMapping struct {
	ClientStatus int
	ErrorType    string
	Retryable    bool
}

func MapDeepSeekUpstreamStatus(status int) DeepSeekUpstreamStatusMapping {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return DeepSeekUpstreamStatusMapping{
			ClientStatus: http.StatusBadGateway,
			ErrorType:    "upstream_auth_error",
			Retryable:    false,
		}
	case http.StatusPaymentRequired:
		return DeepSeekUpstreamStatusMapping{
			ClientStatus: http.StatusBadGateway,
			ErrorType:    "insufficient_balance",
			Retryable:    false,
		}
	case http.StatusTooManyRequests:
		return DeepSeekUpstreamStatusMapping{
			ClientStatus: http.StatusTooManyRequests,
			ErrorType:    "rate_limit_error",
			Retryable:    true,
		}
	case http.StatusServiceUnavailable, 529:
		return DeepSeekUpstreamStatusMapping{
			ClientStatus: http.StatusBadGateway,
			ErrorType:    "overloaded_error",
			Retryable:    true,
		}
	default:
		if status >= http.StatusInternalServerError {
			return DeepSeekUpstreamStatusMapping{
				ClientStatus: http.StatusBadGateway,
				ErrorType:    "server_error",
				Retryable:    true,
			}
		}
		return DeepSeekUpstreamStatusMapping{
			ClientStatus: status,
			ErrorType:    "invalid_request_error",
			Retryable:    false,
		}
	}
}

func NewDeepSeekGatewayService(httpClient *http.Client, responseHeaderFilter *responseheaders.CompiledHeaderFilter) *DeepSeekGatewayService {
	return NewDeepSeekGatewayServiceWithTimeout(httpClient, responseHeaderFilter, compatibleGatewayDefaultUpstreamTimeout)
}

func NewDeepSeekGatewayServiceWithTimeout(httpClient *http.Client, responseHeaderFilter *responseheaders.CompiledHeaderFilter, upstreamTimeout time.Duration) *DeepSeekGatewayService {
	if httpClient == nil {
		httpClient = newDefaultCompatibleGatewayHTTPClient(upstreamTimeout)
	}
	return &DeepSeekGatewayService{
		httpClient:           httpClient,
		responseHeaderFilter: responseHeaderFilter,
	}
}

func (s *DeepSeekGatewayService) ForwardMessages(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("deepseek gateway service unavailable")
	}
	start := time.Now()

	if _, err := validateDeepSeekAccount(account); err != nil {
		return nil, err
	}
	if err := rejectDeepSeekAnthropicUnsupportedContent(body); err != nil {
		return nil, err
	}

	upstreamReq, originalModel, upstreamModel, err := s.buildMessagesRequest(ctx, c, account, body)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("deepseek upstream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if shouldReturnDeepSeekUpstreamError(resp.StatusCode) {
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

	compat := s.glmResponseCompat()
	if gjson.GetBytes(body, "stream").Bool() {
		return compat.handleStreamingMessagesResponse(resp, c, originalModel, upstreamModel, start)
	}
	return compat.handleNonStreamingMessagesResponse(resp, c, originalModel, upstreamModel, start)
}

func (s *DeepSeekGatewayService) ForwardChatCompletions(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("deepseek gateway service unavailable")
	}
	start := time.Now()

	if _, err := validateDeepSeekAccount(account); err != nil {
		return nil, err
	}
	if err := rejectDeepSeekOpenAIUnsupportedContent(body); err != nil {
		return nil, err
	}

	upstreamReq, originalModel, upstreamModel, err := s.buildChatCompletionsRequest(ctx, c, account, body)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("deepseek upstream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if shouldReturnDeepSeekUpstreamError(resp.StatusCode) {
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

func (s *DeepSeekGatewayService) glmResponseCompat() *GLMGatewayService {
	return &GLMGatewayService{responseHeaderFilter: s.responseHeaderFilter}
}

func shouldReturnDeepSeekUpstreamError(status int) bool {
	return status >= http.StatusBadRequest
}

func validateDeepSeekAccount(account *Account) (string, error) {
	if account == nil || !account.IsDeepSeekAPIKey() {
		return "", fmt.Errorf("invalid deepseek account")
	}
	apiKey := account.GetDeepSeekAPIKey()
	if apiKey == "" {
		return "", fmt.Errorf("deepseek api key is required")
	}
	return apiKey, nil
}

func (s *DeepSeekGatewayService) buildMessagesRequest(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
	apiKey, err := validateDeepSeekAccount(account)
	if err != nil {
		return nil, "", "", err
	}
	upstreamBody, originalModel, upstreamModel, err := rewriteDeepSeekAnthropicMessagesBody(body, account)
	if err != nil {
		return nil, "", "", err
	}
	upstreamURL := strings.TrimRight(account.GetDeepSeekAnthropicBaseURL(), "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, "", "", err
	}
	setDeepSeekAnthropicHeaders(req, c, apiKey)
	return req, originalModel, upstreamModel, nil
}

func (s *DeepSeekGatewayService) buildChatCompletionsRequest(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
	apiKey, err := validateDeepSeekAccount(account)
	if err != nil {
		return nil, "", "", err
	}
	upstreamBody, originalModel, upstreamModel, err := rewriteDeepSeekModel(body, account)
	if err != nil {
		return nil, "", "", err
	}
	upstreamURL := strings.TrimRight(account.GetDeepSeekOpenAIBaseURL(), "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, "", "", err
	}
	setDeepSeekOpenAIHeaders(req, c, apiKey)
	return req, originalModel, upstreamModel, nil
}

func setDeepSeekCommonHeaders(req *http.Request, c *gin.Context) {
	req.Header.Del("authorization")
	req.Header.Del("x-api-key")
	req.Header.Del("x-goog-api-key")
	req.Header.Del("cookie")
	req.Header.Del("proxy-authorization")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c != nil {
		req.Header.Set("User-Agent", strings.TrimSpace(c.GetHeader("User-Agent")))
		if req.Header.Get("User-Agent") == "" {
			req.Header.Del("User-Agent")
		}
	}
}

func setDeepSeekOpenAIHeaders(req *http.Request, c *gin.Context, apiKey string) {
	setDeepSeekCommonHeaders(req, c)
	req.Header.Set("Authorization", "Bearer "+apiKey)
}

func rewriteDeepSeekModel(body []byte, account *Account) ([]byte, string, string, error) {
	payload, model, upstreamModel, err := validateDeepSeekModelPayload(body, account)
	if err != nil {
		return nil, "", "", err
	}
	payload["model"] = upstreamModel
	upstreamBody, err := json.Marshal(payload)
	if err != nil {
		return nil, "", "", fmt.Errorf("rewrite deepseek request model: %w", err)
	}
	return upstreamBody, model, upstreamModel, nil
}

func rewriteDeepSeekAnthropicMessagesBody(body []byte, account *Account) ([]byte, string, string, error) {
	payload, model, upstreamModel, err := validateDeepSeekModelPayload(body, account)
	if err != nil {
		return nil, "", "", err
	}
	payload["model"] = upstreamModel
	if value, ok := payload["max_tokens"]; !ok || value == nil {
		payload["max_tokens"] = deepseekDefaultAnthropicMaxTokens
	}
	upstreamBody, err := json.Marshal(payload)
	if err != nil {
		return nil, "", "", fmt.Errorf("rewrite deepseek messages request: %w", err)
	}
	return upstreamBody, model, upstreamModel, nil
}

func validateDeepSeekModelPayload(body []byte, account *Account) (map[string]any, string, string, error) {
	payload, err := decodeGLMPayload(body)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse deepseek request: %w", err)
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, "", "", fmt.Errorf("deepseek request model is required")
	}
	if account == nil || !account.IsDeepSeekModelSupported(model) {
		return nil, "", "", &DeepSeekUnsupportedContentError{Message: "DeepSeek gateway only supports deepseek-v4-flash and deepseek-v4-pro"}
	}
	return payload, model, account.GetDeepSeekMappedModel(model), nil
}

func isOfficialDeepSeekModel(model string) bool {
	for _, supported := range DefaultDeepSeekModelIDs() {
		if model == supported {
			return true
		}
	}
	return false
}

func rejectDeepSeekAnthropicUnsupportedContent(body []byte) error {
	payload, err := decodeGLMPayload(body)
	if err != nil {
		return fmt.Errorf("parse deepseek messages request: %w", err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		return &DeepSeekUnsupportedContentError{Message: "deepseek gateway requires messages array"}
	}
	if system, ok := payload["system"]; ok && !deepSeekAnthropicContentIsSupported(system) {
		return &DeepSeekUnsupportedContentError{Message: "deepseek gateway does not support multimodal content"}
	}
	for _, message := range messages {
		msg, ok := message.(map[string]any)
		if !ok {
			return &DeepSeekUnsupportedContentError{Message: "deepseek gateway requires message objects"}
		}
		if !deepSeekAnthropicContentIsSupported(msg["content"]) {
			return &DeepSeekUnsupportedContentError{Message: "deepseek gateway does not support multimodal content"}
		}
	}
	return nil
}

func deepSeekAnthropicContentIsSupported(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return true
	case []any:
		for _, item := range v {
			block, ok := item.(map[string]any)
			if !ok || !deepSeekAnthropicBlockIsSupported(block) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func deepSeekAnthropicBlockIsSupported(block map[string]any) bool {
	typ, _ := block["type"].(string)
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "text", "tool_use", "thinking":
		return true
	case "tool_result":
		if nested, ok := block["content"]; ok {
			return deepSeekAnthropicToolResultContentIsSupported(nested)
		}
		return true
	default:
		return false
	}
}

func deepSeekAnthropicToolResultContentIsSupported(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case string:
		return true
	case []any:
		for _, item := range v {
			block, ok := item.(map[string]any)
			if !ok || !deepSeekAnthropicBlockIsSupported(block) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func rejectDeepSeekOpenAIUnsupportedContent(body []byte) error {
	payload, err := decodeGLMPayload(body)
	if err != nil {
		return fmt.Errorf("parse deepseek chat completions request: %w", err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		return &DeepSeekUnsupportedContentError{Message: "deepseek gateway requires messages array"}
	}
	for _, message := range messages {
		msg, ok := message.(map[string]any)
		if !ok {
			return &DeepSeekUnsupportedContentError{Message: "deepseek gateway requires message objects"}
		}
		if !glmOpenAIContentIsSupported(msg["content"]) {
			return &DeepSeekUnsupportedContentError{Message: "deepseek gateway does not support multimodal content"}
		}
	}
	return nil
}

func setDeepSeekAnthropicHeaders(req *http.Request, c *gin.Context, apiKey string) {
	if req == nil {
		return
	}
	setDeepSeekCommonHeaders(req, c)
	req.Header.Set("x-api-key", apiKey)
	if c != nil {
		if beta := strings.TrimSpace(c.GetHeader("anthropic-beta")); beta != "" {
			req.Header.Set("anthropic-beta", beta)
		}
		if version := strings.TrimSpace(c.GetHeader("anthropic-version")); version != "" {
			req.Header.Set("anthropic-version", version)
			return
		}
	}
	req.Header.Set("anthropic-version", "2023-06-01")
}

func parseDeepSeekOpenAIUsage(body []byte) *ClaudeUsage {
	usage := &ClaudeUsage{}
	if len(body) == 0 {
		return usage
	}
	mergeDeepSeekOpenAIUsage(usage, gjson.ParseBytes(body).Get("usage"))
	return usage
}

func parseDeepSeekOpenAIStreamingUsage(data string, usage *ClaudeUsage) {
	if usage == nil || strings.TrimSpace(data) == "" {
		return
	}
	mergeDeepSeekOpenAIUsage(usage, gjson.Parse(data).Get("usage"))
}

func mergeDeepSeekOpenAIUsage(usage *ClaudeUsage, usageNode gjson.Result) {
	if usage == nil || !usageNode.Exists() {
		return
	}
	miss := usageNode.Get("prompt_cache_miss_tokens")
	hit := usageNode.Get("prompt_cache_hit_tokens")
	if miss.Exists() || hit.Exists() {
		usage.InputTokens = int(miss.Int())
		usage.CacheReadInputTokens = int(hit.Int())
	} else if input := usageNode.Get("prompt_tokens"); input.Exists() {
		usage.InputTokens = int(input.Int())
	}
	if output := usageNode.Get("completion_tokens"); output.Exists() {
		usage.OutputTokens = int(output.Int())
	}
}

func (s *DeepSeekGatewayService) handleNonStreamingChatCompletionsResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time) (*ForwardResult, error) {
	body, err := readGLMNonStreamResponseBody(resp.Body)
	if err != nil {
		return nil, err
	}
	usage := parseDeepSeekOpenAIUsage(body)
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

func (s *DeepSeekGatewayService) handleStreamingChatCompletionsResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time) (*ForwardResult, error) {
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
				return nil, fmt.Errorf("deepseek upstream stream line too large")
			}
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if data != "" && data != "[DONE]" {
					parseDeepSeekOpenAIStreamingUsage(data, usage)
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
