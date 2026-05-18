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

type OpenCodeGatewayService struct {
	httpClient           *http.Client
	responseHeaderFilter *responseheaders.CompiledHeaderFilter
}

type OpenCodeUnsupportedContentError struct {
	Message string
}

func (e *OpenCodeUnsupportedContentError) Error() string {
	return e.Message
}

type OpenCodeUpstreamStatusMapping struct {
	ClientStatus int
	ErrorType    string
	Retryable    bool
}

func MapOpenCodeUpstreamStatus(status int) OpenCodeUpstreamStatusMapping {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return OpenCodeUpstreamStatusMapping{
			ClientStatus: http.StatusBadGateway,
			ErrorType:    "upstream_auth_error",
			Retryable:    false,
		}
	case http.StatusPaymentRequired:
		return OpenCodeUpstreamStatusMapping{
			ClientStatus: http.StatusBadGateway,
			ErrorType:    "insufficient_balance",
			Retryable:    false,
		}
	case http.StatusTooManyRequests:
		return OpenCodeUpstreamStatusMapping{
			ClientStatus: http.StatusTooManyRequests,
			ErrorType:    "rate_limit_error",
			Retryable:    true,
		}
	case http.StatusServiceUnavailable, 529:
		return OpenCodeUpstreamStatusMapping{
			ClientStatus: http.StatusBadGateway,
			ErrorType:    "overloaded_error",
			Retryable:    true,
		}
	default:
		if status >= http.StatusInternalServerError {
			return OpenCodeUpstreamStatusMapping{
				ClientStatus: http.StatusBadGateway,
				ErrorType:    "server_error",
				Retryable:    true,
			}
		}
		return OpenCodeUpstreamStatusMapping{
			ClientStatus: status,
			ErrorType:    "invalid_request_error",
			Retryable:    false,
		}
	}
}

func NewOpenCodeGatewayService(httpClient *http.Client, responseHeaderFilter *responseheaders.CompiledHeaderFilter) *OpenCodeGatewayService {
	return NewOpenCodeGatewayServiceWithTimeout(httpClient, responseHeaderFilter, compatibleGatewayDefaultUpstreamTimeout)
}

func NewOpenCodeGatewayServiceWithTimeout(httpClient *http.Client, responseHeaderFilter *responseheaders.CompiledHeaderFilter, upstreamTimeout time.Duration) *OpenCodeGatewayService {
	if httpClient == nil {
		httpClient = newDefaultCompatibleGatewayHTTPClient(upstreamTimeout)
	}
	return &OpenCodeGatewayService{
		httpClient:           httpClient,
		responseHeaderFilter: responseHeaderFilter,
	}
}

func (s *OpenCodeGatewayService) ForwardModels(ctx context.Context, c *gin.Context, account *Account, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("opencode gateway service unavailable")
	}
	start := time.Now()

	apiKey, baseURL, err := validateOpenCodeAccount(account)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openCodeEndpointURL(baseURL, "/v1/models"), nil)
	if err != nil {
		return nil, fmt.Errorf("build opencode models request: %w", err)
	}
	setOpenCodeHeaders(req, c, apiKey)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opencode upstream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if shouldReturnOpenCodeUpstreamError(resp.StatusCode) {
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

	body, err := readGLMNonStreamResponseBody(resp.Body)
	if err != nil {
		return nil, err
	}
	writeOpenCodeBufferedResponse(c, resp, s.responseHeaderFilter, body)
	return &ForwardResult{
		RequestID: resp.Header.Get("x-request-id"),
		Duration:  time.Since(start),
	}, nil
}

func (s *OpenCodeGatewayService) ForwardChatCompletions(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("opencode gateway service unavailable")
	}
	start := time.Now()

	upstreamReq, originalModel, upstreamModel, err := s.buildOpenCodeJSONRequest(ctx, c, account, body, "/v1/chat/completions")
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("opencode upstream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if shouldReturnOpenCodeUpstreamError(resp.StatusCode) {
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

	reasoningEffort := extractCCReasoningEffortFromBody(body)
	if gjson.GetBytes(body, "stream").Bool() {
		return s.handleStreamingOpenCodeResponse(resp, c, originalModel, upstreamModel, start, parseOpenCodeChatStreamingUsage, reasoningEffort)
	}
	return s.handleNonStreamingOpenCodeResponse(resp, c, originalModel, upstreamModel, start, parseOpenCodeChatUsage, reasoningEffort)
}

