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

type providerResponsesAnthropicConfig struct {
	ServiceName string
	HTTPClient  *http.Client
	Account     *Account
	Body        []byte

	BuildRequest func(context.Context, *gin.Context, *Account, []byte) (*http.Request, string, string, error)

	ShouldReturnUpstreamError func(int) bool
	ReadErrorBody             func(io.Reader) ([]byte, error)
	ResponseHeaderFilter      *responseheaders.CompiledHeaderFilter
	MaxLineSize               int
}

func forwardProviderResponsesViaAnthropic(ctx context.Context, c *gin.Context, cfg providerResponsesAnthropicConfig) (*ForwardResult, error) {
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

	anthropicReq, err := apicompat.ResponsesToAnthropicRequest(&responsesReq)
	if err != nil {
		return nil, fmt.Errorf("convert responses to anthropic: %w", err)
	}
	anthropicReq.Stream = true

	anthropicBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("marshal anthropic request: %w", err)
	}

	if cfg.BuildRequest == nil {
		return nil, fmt.Errorf("%s build request unavailable", providerResponsesAnthropicServiceName(cfg))
	}
	upstreamReq, _, upstreamModel, err := cfg.BuildRequest(ctx, c, cfg.Account, anthropicBody)
	if err != nil {
		return nil, err
	}
	if upstreamReq == nil {
		return nil, fmt.Errorf("%s build request returned nil request", providerResponsesAnthropicServiceName(cfg))
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
		return nil, fmt.Errorf("%s upstream request failed: %w", providerResponsesAnthropicServiceName(cfg), err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		respBody, readErr := providerResponsesReadErrorBody(cfg, resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		if providerResponsesShouldReturnUpstreamError(cfg, resp.StatusCode) {
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
		return handleProviderResponsesAnthropicStreamingResponse(resp, c, cfg, originalModel, upstreamModel, reasoningEffort, start)
	}
	return handleProviderResponsesAnthropicBufferedStreamingResponse(resp, c, cfg, originalModel, upstreamModel, reasoningEffort, start)
}

func requestPathForResponsesValidation(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	if c.Request.URL != nil && strings.TrimSpace(c.Request.URL.Path) != "" {
		return c.Request.URL.Path
	}
	if c.Request.RequestURI != "" {
		return c.Request.RequestURI
	}
	return c.FullPath()
}

func writeProviderResponsesCompatibilityError(c *gin.Context, err error) {
	if c == nil {
		return
	}
	status := http.StatusInternalServerError
	errorType := "api_error"
	message := "Provider Responses compatibility error"

	var compatErr *ProviderResponsesCompatibilityError
	if errors.As(err, &compatErr) && compatErr != nil {
		if compatErr.StatusCode > 0 {
			status = compatErr.StatusCode
		}
		if strings.TrimSpace(compatErr.Type) != "" {
			errorType = compatErr.Type
		}
		if strings.TrimSpace(compatErr.Message) != "" {
			message = compatErr.Message
		}
	} else if err != nil && strings.TrimSpace(err.Error()) != "" {
		message = err.Error()
	}

	c.JSON(status, gin.H{
		"error": gin.H{
			"type":    errorType,
			"message": message,
		},
	})
}

func providerResponsesAnthropicServiceName(cfg providerResponsesAnthropicConfig) string {
	if name := strings.TrimSpace(cfg.ServiceName); name != "" {
		return name
	}
	return "provider responses"
}

func providerResponsesShouldReturnUpstreamError(cfg providerResponsesAnthropicConfig, status int) bool {
	if cfg.ShouldReturnUpstreamError != nil {
		return cfg.ShouldReturnUpstreamError(status)
	}
	return status >= http.StatusBadRequest
}

func providerResponsesReadErrorBody(cfg providerResponsesAnthropicConfig, body io.Reader) ([]byte, error) {
	if cfg.ReadErrorBody != nil {
		return cfg.ReadErrorBody(body)
	}
	return io.ReadAll(io.LimitReader(body, 2<<20))
}

func providerResponsesMaxLineSize(cfg providerResponsesAnthropicConfig) int {
	if cfg.MaxLineSize > 0 {
		return cfg.MaxLineSize
	}
	return defaultMaxLineSize
}

func providerResponsesScannerBufferSize(maxLineSize int) int {
	const defaultScannerBufferSize = 64 * 1024
	if maxLineSize > 0 && maxLineSize < defaultScannerBufferSize {
		return maxLineSize
	}
	return defaultScannerBufferSize
}

func handleProviderResponsesAnthropicBufferedStreamingResponse(
	resp *http.Response,
	c *gin.Context,
	cfg providerResponsesAnthropicConfig,
	originalModel string,
	upstreamModel string,
	reasoningEffort *string,
	startTime time.Time,
) (*ForwardResult, error) {
	requestID := resp.Header.Get("x-request-id")

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := providerResponsesMaxLineSize(cfg)
	scanner.Buffer(make([]byte, 0, providerResponsesScannerBufferSize(maxLineSize)), maxLineSize)

	var finalResp *apicompat.AnthropicResponse
	var usage ClaudeUsage

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "event: ") {
			continue
		}
		eventType := strings.TrimPrefix(line, "event: ")

		if !scanner.Scan() {
			break
		}
		dataLine := scanner.Text()
		if !strings.HasPrefix(dataLine, "data: ") {
			continue
		}
		payload := dataLine[6:]

		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			logger.L().Warn("provider responses anthropic buffered: failed to parse event",
				zap.Error(err),
				zap.String("request_id", requestID),
				zap.String("event_type", eventType),
				zap.String("service", providerResponsesAnthropicServiceName(cfg)),
			)
			continue
		}

		if event.Type == "message_start" && event.Message != nil {
			finalResp = event.Message
			mergeAnthropicUsage(&usage, event.Message.Usage)
		}
		if event.Type == "message_delta" {
			if event.Usage != nil {
				mergeAnthropicUsage(&usage, *event.Usage)
			}
			if event.Delta != nil && event.Delta.StopReason != "" && finalResp != nil {
				finalResp.StopReason = event.Delta.StopReason
			}
		}
		if event.Type == "content_block_start" && event.ContentBlock != nil && finalResp != nil {
			finalResp.Content = append(finalResp.Content, *event.ContentBlock)
		}
		if event.Type == "content_block_delta" && event.Delta != nil && finalResp != nil && event.Index != nil {
			idx := *event.Index
			if idx >= 0 && idx < len(finalResp.Content) {
				switch event.Delta.Type {
				case "text_delta":
					finalResp.Content[idx].Text += event.Delta.Text
				case "thinking_delta":
					finalResp.Content[idx].Thinking += event.Delta.Thinking
				case "input_json_delta":
					finalResp.Content[idx].Input = appendRawJSON(finalResp.Content[idx].Input, event.Delta.PartialJSON)
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.L().Warn("provider responses anthropic buffered: read error",
				zap.Error(err),
				zap.String("request_id", requestID),
				zap.String("service", providerResponsesAnthropicServiceName(cfg)),
			)
			if !c.Writer.Written() {
				writeResponsesError(c, http.StatusBadGateway, "server_error", "Upstream stream read failed")
			}
		}
		return nil, fmt.Errorf("upstream stream read failed: %w", err)
	}

	if finalResp == nil {
		writeResponsesError(c, http.StatusBadGateway, "server_error", "Upstream stream ended without a response")
		return nil, fmt.Errorf("upstream stream ended without response")
	}

	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		finalResp.Usage = apicompat.AnthropicUsage{
			InputTokens:              usage.InputTokens,
			OutputTokens:             usage.OutputTokens,
			CacheCreationInputTokens: usage.CacheCreationInputTokens,
			CacheReadInputTokens:     usage.CacheReadInputTokens,
		}
	}

	responsesResp := apicompat.AnthropicToResponsesResponse(finalResp)
	responsesResp.Model = originalModel

	if cfg.ResponseHeaderFilter != nil {
		responseheaders.WriteFilteredHeaders(c.Writer.Header(), resp.Header, cfg.ResponseHeaderFilter)
	}
	if respBytes, err := json.Marshal(responsesResp); err == nil {
		respBytes = reverseToolNamesIfPresent(c, respBytes)
		c.Data(http.StatusOK, "application/json; charset=utf-8", respBytes)
	} else {
		c.JSON(http.StatusOK, responsesResp)
	}

	return &ForwardResult{
		RequestID:       requestID,
		Usage:           usage,
		Model:           originalModel,
		UpstreamModel:   upstreamModel,
		ReasoningEffort: reasoningEffort,
		Stream:          false,
		Duration:        time.Since(startTime),
	}, nil
}

