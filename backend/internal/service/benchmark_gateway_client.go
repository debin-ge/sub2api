package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
)

type BenchmarkGatewayClient interface {
	Execute(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error)
}

type BenchmarkGatewayRequest struct {
	RunID        int64
	RunTargetID  int64
	RunTaskID    int64
	Attempt      int
	ModelName    string
	ChannelID    int64
	Prompt       string
	InputPayload map[string]any
	Timeout      time.Duration
}

type BenchmarkGatewayResponse struct {
	RequestID        string
	Content          string
	RawResponse      map[string]any
	LatencyMS        int
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	EstimatedCost    float64
}

type benchmarkGatewayExecuteFunc func(context.Context, benchmarkGatewayInternalRequest) (*BenchmarkGatewayResponse, error)

type benchmarkGatewayClient struct {
	execute benchmarkGatewayExecuteFunc
}

var _ BenchmarkGatewayClient = (*benchmarkGatewayClient)(nil)

type benchmarkGatewayInternalRequest struct {
	RequestID       string
	UserPayload     map[string]any
	ForcedChannelID int64
	Timeout         time.Duration
}

type benchmarkGatewayForcedChannelIDKey struct{}

func newBenchmarkGatewayClient(execute benchmarkGatewayExecuteFunc) BenchmarkGatewayClient {
	return &benchmarkGatewayClient{execute: execute}
}

func (c *benchmarkGatewayClient) Execute(ctx context.Context, req BenchmarkGatewayRequest) (*BenchmarkGatewayResponse, error) {
	if c.execute == nil {
		return nil, errors.New("benchmark gateway executor is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	internalReq := buildBenchmarkGatewayInternalRequest(req)
	ctx = context.WithValue(ctx, benchmarkGatewayForcedChannelIDKey{}, req.ChannelID)
	ctx = context.WithValue(ctx, ctxkey.RequestID, internalReq.RequestID)
	ctx = context.WithValue(ctx, ctxkey.ClientRequestID, internalReq.RequestID)
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	resp, err := c.execute(ctx, internalReq)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		resp = &BenchmarkGatewayResponse{}
	}
	if strings.TrimSpace(resp.RequestID) == "" {
		resp.RequestID = internalReq.RequestID
	}
	return resp, nil
}

func buildBenchmarkGatewayInternalRequest(req BenchmarkGatewayRequest) benchmarkGatewayInternalRequest {
	return benchmarkGatewayInternalRequest{
		RequestID:       deterministicBenchmarkGatewayRequestID(req),
		UserPayload:     buildBenchmarkGatewayUserPayload(req),
		ForcedChannelID: req.ChannelID,
		Timeout:         req.Timeout,
	}
}

func deterministicBenchmarkGatewayRequestID(req BenchmarkGatewayRequest) string {
	return fmt.Sprintf("bench:%d:%d:%d:%d", req.RunID, req.RunTargetID, req.RunTaskID, req.Attempt)
}

func buildBenchmarkGatewayUserPayload(req BenchmarkGatewayRequest) map[string]any {
	payload := make(map[string]any, len(req.InputPayload)+2)
	for key, value := range req.InputPayload {
		if isBenchmarkGatewayInternalPayloadKey(key) {
			continue
		}
		payload[key] = value
	}
	payload["model"] = req.ModelName
	if _, ok := payload["input"]; !ok && payload["messages"] == nil && req.Prompt != "" {
		payload["input"] = req.Prompt
	}
	return payload
}

func isBenchmarkGatewayInternalPayloadKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "channel_id", "channelid", "forced_channel_id", "forcedchannelid":
		return true
	default:
		return false
	}
}

func benchmarkGatewayForcedChannelIDFromContext(ctx context.Context) (int64, bool) {
	if ctx == nil {
		return 0, false
	}
	value, ok := ctx.Value(benchmarkGatewayForcedChannelIDKey{}).(int64)
	return value, ok
}