func (s *OpenCodeGatewayService) ForwardResponses(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("opencode gateway service unavailable")
	}
	start := time.Now()

	upstreamReq, originalModel, upstreamModel, err := s.buildOpenCodeJSONRequest(ctx, c, account, body, "/v1/responses")
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("opencode upstream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if shouldReturnOpenCodeUpstreamError(resp.StatusCode) {
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

	reasoningEffort := ExtractResponsesReasoningEffortFromBody(body)
	if gjson.GetBytes(body, "stream").Bool() {
		return s.handleStreamingOpenCodeResponse(resp, c, originalModel, upstreamModel, start, parseOpenCodeResponsesStreamingUsage, reasoningEffort)
	}
	return s.handleNonStreamingOpenCodeResponse(resp, c, originalModel, upstreamModel, start, parseOpenCodeResponsesUsage, reasoningEffort)
}

func (s *OpenCodeGatewayService) buildOpenCodeJSONRequest(ctx context.Context, c *gin.Context, account *Account, body []byte, endpoint string) (*http.Request, string, string, error) {
	apiKey, baseURL, err := validateOpenCodeAccount(account)
	if err != nil {
		return nil, "", "", err
	}
	upstreamBody, originalModel, upstreamModel, err := rewriteOpenCodeModel(body, account)
	if err != nil {
		return nil, "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, openCodeEndpointURL(baseURL, endpoint), bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, "", "", err
	}
	setOpenCodeHeaders(req, c, apiKey)
	return req, originalModel, upstreamModel, nil
}

func validateOpenCodeAccount(account *Account) (string, string, error) {
	if account == nil || !account.IsOpenCodeAPIKey() {
		return "", "", fmt.Errorf("invalid opencode account")
	}
	apiKey := account.GetOpenCodeAPIKey()
	if apiKey == "" {
		return "", "", fmt.Errorf("opencode api key is required")
	}
	baseURL := account.GetOpenCodeBaseURL()
	if baseURL == "" {
		return "", "", fmt.Errorf("opencode base url is required")
	}
	return apiKey, baseURL, nil
}

func openCodeEndpointURL(baseURL string, endpoint string) string {
	normalized := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if strings.HasSuffix(normalized, "/v1") && strings.HasPrefix(endpoint, "/v1/") {
		return normalized + strings.TrimPrefix(endpoint, "/v1")
	}
	return normalized + endpoint
}

func setOpenCodeHeaders(req *http.Request, c *gin.Context, apiKey string) {
	req.Header.Del("authorization")
	req.Header.Del("x-api-key")
	req.Header.Del("x-goog-api-key")
	req.Header.Del("cookie")
	req.Header.Del("proxy-authorization")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	if c != nil {
		req.Header.Set("User-Agent", strings.TrimSpace(c.GetHeader("User-Agent")))
		if req.Header.Get("User-Agent") == "" {
			req.Header.Del("User-Agent")
		}
	}
}

func rewriteOpenCodeModel(body []byte, account *Account) ([]byte, string, string, error) {
	payload, model, upstreamModel, err := validateOpenCodeModelPayload(body, account)
	if err != nil {
		return nil, "", "", err
	}
	payload["model"] = upstreamModel
	upstreamBody, err := json.Marshal(payload)
	if err != nil {
		return nil, "", "", fmt.Errorf("rewrite opencode request model: %w", err)
	}
	return upstreamBody, model, upstreamModel, nil
}

func validateOpenCodeModelPayload(body []byte, account *Account) (map[string]any, string, string, error) {
	payload, err := decodeGLMPayload(body)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse opencode request: %w", err)
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, "", "", fmt.Errorf("opencode request model is required")
	}
	if account == nil || !account.IsOpenCodeModelSupported(model) {
		return nil, "", "", &OpenCodeUnsupportedContentError{Message: "OpenCode gateway does not support model " + model}
	}
	return payload, model, account.GetOpenCodeMappedModel(model), nil
}

func shouldReturnOpenCodeUpstreamError(status int) bool {
	return status >= http.StatusBadRequest
}

