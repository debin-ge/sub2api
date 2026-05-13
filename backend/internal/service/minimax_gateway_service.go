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
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const (
	miniMaxInternationalHost         = "api.minimax.io"
	miniMaxChinaHost                 = "api.minimaxi.com"
	miniMaxNonStreamResponseMaxBytes = 2 << 20
)

type MiniMaxGatewayService struct {
	httpClient           *http.Client
	quotaService         *MiniMaxQuotaService
	responseHeaderFilter *responseheaders.CompiledHeaderFilter
}

type MiniMaxUnsupportedContentError struct {
	Message string
}

func (e *MiniMaxUnsupportedContentError) Error() string {
	return e.Message
}

type MiniMaxUpstreamStatusMapping struct {
	ClientStatus int
	ErrorType    string
	Retryable    bool
}

func MapMiniMaxUpstreamStatus(status int) MiniMaxUpstreamStatusMapping {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return MiniMaxUpstreamStatusMapping{
			ClientStatus: http.StatusBadGateway,
			ErrorType:    "upstream_auth_error",
			Retryable:    false,
		}
	case http.StatusTooManyRequests:
		return MiniMaxUpstreamStatusMapping{
			ClientStatus: http.StatusTooManyRequests,
			ErrorType:    "rate_limit_error",
			Retryable:    true,
		}
	case 529:
		return MiniMaxUpstreamStatusMapping{
			ClientStatus: http.StatusBadGateway,
			ErrorType:    "overloaded_error",
			Retryable:    true,
		}
	default:
		if status >= http.StatusInternalServerError {
			return MiniMaxUpstreamStatusMapping{
				ClientStatus: http.StatusBadGateway,
				ErrorType:    "server_error",
				Retryable:    true,
			}
		}
		return MiniMaxUpstreamStatusMapping{
			ClientStatus: status,
			ErrorType:    "invalid_request_error",
			Retryable:    false,
		}
	}
}

func NewMiniMaxGatewayService(httpClient *http.Client, quotaService *MiniMaxQuotaService, responseHeaderFilter *responseheaders.CompiledHeaderFilter) *MiniMaxGatewayService {
	return NewMiniMaxGatewayServiceWithTimeout(httpClient, quotaService, responseHeaderFilter, compatibleGatewayDefaultUpstreamTimeout)
}

func NewMiniMaxGatewayServiceWithTimeout(httpClient *http.Client, quotaService *MiniMaxQuotaService, responseHeaderFilter *responseheaders.CompiledHeaderFilter, upstreamTimeout time.Duration) *MiniMaxGatewayService {
	if httpClient == nil {
		httpClient = newDefaultCompatibleGatewayHTTPClient(upstreamTimeout)
	}
	return &MiniMaxGatewayService{
		httpClient:           httpClient,
		quotaService:         quotaService,
		responseHeaderFilter: responseHeaderFilter,
	}
}

func (s *MiniMaxGatewayService) ForwardMessages(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("minimax gateway service unavailable")
	}
	start := time.Now()

	if err := rejectMiniMaxUnsupportedContent(body); err != nil {
		return nil, err
	}

	upstreamReq, originalModel, upstreamModel, err := s.buildMessagesRequest(ctx, c, account, body)
	if err != nil {
		return nil, err
	}

	requestID = strings.TrimSpace(requestID)
	if s.quotaService == nil {
		return nil, fmt.Errorf("minimax quota service unavailable")
	}
	decision, err := s.quotaService.ReserveTextRequest(ctx, account, requestID)
	if err != nil {
		return nil, err
	}
	if decision == nil || !decision.Allowed {
		return nil, fmt.Errorf("minimax quota exhausted")
	}

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		_ = s.quotaService.RollbackTextRequest(ctx, account.ID, requestID)
		return nil, fmt.Errorf("minimax upstream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if shouldReturnMiniMaxUpstreamError(resp.StatusCode) {
		body, readErr := readMiniMaxNonStreamResponseBody(resp.Body)
		_ = s.quotaService.RollbackTextRequest(ctx, account.ID, requestID)
		if readErr != nil {
			return nil, readErr
		}
		return nil, &UpstreamFailoverError{
			StatusCode:      resp.StatusCode,
			ResponseBody:    body,
			ResponseHeaders: resp.Header.Clone(),
		}
	}

	stream := gjson.GetBytes(body, "stream").Bool()
	var result *ForwardResult
	if stream {
		result, err = s.handleStreamingMessagesResponse(resp, c, originalModel, upstreamModel, start)
	} else {
		result, err = s.handleNonStreamingMessagesResponse(resp, c, originalModel, upstreamModel, start)
	}
	if err != nil {
		_ = s.quotaService.RollbackTextRequest(ctx, account.ID, requestID)
		return nil, err
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		_ = s.quotaService.RollbackTextRequest(ctx, account.ID, requestID)
	}
	return result, nil
}

