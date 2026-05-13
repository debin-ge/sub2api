package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

const kimiDefaultAnthropicMaxTokens = int64(4096)

type KimiGatewayService struct {
	httpClient           *http.Client
	responseHeaderFilter *responseheaders.CompiledHeaderFilter
}

type KimiUnsupportedContentError struct {
	Message string
}

func (e *KimiUnsupportedContentError) Error() string {
	return e.Message
}

type KimiUpstreamStatusMapping struct {
	ClientStatus int
	ErrorType    string
	Retryable    bool
}

func MapKimiUpstreamStatus(status int) KimiUpstreamStatusMapping {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return KimiUpstreamStatusMapping{
			ClientStatus: http.StatusBadGateway,
			ErrorType:    "upstream_auth_error",
			Retryable:    false,
		}
	case http.StatusTooManyRequests:
		return KimiUpstreamStatusMapping{
			ClientStatus: http.StatusTooManyRequests,
			ErrorType:    "rate_limit_error",
			Retryable:    true,
		}
	case 529:
		return KimiUpstreamStatusMapping{
			ClientStatus: http.StatusBadGateway,
			ErrorType:    "overloaded_error",
			Retryable:    true,
		}
	default:
		if status >= http.StatusInternalServerError {
			return KimiUpstreamStatusMapping{
				ClientStatus: http.StatusBadGateway,
				ErrorType:    "server_error",
				Retryable:    true,
			}
		}
		return KimiUpstreamStatusMapping{
			ClientStatus: status,
			ErrorType:    "invalid_request_error",
			Retryable:    false,
		}
	}
}

func NewKimiGatewayService(httpClient *http.Client, responseHeaderFilter *responseheaders.CompiledHeaderFilter) *KimiGatewayService {
	return NewKimiGatewayServiceWithTimeout(httpClient, responseHeaderFilter, compatibleGatewayDefaultUpstreamTimeout)
}

func NewKimiGatewayServiceWithTimeout(httpClient *http.Client, responseHeaderFilter *responseheaders.CompiledHeaderFilter, upstreamTimeout time.Duration) *KimiGatewayService {
	if httpClient == nil {
		httpClient = newDefaultCompatibleGatewayHTTPClient(upstreamTimeout)
	}
	return &KimiGatewayService{
		httpClient:           httpClient,
		responseHeaderFilter: responseHeaderFilter,
	}
}

func (s *KimiGatewayService) ForwardMessages(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("kimi gateway service unavailable")
	}
	start := time.Now()

	if _, err := validateKimiAccount(account); err != nil {
		return nil, err
	}
	if err := rejectKimiAnthropicUnsupportedContent(body); err != nil {
		return nil, err
	}

	upstreamReq, originalModel, upstreamModel, err := s.buildMessagesRequest(ctx, c, account, body)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("kimi upstream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if shouldReturnKimiUpstreamError(resp.StatusCode) {
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

func (s *KimiGatewayService) ForwardChatCompletions(ctx context.Context, c *gin.Context, account *Account, body []byte, requestID string) (*ForwardResult, error) {
	if s == nil {
		return nil, fmt.Errorf("kimi gateway service unavailable")
	}
	start := time.Now()

	if _, err := validateKimiAccount(account); err != nil {
		return nil, err
	}
	if err := rejectKimiOpenAIUnsupportedContent(body); err != nil {
		return nil, err
	}

	upstreamReq, originalModel, upstreamModel, err := s.buildChatCompletionsRequest(ctx, c, account, body)
	if err != nil {
		return nil, err
	}

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("kimi upstream request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if shouldReturnKimiUpstreamError(resp.StatusCode) {
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
		return compat.handleStreamingChatCompletionsResponse(resp, c, originalModel, upstreamModel, start)
	}
	return compat.handleNonStreamingChatCompletionsResponse(resp, c, originalModel, upstreamModel, start)
}

func (s *KimiGatewayService) glmResponseCompat() *GLMGatewayService {
	return &GLMGatewayService{responseHeaderFilter: s.responseHeaderFilter}
}

func shouldReturnKimiUpstreamError(status int) bool {
	return status >= http.StatusBadRequest
}

func validateKimiAccount(account *Account) (string, error) {
	if account == nil || !account.IsKimiCode() {
		return "", fmt.Errorf("invalid kimi account")
	}
	apiKey := account.GetKimiAPIKey()
	if apiKey == "" {
		return "", fmt.Errorf("kimi api key is required")
	}
	return apiKey, nil
}

func (s *KimiGatewayService) buildMessagesRequest(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
	apiKey, err := validateKimiAccount(account)
	if err != nil {
		return nil, "", "", err
	}
	upstreamBody, originalModel, upstreamModel, err := rewriteKimiAnthropicMessagesBody(body, account)
	if err != nil {
		return nil, "", "", err
	}
	upstreamURL := strings.TrimRight(account.GetKimiAnthropicBaseURL(), "/") + "/v1/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, "", "", err
	}
	setKimiUpstreamHeaders(req, c, apiKey)
	setKimiAnthropicHeaders(req, c)
	return req, originalModel, upstreamModel, nil
}

