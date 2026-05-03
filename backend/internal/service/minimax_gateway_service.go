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

func NewMiniMaxGatewayService(httpClient *http.Client, quotaService *MiniMaxQuotaService, responseHeaderFilter *responseheaders.CompiledHeaderFilter) *MiniMaxGatewayService {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
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

	stream := gjson.GetBytes(body, "stream").Bool()
	if stream {
		return s.handleStreamingMessagesResponse(resp, c, originalModel, upstreamModel, start)
	}
	return s.handleNonStreamingMessagesResponse(resp, c, originalModel, upstreamModel, start)
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, buildMiniMaxMessagesURL(account), bytes.NewReader(upstreamBody))
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
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, "", "", fmt.Errorf("parse minimax messages request: %w", err)
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return nil, "", "", fmt.Errorf("minimax messages model is required")
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
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return fmt.Errorf("parse minimax messages request: %w", err)
	}
	messages, _ := payload["messages"].([]any)
	for _, message := range messages {
		msg, _ := message.(map[string]any)
		if containsMiniMaxUnsupportedContent(msg["content"]) {
			return &MiniMaxUnsupportedContentError{Message: "minimax token plan gateway supports text messages only"}
		}
	}
	return nil
}

func containsMiniMaxUnsupportedContent(value any) bool {
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if containsMiniMaxUnsupportedContent(item) {
				return true
			}
		}
	case map[string]any:
		if typ, _ := v["type"].(string); isMiniMaxUnsupportedContentType(typ) {
			return true
		}
		for _, item := range v {
			if containsMiniMaxUnsupportedContent(item) {
				return true
			}
		}
	}
	return false
}

func isMiniMaxUnsupportedContentType(typ string) bool {
	switch strings.ToLower(strings.TrimSpace(typ)) {
	case "image", "document", "audio", "video":
		return true
	default:
		return false
	}
}

func buildMiniMaxMessagesURL(account *Account) string {
	return strings.TrimRight(account.GetMiniMaxAnthropicBaseURL(), "/") + "/v1/messages"
}

func parseMiniMaxClaudeUsage(body []byte) *ClaudeUsage {
	return parseClaudeUsageFromResponseBody(body)
}

func (s *MiniMaxGatewayService) handleNonStreamingMessagesResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time) (*ForwardResult, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
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
		c.Data(resp.StatusCode, contentType, body)
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

func (s *MiniMaxGatewayService) handleStreamingMessagesResponse(resp *http.Response, c *gin.Context, originalModel string, upstreamModel string, start time.Time) (*ForwardResult, error) {
	usage := &ClaudeUsage{}
	if c != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, s.responseHeaderFilter)
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Status(resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), defaultMaxLineSize)
	gatewayUsageParser := &GatewayService{}
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(strings.TrimSpace(line), "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "data:"))
			if data != "" && data != "[DONE]" {
				gatewayUsageParser.parseSSEUsage(data, usage)
			}
		}
		if c != nil {
			if _, err := io.WriteString(c.Writer, line+"\n"); err != nil {
				return nil, err
			}
			if line == "" {
				if flusher, ok := c.Writer.(http.Flusher); ok {
					flusher.Flush()
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
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