func (s *MiniMaxGatewayService) ForwardChatCompletions(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("minimax gateway service unavailable")
	}
	start := time.Now()

	upstreamReq, originalModel, upstreamModel, err := s.buildChatCompletionsRequest(ctx, c, account, body)
	if err != nil {
		return nil, err
	}

	requestID = strings.TrimSpace(requestID)
	if s.quotaService == nil {
		return nil, fmt.Errorf("minimax quota service unavailable")
	}
	decision, err := s.quotaService.ReserveTextRequest(ctx, account, requestID)
	if err != nil {
		return nil, err
	}
	if decision == nil || !decision.Allowed {
		return nil, fmt.Errorf("minimax quota exhausted")
	}

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		_ = s.quotaService.RollbackTextRequest(ctx, account.ID, requestID)
		return nil, fmt.Errorf("minimax upstream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if shouldReturnMiniMaxUpstreamError(resp.StatusCode) {
		body, readErr := readMiniMaxNonStreamResponseBody(resp.Body)
		_ = s.quotaService.RollbackTextRequest(ctx, account.ID, requestID)
		if readErr != nil {
			return nil, readErr
		}
		return nil, &UpstreamFailoverError{
			StatusCode:      resp.StatusCode,
			ResponseBody:    body,
			ResponseHeaders: resp.Header.Clone(),
		}
	}

	stream := gjson.GetBytes(body, "stream").Bool()
	var result *ForwardResult
	if stream {
		result, err = s.handleStreamingChatCompletionsResponse(resp, c, originalModel, upstreamModel, start)
	} else {
		result, err = s.handleNonStreamingChatCompletionsResponse(resp, c, originalModel, upstreamModel, start)
	}
	if err != nil {
		_ = s.quotaService.RollbackTextRequest(ctx, account.ID, requestID)
		return nil, err
	}
	if resp.StatusCode >= http.StatusInternalServerError {
		_ = s.quotaService.RollbackTextRequest(ctx, account.ID, requestID)
	}
	return result, nil
}

func shouldReturnMiniMaxUpstreamError(status int) bool {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return true
	}
	return MapMiniMaxUpstreamStatus(status).Retryable
}

func (s *MiniMaxGatewayService) buildMessagesRequest(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
	if account == nil || !account.IsMiniMaxTokenPlan() {
		return nil, "", "", fmt.Errorf("invalid minimax account")
	}
	apiKey := account.GetMiniMaxAPIKey()
	if apiKey == "" {
		return nil, "", "", fmt.Errorf("minimax api key is required")
	}

	upstreamBody, originalModel, upstreamModel, err := rewriteMiniMaxModel(body, account)
	if err != nil {
		return nil, "", "", err
	}

	upstreamURL, err := buildMiniMaxMessagesURL(account)
	if err != nil {
		return nil, "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, "", "", err
	}
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
	return req, originalModel, upstreamModel, nil
}

func (s *MiniMaxGatewayService) buildChatCompletionsRequest(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
	if account == nil || !account.IsMiniMaxTokenPlan() {
		return nil, "", "", fmt.Errorf("invalid minimax account")
	}
	apiKey := account.GetMiniMaxAPIKey()
	if apiKey == "" {
		return nil, "", "", fmt.Errorf("minimax api key is required")
	}

	upstreamBody, originalModel, upstreamModel, err := rewriteMiniMaxModel(body, account)
	if err != nil {
		return nil, "", "", err
	}

	upstreamURL, err := buildMiniMaxChatCompletionsURL(account)
	if err != nil {
		return nil, "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, "", "", err
	}
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
	return req, originalModel, upstreamModel, nil
}

func rewriteMiniMaxModel(body []byte, account *Account) ([]byte, string, string, error) {
	payload, err := decodeMiniMaxMessagesPayload(body)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse minimax messages request: %w", err)
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, "", "", fmt.Errorf("minimax messages model is required")
	}
	if account == nil || !account.IsMiniMaxModelSupported(model) {
		return nil, "", "", &MiniMaxUnsupportedContentError{Message: fmt.Sprintf("minimax model %s is not supported by this account", model)}
	}
	upstreamModel := account.GetMiniMaxMappedModel(model)
	payload["model"] = upstreamModel

	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, "", "", fmt.Errorf("rewrite minimax messages model: %w", err)
	}
	return rewritten, model, upstreamModel, nil
}

