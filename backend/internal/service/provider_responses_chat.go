package service

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/util/responseheaders"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type providerResponsesChatConfig struct {
	ServiceName string
	HTTPClient  *http.Client
	Account     *Account
	Body        []byte

	BuildRequest func(context.Context, *gin.Context, *Account, []byte) (*http.Request, string, string, error)

	ShouldReturnUpstreamError func(int) bool
	ReadErrorBody             func(io.Reader) ([]byte, error)
	ResponseHeaderFilter      *responseheaders.CompiledHeaderFilter
	ParseUsage                func([]byte) *ClaudeUsage
	ParseStreamingUsage       func(string, *ClaudeUsage)
	MaxLineSize               int
}

func forwardProviderResponsesViaChat(ctx context.Context, c *gin.Context, cfg providerResponsesChatConfig) (*ForwardResult, error) {
	start := time.Now()

	if err := ValidateProviderResponsesCompatibilityRequest(requestPathForResponsesValidation(c), cfg.Body); err != nil {
		writeProviderResponsesCompatibilityError(c, err)
		return nil, err
	}

	var responsesReq apicompat.ResponsesRequest
	if err := json.Unmarshal(cfg.Body, &responsesReq); err != nil {
		return nil, fmt.Errorf("parse responses request: %w", err)
	}
	originalModel := responsesReq.Model
	clientStream := responsesReq.Stream
	reasoningEffort := ExtractResponsesReasoningEffortFromBody(cfg.Body)

	chatReq, err := apicompat.ResponsesToChatCompletionsRequest(&responsesReq)
	if err != nil {
		return nil, fmt.Errorf("convert responses to chat completions: %w", err)
	}
	chatBody, err := json.Marshal(chatReq)
	if err != nil {
		return nil, fmt.Errorf("marshal chat completions request: %w", err)
	}

	if cfg.BuildRequest == nil {
		return nil, fmt.Errorf("%s build request unavailable", providerResponsesChatServiceName(cfg))
	}
	upstreamReq, _, upstreamModel, err := cfg.BuildRequest(ctx, c, cfg.Account, chatBody)
	if err != nil {
		return nil, err
	}
	if upstreamReq == nil {
		return nil, fmt.Errorf("%s build request returned nil request", providerResponsesChatServiceName(cfg))
	}
	if strings.TrimSpace(upstreamModel) == "" {
		upstreamModel = originalModel
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	resp, err := httpClient.Do(upstreamReq)
	if err != nil {
		return nil, fmt.Errorf("%s upstream request failed: %w", providerResponsesChatServiceName(cfg), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		respBody, readErr := providerResponsesChatReadErrorBody(cfg, resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		if providerResponsesChatShouldReturnUpstreamError(cfg, resp.StatusCode) {
			return nil, &UpstreamFailoverError{
				StatusCode:      resp.StatusCode,
				ResponseBody:    respBody,
				ResponseHeaders: resp.Header.Clone(),
			}
		}
		upstreamMsg := strings.TrimSpace(extractUpstreamErrorMessage(respBody))
		upstreamMsg = sanitizeUpstreamErrorMessage(upstreamMsg)
		if upstreamMsg == "" {
			upstreamMsg = http.StatusText(resp.StatusCode)
		}
		writeResponsesError(c, mapUpstreamStatusCode(resp.StatusCode), "server_error", upstreamMsg)
		return nil, fmt.Errorf("upstream error: %d %s", resp.StatusCode, upstreamMsg)
	}

	if clientStream {
		return handleProviderResponsesChatStreamingResponse(resp, c, cfg, originalModel, upstreamModel, reasoningEffort, start)
	}
	return handleProviderResponsesChatBufferedResponse(resp, c, cfg, originalModel, upstreamModel, reasoningEffort, start)
}

func providerResponsesChatServiceName(cfg providerResponsesChatConfig) string {
	if name := strings.TrimSpace(cfg.ServiceName); name != "" {
		return name
	}
	return "provider responses chat"
}

func providerResponsesChatShouldReturnUpstreamError(cfg providerResponsesChatConfig, status int) bool {
	if cfg.ShouldReturnUpstreamError != nil {
		return cfg.ShouldReturnUpstreamError(status)
	}
	return status >= http.StatusBadRequest
}

func providerResponsesChatReadErrorBody(cfg providerResponsesChatConfig, body io.Reader) ([]byte, error) {
	if cfg.ReadErrorBody != nil {
		return cfg.ReadErrorBody(body)
	}
	return io.ReadAll(io.LimitReader(body, 2<<20))
}

func providerResponsesChatMaxLineSize(cfg providerResponsesChatConfig) int {
	if cfg.MaxLineSize > 0 {
		return cfg.MaxLineSize
	}
	return defaultMaxLineSize
}

func handleProviderResponsesChatBufferedResponse(
	resp *http.Response,
	c *gin.Context,
	cfg providerResponsesChatConfig,
	originalModel string,
	upstreamModel string,
	reasoningEffort *string,
	startTime time.Time,
) (*ForwardResult, error) {
	body, err := readGLMNonStreamResponseBody(resp.Body)
	if err != nil {
		return nil, err
	}

	var chatResp apicompat.ChatCompletionsResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return nil, fmt.Errorf("parse chat completions response: %w", err)
	}
	usage := providerResponsesChatUsage(cfg, body, chatResp.Usage)
	responsesResp := apicompat.ChatCompletionsToResponsesResponse(&chatResp, originalModel)

	if cfg.ResponseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, cfg.ResponseHeaderFilter)
	}
	respBytes, err := json.Marshal(responsesResp)
	if err != nil {
		return nil, fmt.Errorf("marshal responses response: %w", err)
	}
	respBytes = reverseToolNamesIfPresent(c, respBytes)
	c.Data(http.StatusOK, "application/json; charset=utf-8", respBytes)

	return &ForwardResult{
		RequestID:       resp.Header.Get("x-request-id"),
		Usage:           usage,
		Model:           originalModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: reasoningEffort,
		Stream:          false,
		Duration:        time.Since(startTime),
	}, nil
}