func handleProviderResponsesAnthropicStreamingResponse(
	resp *http.Response,
	c *gin.Context,
	cfg providerResponsesAnthropicConfig,
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

	state := apicompat.NewAnthropicEventToResponsesState()
	state.Model = originalModel
	var usage ClaudeUsage
	var firstTokenMs *int
	firstChunk := true

	scanner := bufio.NewScanner(resp.Body)
	maxLineSize := providerResponsesMaxLineSize(cfg)
	scanner.Buffer(make([]byte, 0, providerResponsesScannerBufferSize(maxLineSize)), maxLineSize)

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

	processEvent := func(event *apicompat.AnthropicStreamEvent) bool {
		if firstChunk {
			firstChunk = false
			ms := int(time.Since(startTime).Milliseconds())
			firstTokenMs = &ms
		}

		if event.Type == "message_delta" && event.Usage != nil {
			mergeAnthropicUsage(&usage, *event.Usage)
		}
		if event.Type == "message_start" && event.Message != nil {
			mergeAnthropicUsage(&usage, event.Message.Usage)
		}

		events := apicompat.AnthropicEventToResponsesEvents(event, state)
		for _, evt := range events {
			sse, err := apicompat.ResponsesEventToSSE(evt)
			if err != nil {
				logger.L().Warn("provider responses anthropic stream: failed to marshal event",
					zap.Error(err),
					zap.String("request_id", requestID),
					zap.String("service", providerResponsesAnthropicServiceName(cfg)),
				)
				continue
			}
			out := string(reverseToolNamesIfPresent(c, []byte(sse)))
			if _, err := fmt.Fprint(c.Writer, out); err != nil {
				logger.L().Info("provider responses anthropic stream: client disconnected",
					zap.String("request_id", requestID),
					zap.String("service", providerResponsesAnthropicServiceName(cfg)),
				)
				return true
			}
		}
		if len(events) > 0 {
			c.Writer.Flush()
		}
		return false
	}

	finalizeStream := func() (*ForwardResult, error) {
		if finalEvents := apicompat.FinalizeAnthropicResponsesStream(state); len(finalEvents) > 0 {
			for _, evt := range finalEvents {
				sse, err := apicompat.ResponsesEventToSSE(evt)
				if err != nil {
					continue
				}
				out := string(reverseToolNamesIfPresent(c, []byte(sse)))
				fmt.Fprint(c.Writer, out) //nolint:errcheck
			}
			c.Writer.Flush()
		}
		return resultWithUsage(), nil
	}

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "event: ") {
			continue
		}
		eventType := strings.TrimPrefix(line, "event: ")

		if !scanner.Scan() {
			break
		}
		dataLine := scanner.Text()
		if !strings.HasPrefix(dataLine, "data: ") {
			continue
		}
		payload := dataLine[6:]

		var event apicompat.AnthropicStreamEvent
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			logger.L().Warn("provider responses anthropic stream: failed to parse event",
				zap.Error(err),
				zap.String("request_id", requestID),
				zap.String("event_type", eventType),
				zap.String("service", providerResponsesAnthropicServiceName(cfg)),
			)
			continue
		}

		if processEvent(&event) {
			return resultWithUsage(), nil
		}
	}

	if err := scanner.Err(); err != nil {
		if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			logger.L().Warn("provider responses anthropic stream: read error",
				zap.Error(err),
				zap.String("request_id", requestID),
				zap.String("service", providerResponsesAnthropicServiceName(cfg)),
			)
		}
		return resultWithUsage(), fmt.Errorf("upstream stream read failed: %w", err)
	}

	return finalizeStream()
}