func rejectMiniMaxUnsupportedContent(body []byte) error {
	payload, err := decodeMiniMaxMessagesPayload(body)
	if err != nil {
		return fmt.Errorf("parse minimax messages request: %w", err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		return &MiniMaxUnsupportedContentError{Message: "minimax token plan gateway requires messages array"}
	}
	for _, message := range messages {
		msg, _ := message.(map[string]any)
		if !miniMaxContentIsTextOnly(msg["content"]) {
			return &MiniMaxUnsupportedContentError{Message: "minimax token plan gateway supports text messages only"}
		}
	}
	return nil
}

func decodeMiniMaxMessagesPayload(body []byte) (map[string]any, error) {
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

func miniMaxContentIsTextOnly(value any) bool {
	switch v := value.(type) {
	case string:
		return true
	case []any:
		if len(v) == 0 {
			return true
		}
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

func buildMiniMaxMessagesURL(account *Account) (string, error) {
	baseURL, err := validateMiniMaxUpstreamBaseURL(account.GetMiniMaxAnthropicBaseURL())
	if err != nil {
		return "", err
	}
	return strings.TrimRight(baseURL, "/") + "/v1/messages", nil
}

func buildMiniMaxChatCompletionsURL(account *Account) (string, error) {
	baseURL, err := validateMiniMaxUpstreamBaseURL(account.GetMiniMaxOpenAIBaseURL())
	if err != nil {
		return "", err
	}
	return strings.TrimRight(baseURL, "/") + "/chat/completions", nil
}

func validateMiniMaxUpstreamBaseURL(raw string) (string, error) {
	return urlvalidator.ValidateHTTPURL(raw, false, urlvalidator.ValidationOptions{
		AllowedHosts: []string{miniMaxInternationalHost, miniMaxChinaHost},
		AllowPrivate: false,
	})
}

func parseMiniMaxClaudeUsage(body []byte) *ClaudeUsage {
	return parseClaudeUsageFromResponseBody(body)
}

func parseMiniMaxOpenAIUsage(body []byte) *ClaudeUsage {
	usage := &ClaudeUsage{}
	if len(body) == 0 {
		return usage
	}
	mergeMiniMaxOpenAIUsage(usage, gjson.ParseBytes(body).Get("usage"))
	return usage
}

func parseMiniMaxOpenAIStreamingUsage(data string, usage *ClaudeUsage) {
	if usage == nil || strings.TrimSpace(data) == "" {
		return
	}
	mergeMiniMaxOpenAIUsage(usage, gjson.Parse(data).Get("usage"))
}

func mergeMiniMaxOpenAIUsage(usage *ClaudeUsage, usageNode gjson.Result) {
	if usage == nil || !usageNode.Exists() {
		return
	}
	if input := usageNode.Get("prompt_tokens"); input.Exists() {
		usage.InputTokens = int(input.Int())
	} else if input := usageNode.Get("input_tokens"); input.Exists() {
		usage.InputTokens = int(input.Int())
	}
	if output := usageNode.Get("completion_tokens"); output.Exists() {
		usage.OutputTokens = int(output.Int())
	} else if output := usageNode.Get("output_tokens"); output.Exists() {
		usage.OutputTokens = int(output.Int())
	}
	if cached := usageNode.Get("prompt_tokens_details.cached_tokens"); cached.Exists() {
		usage.CacheReadInputTokens = int(cached.Int())
	} else if cached := usageNode.Get("cached_tokens"); cached.Exists() {
		usage.CacheReadInputTokens = int(cached.Int())
	}
}

func (s *MiniMaxGatewayService) handleNonStreamingMessagesResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time) (*ForwardResult, error) {
	body, err := readMiniMaxNonStreamResponseBody(resp.Body)
	if err != nil {
		return nil, err
	}
	usage := parseMiniMaxClaudeUsage(body)
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

func (s *MiniMaxGatewayService) handleNonStreamingChatCompletionsResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time) (*ForwardResult, error) {
	body, err := readMiniMaxNonStreamResponseBody(resp.Body)
	if err != nil {
		return nil, err
	}
	usage := parseMiniMaxOpenAIUsage(body)
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

func readMiniMaxNonStreamResponseBody(body io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, miniMaxNonStreamResponseMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > miniMaxNonStreamResponseMaxBytes {
		return nil, fmt.Errorf("minimax upstream response too large")
	}
	return data, nil
}

func (s *MiniMaxGatewayService) handleStreamingMessagesResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time) (*ForwardResult, error) {
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
				return nil, fmt.Errorf("minimax upstream stream line too large")
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

func (s *MiniMaxGatewayService) handleStreamingChatCompletionsResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time) (*ForwardResult, error) {
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
				return nil, fmt.Errorf("minimax upstream stream line too large")
			}
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if data != "" && data != "[DONE]" {
					parseMiniMaxOpenAIStreamingUsage(data, usage)
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