func providerResponsesChatUsage(cfg providerResponsesChatConfig, body []byte, chatUsage *apicompat.ChatUsage) ClaudeUsage {
	if cfg.ParseUsage != nil {
		if usage := cfg.ParseUsage(body); usage != nil {
			return *usage
		}
	}
	return providerResponsesClaudeUsageFromChat(chatUsage)
}

func providerResponsesClaudeUsageFromChat(chatUsage *apicompat.ChatUsage) ClaudeUsage {
	if chatUsage == nil {
		return ClaudeUsage{}
	}
	return ClaudeUsage{
		InputTokens:          chatUsage.PromptTokens,
		OutputTokens:         chatUsage.CompletionTokens,
		CacheReadInputTokens: providerResponsesCachedTokens(chatUsage),
	}
}

func providerResponsesCachedTokens(chatUsage *apicompat.ChatUsage) int {
	if chatUsage == nil || chatUsage.PromptTokensDetails == nil {
		return 0
	}
	return chatUsage.PromptTokensDetails.CachedTokens
}

func handleProviderResponsesChatStreamingResponse(
	resp *http.Response,
	c *gin.Context,
	cfg providerResponsesChatConfig,
	originalModel string,
	upstreamModel string,
	reasoningEffort *string,
	startTime time.Time,
) (*ForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")

	if cfg.ResponseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, cfg.ResponseHeaderFilter)
	}
	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.WriteHeader(http.StatusOK)

	state := apicompat.NewChatCompletionsToResponsesState(originalModel)
	var usage ClaudeUsage
	var firstTokenMs *int
	firstChunk := true

	resultWithUsage := func() *ForwardResult {
		return &ForwardResult{
			RequestID:       requestID,
			Usage:           usage,
			Model:           originalModel,
			UpstreamModel:   upstreamModel,
			ReasoningEffort: reasoningEffort,
			Stream:          true,
			Duration:        time.Since(startTime),
			FirstTokenMs:    firstTokenMs,
		}
	}

	processData := func(data string) (bool, error) {
		if data == "" || data == "[DONE]" {
			return false, nil
		}
		if cfg.ParseStreamingUsage != nil {
			cfg.ParseStreamingUsage(data, &usage)
		}
		var chunk apicompat.ChatCompletionsChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			logger.L().Warn("provider responses chat stream: failed to parse chunk",
				zap.Error(err),
				zap.String("request_id", requestID),
				zap.String("service", providerResponsesChatServiceName(cfg)),
			)
			return false, nil
		}
		if cfg.ParseStreamingUsage == nil && chunk.Usage != nil {
			usage = providerResponsesClaudeUsageFromChat(chunk.Usage)
		}
		events := apicompat.ChatChunkToResponsesEvents(&chunk, state)
		if len(events) > 0 && firstChunk {
			firstChunk = false
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}
		return writeProviderResponsesChatEvents(c, events)
	}

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := providerResponsesChatMaxLineSize(cfg)
	scanner.Buffer(make([]byte, 0, providerResponsesScannerBufferSize(maxLineSize)), maxLineSize)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		disconnected, err := processData(data)
		if err != nil {
			return resultWithUsage(), err
		}
		if disconnected {
			return resultWithUsage(), nil
		}
	}

	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.L().Warn("provider responses chat stream: read error",
				zap.Error(err),
				zap.String("request_id", requestID),
				zap.String("service", providerResponsesChatServiceName(cfg)),
			)
		}
		return resultWithUsage(), fmt.Errorf("upstream stream read failed: %w", err)
	}

	finalEvents := apicompat.FinalizeChatCompletionsResponsesStream(state)
	if _, err := writeProviderResponsesChatEvents(c, finalEvents); err != nil {
		return resultWithUsage(), err
	}
	return resultWithUsage(), nil
}

func writeProviderResponsesChatEvents(c *gin.Context, events []apicompat.ResponsesStreamEvent) (bool, error) {
	for _, evt := range events {
		sse, err := apicompat.ResponsesEventToSSE(evt)
		if err != nil {
			return false, err
		}
		out := string(reverseToolNamesIfPresent(c, []byte(sse)))
		if _, err := fmt.Fprint(c.Writer, out); err != nil {
			return true, nil
		}
	}
	if len(events) > 0 {
		c.Writer.Flush()
	}
	return false, nil
}
