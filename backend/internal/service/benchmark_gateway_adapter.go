package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

type benchmarkOpenAIGatewayAdapter struct {
	gateway      benchmarkOpenAIGateway
	channelGroup benchmarkChannelGroupResolver
}

type benchmarkOpenAIGateway interface {
	SelectAccountForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error)
	Forward(ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error)
}

type benchmarkChannelGroupResolver interface {
	GetGroupIDs(ctx context.Context, channelID int64) ([]int64, error)
	GetGroupPlatforms(ctx context.Context, groupIDs []int64) (map[int64]string, error)
}

func ProvideBenchmarkGatewayClient(gateway *OpenAIGatewayService, channelRepo ChannelRepository) BenchmarkGatewayClient {
	adapter := &benchmarkOpenAIGatewayAdapter{
		gateway:      gateway,
		channelGroup: channelRepo,
	}
	return newBenchmarkGatewayClient(adapter.execute)
}

func (a *benchmarkOpenAIGatewayAdapter) execute(ctx context.Context, req benchmarkGatewayInternalRequest) (*BenchmarkGatewayResponse, error) {
	if a == nil || a.gateway == nil {
		return nil, errors.New("openai gateway service is required")
	}
	if a.channelGroup == nil {
		return nil, errors.New("channel group resolver is required")
	}
	if req.ForcedChannelID <= 0 {
		return nil, errors.New("benchmark forced channel id is required")
	}

	model := benchmarkGatewayPayloadModel(req.UserPayload)
	if model == "" {
		return nil, errors.New("benchmark gateway payload model is required")
	}

	// ForcedChannelID is an explicit scheduling constraint: resolve that channel
	// to its OpenAI group and pass only that group into account selection.
	groupID, err := a.selectOpenAIGroupForChannel(ctx, req.ForcedChannelID)
	if err != nil {
		return nil, err
	}
	account, err := a.gateway.SelectAccountForModelWithExclusions(ctx, &groupID, "", model, nil)
	if err != nil {
		return nil, err
	}
	if account == nil {
		return nil, fmt.Errorf("no openai account selected for benchmark channel %d model %s", req.ForcedChannelID, model)
	}

	body, err := benchmarkGatewayPayloadBody(req.UserPayload)
	if err != nil {
		return nil, err
	}
	ginCtx, recorder := benchmarkGatewayGinContext(ctx, body, req.RequestID)
	result, err := a.gateway.Forward(ctx, ginCtx, account, body)
	if err != nil {
		return benchmarkGatewayResponseFromRecorder(req.RequestID, recorder, result), err
	}
	if recorder.Code >= http.StatusBadRequest {
		return benchmarkGatewayResponseFromRecorder(req.RequestID, recorder, result), fmt.Errorf("benchmark gateway upstream returned status %d", recorder.Code)
	}
	return benchmarkGatewayResponseFromRecorder(req.RequestID, recorder, result), nil
}

func (a *benchmarkOpenAIGatewayAdapter) selectOpenAIGroupForChannel(ctx context.Context, channelID int64) (int64, error) {
	groupIDs, err := a.channelGroup.GetGroupIDs(ctx, channelID)
	if err != nil {
		return 0, fmt.Errorf("get benchmark channel groups: %w", err)
	}
	if len(groupIDs) == 0 {
		return 0, fmt.Errorf("benchmark channel %d has no groups", channelID)
	}
	platforms, err := a.channelGroup.GetGroupPlatforms(ctx, groupIDs)
	if err != nil {
		return 0, fmt.Errorf("get benchmark channel group platforms: %w", err)
	}
	for _, groupID := range groupIDs {
		if strings.EqualFold(strings.TrimSpace(platforms[groupID]), PlatformOpenAI) {
			return groupID, nil
		}
	}
	return 0, fmt.Errorf("benchmark channel %d has no openai-compatible group", channelID)
}

func benchmarkGatewayPayloadModel(payload map[string]any) string {
	model, _ := payload["model"].(string)
	return strings.TrimSpace(model)
}

func benchmarkGatewayPayloadBody(payload map[string]any) ([]byte, error) {
	body := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		if isBenchmarkGatewayInternalPayloadKey(key) {
			continue
		}
		body[key] = cloneBenchmarkGatewayPayloadValue(value)
	}
	body["stream"] = false
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal benchmark gateway payload: %w", err)
	}
	return encoded, nil
}

func benchmarkGatewayGinContext(ctx context.Context, body []byte, requestID string) (*gin.Context, *httptest.ResponseRecorder) {
	recorder := httptest.NewRecorder()
	ginCtx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(body)).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", codexCLIUserAgent)
	req.Header.Set("originator", "codex_cli_rs")
	req.Header.Set("X-Request-ID", requestID)
	ginCtx.Request = req
	SetOpenAIClientTransport(ginCtx, OpenAIClientTransportHTTP)
	return ginCtx, recorder
}

func benchmarkGatewayResponseFromRecorder(requestID string, recorder *httptest.ResponseRecorder, result *OpenAIForwardResult) *BenchmarkGatewayResponse {
	resp := &BenchmarkGatewayResponse{
		RequestID:   strings.TrimSpace(requestID),
		RawResponse: benchmarkGatewayRawResponse(recorder.Body.Bytes()),
		Content:     benchmarkGatewayContentFromBody(recorder.Body.Bytes()),
	}
	if result != nil {
		if strings.TrimSpace(result.RequestID) != "" {
			resp.RequestID = result.RequestID
		}
		resp.LatencyMS = int(result.Duration.Milliseconds())
		resp.PromptTokens = result.Usage.InputTokens
		resp.CompletionTokens = result.Usage.OutputTokens
		resp.TotalTokens = result.Usage.InputTokens + result.Usage.OutputTokens
	}
	if resp.RequestID == "" {
		resp.RequestID = requestID
	}
	return resp
}

func benchmarkGatewayRawResponse(body []byte) map[string]any {
	var raw map[string]any
	if len(body) == 0 || json.Unmarshal(body, &raw) != nil {
		return nil
	}
	return raw
}

func benchmarkGatewayContentFromBody(body []byte) string {
	if len(body) == 0 || !gjson.ValidBytes(body) {
		return strings.TrimSpace(string(body))
	}
	root := gjson.ParseBytes(body)
	if text := strings.TrimSpace(root.Get("output_text").String()); text != "" {
		return text
	}
	chatContent := root.Get("choices.0.message.content")
	if chatContent.IsArray() {
		var parts []string
		chatContent.ForEach(func(_, item gjson.Result) bool {
			var text string
			switch {
			case item.Type == gjson.String:
				text = item.String()
			case item.IsObject():
				text = item.Get("text").String()
			}
			if text = strings.TrimSpace(text); text != "" {
				parts = append(parts, text)
			}
			return true
		})
		if len(parts) > 0 {
			return strings.Join(parts, "\n")
		}
	}
	if text := strings.TrimSpace(chatContent.String()); text != "" {
		return text
	}
	var parts []string
	root.Get("output").ForEach(func(_, output gjson.Result) bool {
		output.Get("content").ForEach(func(_, content gjson.Result) bool {
			if text := strings.TrimSpace(content.Get("text").String()); text != "" {
				parts = append(parts, text)
			}
			return true
		})
		return true
	})
	if len(parts) > 0 {
		return strings.Join(parts, "\n")
	}
	return strings.TrimSpace(string(body))
}
