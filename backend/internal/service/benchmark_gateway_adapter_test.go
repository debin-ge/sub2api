package service

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type benchmarkChannelGroupResolverStub struct {
	gotChannelID int64
	gotGroupIDs  []int64
	groupIDs     []int64
	platforms    map[int64]string
}

func (s *benchmarkChannelGroupResolverStub) GetGroupIDs(ctx context.Context, channelID int64) ([]int64, error) {
	s.gotChannelID = channelID
	return append([]int64(nil), s.groupIDs...), nil
}

func (s *benchmarkChannelGroupResolverStub) GetGroupPlatforms(ctx context.Context, groupIDs []int64) (map[int64]string, error) {
	s.gotGroupIDs = append([]int64(nil), groupIDs...)
	return s.platforms, nil
}

type benchmarkOpenAIGatewayStub struct {
	selectCalled bool
	gotGroupID   *int64
	gotModel     string
	gotBody      string
}

func (s *benchmarkOpenAIGatewayStub) SelectAccountForModelWithExclusions(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}) (*Account, error) {
	s.selectCalled = true
	if groupID != nil {
		value := *groupID
		s.gotGroupID = &value
	}
	s.gotModel = requestedModel
	return &Account{ID: 1}, nil
}

func (s *benchmarkOpenAIGatewayStub) Forward(ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
	s.gotBody = string(body)
	c.JSON(http.StatusOK, gin.H{"output_text": "adapter ok"})
	return &OpenAIForwardResult{
		RequestID: "upstream-req-1",
		Usage: OpenAIUsage{
			InputTokens:  2,
			OutputTokens: 3,
		},
		Duration: 12 * time.Millisecond,
	}, nil
}

func TestBenchmarkAdapterUsesForcedChannelOpenAIGroupForSelection(t *testing.T) {
	t.Parallel()

	channelResolver := &benchmarkChannelGroupResolverStub{
		groupIDs:  []int64{101, 202},
		platforms: map[int64]string{101: PlatformAnthropic, 202: PlatformOpenAI},
	}
	gateway := &benchmarkOpenAIGatewayStub{}
	adapter := &benchmarkOpenAIGatewayAdapter{
		gateway:      gateway,
		channelGroup: channelResolver,
	}

	resp, err := adapter.execute(context.Background(), benchmarkGatewayInternalRequest{
		RequestID:       "bench:req",
		ForcedChannelID: 77,
		UserPayload: map[string]any{
			"model": "gpt-4.1",
			"input": "answer",
		},
	})
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}

	if channelResolver.gotChannelID != 77 {
		t.Fatalf("GetGroupIDs channel = %d, want 77", channelResolver.gotChannelID)
	}
	if len(channelResolver.gotGroupIDs) != 2 || channelResolver.gotGroupIDs[0] != 101 || channelResolver.gotGroupIDs[1] != 202 {
		t.Fatalf("GetGroupPlatforms groups = %#v, want [101 202]", channelResolver.gotGroupIDs)
	}
	if gateway.gotGroupID == nil || *gateway.gotGroupID != 202 {
		t.Fatalf("selection group = %#v, want 202", gateway.gotGroupID)
	}
	if gateway.gotModel != "gpt-4.1" {
		t.Fatalf("selection model = %q, want gpt-4.1", gateway.gotModel)
	}
	if resp.RequestID != "upstream-req-1" {
		t.Fatalf("response request id = %q, want upstream-req-1", resp.RequestID)
	}
	if resp.Content != "adapter ok" {
		t.Fatalf("response content = %q, want adapter ok", resp.Content)
	}
	if resp.TotalTokens != 5 {
		t.Fatalf("response total tokens = %d, want 5", resp.TotalTokens)
	}
}

func TestBenchmarkAdapterPayloadBodyDoesNotExposeForcedChannel(t *testing.T) {
	t.Parallel()

	body, err := benchmarkGatewayPayloadBody(map[string]any{
		"model":             "gpt-4.1",
		"input":             "answer",
		"channel_id":        int64(999),
		"forced_channel_id": int64(888),
	})
	if err != nil {
		t.Fatalf("payload body error = %v", err)
	}
	bodyText := string(body)
	if strings.Contains(bodyText, "channel_id") || strings.Contains(bodyText, "forced_channel_id") {
		t.Fatalf("payload leaked forced channel fields: %s", bodyText)
	}
}

func TestBenchmarkAdapterPayloadBodyForcesStreamFalse(t *testing.T) {
	t.Parallel()

	body, err := benchmarkGatewayPayloadBody(map[string]any{
		"model":  "gpt-4.1",
		"input":  "answer",
		"stream": true,
	})
	if err != nil {
		t.Fatalf("payload body error = %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got, ok := payload["stream"].(bool); !ok || got {
		t.Fatalf("stream = %#v, want false in body %s", payload["stream"], string(body))
	}
}

func TestBenchmarkGatewayContentFromBodyExtractsChatContentArray(t *testing.T) {
	t.Parallel()

	body := []byte(`{"choices":[{"message":{"content":[" first ",{"type":"text","text":" second "},{"text":"   "},{"type":"image_url","image_url":{"url":"ignored"}},7]}}]}`)

	if got := benchmarkGatewayContentFromBody(body); got != "first\nsecond" {
		t.Fatalf("content = %q, want %q", got, "first\nsecond")
	}
}

func TestBenchmarkAdapterReturnsErrorWhenForcedChannelHasNoOpenAIGroup(t *testing.T) {
	t.Parallel()

	channelResolver := &benchmarkChannelGroupResolverStub{
		groupIDs:  []int64{101},
		platforms: map[int64]string{101: PlatformAnthropic},
	}
	gateway := &benchmarkOpenAIGatewayStub{}
	adapter := &benchmarkOpenAIGatewayAdapter{
		gateway:      gateway,
		channelGroup: channelResolver,
	}

	_, err := adapter.execute(context.Background(), benchmarkGatewayInternalRequest{
		RequestID:       "bench:req",
		ForcedChannelID: 77,
		UserPayload: map[string]any{
			"model": "gpt-4.1",
			"input": "answer",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "no openai-compatible group") {
		t.Fatalf("execute error = %v, want no openai-compatible group", err)
	}
	if gateway.selectCalled {
		t.Fatal("selector should not be called when forced channel has no OpenAI group")
	}
}