func (s *KimiGatewayService) buildChatCompletionsRequest(ctx context.Context, c *gin.Context, account *Account, body []byte) (*http.Request, string, string, error) {
	apiKey, err := validateKimiAccount(account)
	if err != nil {
		return nil, "", "", err
	}
	upstreamBody, originalModel, upstreamModel, err := rewriteKimiModel(body, account)
	if err != nil {
		return nil, "", "", err
	}
	upstreamURL := strings.TrimRight(account.GetKimiOpenAIBaseURL(), "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, upstreamURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, "", "", err
	}
	setKimiUpstreamHeaders(req, c, apiKey)
	return req, originalModel, upstreamModel, nil
}

func setKimiUpstreamHeaders(req *http.Request, c *gin.Context, apiKey string) {
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

func rewriteKimiModel(body []byte, account *Account) ([]byte, string, string, error) {
	payload, model, upstreamModel, err := validateKimiModelPayload(body, account)
	if err != nil {
		return nil, "", "", err
	}
	payload["model"] = upstreamModel
	upstreamBody, err := json.Marshal(payload)
	if err != nil {
		return nil, "", "", fmt.Errorf("rewrite kimi request model: %w", err)
	}
	return upstreamBody, model, upstreamModel, nil
}

func rewriteKimiAnthropicMessagesBody(body []byte, account *Account) ([]byte, string, string, error) {
	payload, model, upstreamModel, err := validateKimiModelPayload(body, account)
	if err != nil {
		return nil, "", "", err
	}
	payload["model"] = upstreamModel
	if value, ok := payload["max_tokens"]; !ok || value == nil {
		payload["max_tokens"] = kimiDefaultAnthropicMaxTokens
	}
	upstreamBody, err := json.Marshal(payload)
	if err != nil {
		return nil, "", "", fmt.Errorf("rewrite kimi messages request: %w", err)
	}
	return upstreamBody, model, upstreamModel, nil
}

func validateKimiModelPayload(body []byte, account *Account) (map[string]any, string, string, error) {
	payload, err := decodeGLMPayload(body)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse kimi request: %w", err)
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, "", "", fmt.Errorf("kimi request model is required")
	}
	if account == nil || !account.IsKimiModelSupported(model) {
		return nil, "", "", &KimiUnsupportedContentError{Message: "Kimi gateway only supports model kimi-for-coding"}
	}
	return payload, model, account.GetKimiMappedModel(model), nil
}

func rejectKimiAnthropicUnsupportedContent(body []byte) error {
	payload, err := decodeGLMPayload(body)
	if err != nil {
		return fmt.Errorf("parse kimi messages request: %w", err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		return &KimiUnsupportedContentError{Message: "kimi gateway requires messages array"}
	}
	if system, ok := payload["system"]; ok && !glmAnthropicContentIsSupported(system) {
		return &KimiUnsupportedContentError{Message: "kimi gateway does not support multimodal content"}
	}
	for _, message := range messages {
		msg, ok := message.(map[string]any)
		if !ok {
			return &KimiUnsupportedContentError{Message: "kimi gateway requires message objects"}
		}
		if !glmAnthropicContentIsSupported(msg["content"]) {
			return &KimiUnsupportedContentError{Message: "kimi gateway does not support multimodal content"}
		}
	}
	return nil
}

func rejectKimiOpenAIUnsupportedContent(body []byte) error {
	payload, err := decodeGLMPayload(body)
	if err != nil {
		return fmt.Errorf("parse kimi chat completions request: %w", err)
	}
	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) == 0 {
		return &KimiUnsupportedContentError{Message: "kimi gateway requires messages array"}
	}
	for _, message := range messages {
		msg, ok := message.(map[string]any)
		if !ok {
			return &KimiUnsupportedContentError{Message: "kimi gateway requires message objects"}
		}
		if !glmOpenAIContentIsSupported(msg["content"]) {
			return &KimiUnsupportedContentError{Message: "kimi gateway does not support multimodal content"}
		}
	}
	return nil
}

func setKimiAnthropicHeaders(req *http.Request, c *gin.Context) {
	if req == nil {
		return
	}
	if c != nil {
		if version := strings.TrimSpace(c.GetHeader("anthropic-version")); version != "" {
			req.Header.Set("anthropic-version", version)
			return
		}
	}
	req.Header.Set("anthropic-version", "2023-06-01")
}