func (s *OpenCodeGatewayService) handleNonStreamingOpenCodeResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time, parseUsage func([]byte) *ClaudeUsage, reasoningEffort *string) (*ForwardResult, error) {
	body, err := readGLMNonStreamResponseBody(resp.Body)
	if err != nil {
		return nil, err
	}
	usage := parseUsage(body)
	writeOpenCodeBufferedResponse(c, resp, s.responseHeaderFilter, body)
	return &ForwardResult{
		RequestID:       resp.Header.Get("x-request-id"),
		Usage:           *usage,
		Model:           originalModel,
		UpstreamModel:   upstreamModel,
		Stream:          false,
		Duration:        time.Since(start),
		ReasoningEffort: reasoningEffort,
	}, nil
}

func (s *OpenCodeGatewayService) handleStreamingOpenCodeResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time, parseUsage func(string, *ClaudeUsage), reasoningEffort *string) (*ForwardResult, error) {
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
				return nil, fmt.Errorf("opencode upstream stream line too large")
			}
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if data != "" && data != "[DONE]" {
					parseUsage(data, usage)
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
		RequestID:       resp.Header.Get("x-request-id"),
		Usage:           *usage,
		Model:           originalModel,
		UpstreamModel:   upstreamModel,
		Stream:          true,
		Duration:        time.Since(start),
		ReasoningEffort: reasoningEffort,
	}, nil
}

func writeOpenCodeBufferedResponse(c *gin.Context, resp *http.Response, filter *responseheaders.CompiledHeaderFilter, body []byte) {
	if c == nil || resp == nil {
		return
	}
	contentType := strings.TrimSpace(resp.Header.Get("Content-Type"))
	if contentType == "" {
		contentType = "application/json"
	}
	responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, filter)
	c.Writer.Header().Set("Content-Type", contentType)
	c.Status(resp.StatusCode)
	if len(body) == 0 {
		c.Writer.WriteHeaderNow()
		return
	}
	_, _ = c.Writer.Write(body)
}

func parseOpenCodeChatUsage(body []byte) *ClaudeUsage {
	usage := &ClaudeUsage{}
	if len(body) == 0 {
		return usage
	}
	mergeOpenCodeChatUsage(usage, gjson.ParseBytes(body).Get("usage"))
	return usage
}

func parseOpenCodeResponsesUsage(body []byte) *ClaudeUsage {
	usage := &ClaudeUsage{}
	if len(body) == 0 {
		return usage
	}
	mergeOpenCodeResponsesUsage(usage, gjson.ParseBytes(body).Get("usage"))
	return usage
}

func parseOpenCodeChatStreamingUsage(data string, usage *ClaudeUsage) {
	if usage == nil || strings.TrimSpace(data) == "" {
		return
	}
	mergeOpenCodeChatUsage(usage, gjson.Parse(data).Get("usage"))
}

func parseOpenCodeResponsesStreamingUsage(data string, usage *ClaudeUsage) {
	if usage == nil || strings.TrimSpace(data) == "" {
		return
	}
	node := gjson.Parse(data)
	mergeOpenCodeResponsesUsage(usage, node.Get("usage"))
	mergeOpenCodeResponsesUsage(usage, node.Get("response.usage"))
}

func mergeOpenCodeChatUsage(usage *ClaudeUsage, usageNode gjson.Result) {
	if usage == nil || !usageNode.Exists() {
		return
	}
	prompt := int(usageNode.Get("prompt_tokens").Int())
	cached := int(usageNode.Get("prompt_tokens_details.cached_tokens").Int())
	if cached > 0 && prompt >= cached {
		usage.InputTokens = prompt - cached
		usage.CacheReadInputTokens = cached
	} else if input := usageNode.Get("prompt_tokens"); input.Exists() {
		usage.InputTokens = prompt
	}
	if output := usageNode.Get("completion_tokens"); output.Exists() {
		usage.OutputTokens = int(output.Int())
	}
}

func mergeOpenCodeResponsesUsage(usage *ClaudeUsage, usageNode gjson.Result) {
	if usage == nil || !usageNode.Exists() {
		return
	}
	inputTokens := int(usageNode.Get("input_tokens").Int())
	cached := int(usageNode.Get("input_tokens_details.cached_tokens").Int())
	if cached > 0 && inputTokens >= cached {
		usage.InputTokens = inputTokens - cached
		usage.CacheReadInputTokens = cached
	} else if input := usageNode.Get("input_tokens"); input.Exists() {
		usage.InputTokens = inputTokens
	}
	if output := usageNode.Get("output_tokens"); output.Exists() {
		usage.OutputTokens = int(output.Int())
	}
}
