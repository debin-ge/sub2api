package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBenchmarkGatewayBuildsDeterministicRequestID(t *testing.T) {
	t.Parallel()

	var gotInternal benchmarkGatewayInternalRequest
	client := newBenchmarkGatewayClient(func(ctx context.Context, req benchmarkGatewayInternalRequest) (*BenchmarkGatewayResponse, error) {
		gotInternal = req
		return &BenchmarkGatewayResponse{}, nil
	})

	resp, err := client.Execute(context.Background(), BenchmarkGatewayRequest{
		RunID:       101,
		RunTargetID: 202,
		RunTaskID:   303,
		Attempt:     2,
		ModelName:   "target-model",
		ChannelID:   404,
		Prompt:      "answer briefly",
		Timeout:     time.Second,
	})
	if err != nil {
		t.Fatalf("Execute error = %v", err)
	}
	const wantRequestID = "bench:101:202:303:2"
	if resp.RequestID != wantRequestID {
		t.Fatalf("response request id = %q, want %q", resp.RequestID, wantRequestID)
	}
	if gotInternal.RequestID != wantRequestID {
		t.Fatalf("internal request id = %q, want %q", gotInternal.RequestID, wantRequestID)
	}
}

func TestBenchmarkGatewayRequestUsesTargetModelName(t *testing.T) {
	t.Parallel()

	var gotPayload map[string]any
	client := newBenchmarkGatewayClient(func(ctx context.Context, req benchmarkGatewayInternalRequest) (*BenchmarkGatewayResponse, error) {
		gotPayload = req.UserPayload
		return &BenchmarkGatewayResponse{RequestID: "provider-req-1"}, nil
	})

	resp, err := client.Execute(context.Background(), BenchmarkGatewayRequest{
		RunID:       1,
		RunTargetID: 2,
		RunTaskID:   3,
		Attempt:     1,
		ModelName:   "radar-target-model",
		ChannelID:   9,
		Prompt:      "solve",
		InputPayload: map[string]any{
			"model":       "task-default-model",
			"temperature": 0.2,
		},
	})
	if err != nil {
		t.Fatalf("Execute error = %v", err)
	}
	if resp.RequestID != "provider-req-1" {
		t.Fatalf("provider request id should be preserved, got %q", resp.RequestID)
	}
	if gotPayload["model"] != "radar-target-model" {
		t.Fatalf("payload model = %#v, want target model", gotPayload["model"])
	}
	if gotPayload["temperature"] != 0.2 {
		t.Fatalf("payload should preserve benchmark task fields, got %#v", gotPayload)
	}
}

func TestBenchmarkGatewayRequestCarriesForcedChannelIDInternally(t *testing.T) {
	t.Parallel()

	var gotInternal benchmarkGatewayInternalRequest
	var gotContextChannelID int64
	var gotContextOK bool
	client := newBenchmarkGatewayClient(func(ctx context.Context, req benchmarkGatewayInternalRequest) (*BenchmarkGatewayResponse, error) {
		gotInternal = req
		gotContextChannelID, gotContextOK = benchmarkGatewayForcedChannelIDFromContext(ctx)
		return &BenchmarkGatewayResponse{}, nil
	})

	_, err := client.Execute(context.Background(), BenchmarkGatewayRequest{
		RunID:       10,
		RunTargetID: 20,
		RunTaskID:   30,
		Attempt:     1,
		ModelName:   "target-model",
		ChannelID:   9876,
		Prompt:      "answer",
	})
	if err != nil {
		t.Fatalf("Execute error = %v", err)
	}
	if gotInternal.ForcedChannelID != 9876 {
		t.Fatalf("internal forced channel id = %d, want 9876", gotInternal.ForcedChannelID)
	}
	if !gotContextOK || gotContextChannelID != 9876 {
		t.Fatalf("context forced channel id = %d, ok=%v; want 9876,true", gotContextChannelID, gotContextOK)
	}
}

func TestBenchmarkGatewayRequestDoesNotExposeForcedChannelInUserPayload(t *testing.T) {
	t.Parallel()

	var gotPayload map[string]any
	client := newBenchmarkGatewayClient(func(ctx context.Context, req benchmarkGatewayInternalRequest) (*BenchmarkGatewayResponse, error) {
		gotPayload = req.UserPayload
		return &BenchmarkGatewayResponse{}, nil
	})

	_, err := client.Execute(context.Background(), BenchmarkGatewayRequest{
		RunID:       11,
		RunTargetID: 22,
		RunTaskID:   33,
		Attempt:     1,
		ModelName:   "target-model",
		ChannelID:   44,
		Prompt:      "answer",
		InputPayload: map[string]any{
			"channel_id":        int64(999),
			"forced_channel_id": int64(999),
			"messages": []any{
				map[string]any{"role": "user", "content": "answer"},
			},
		},
	})
	if err != nil {
		t.Fatalf("Execute error = %v", err)
	}
	if _, ok := gotPayload["channel_id"]; ok {
		t.Fatalf("payload leaked channel_id: %#v", gotPayload)
	}
	if _, ok := gotPayload["forced_channel_id"]; ok {
		t.Fatalf("payload leaked forced_channel_id: %#v", gotPayload)
	}
	payloadJSON, err := json.Marshal(gotPayload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if string(payloadJSON) == "" || !json.Valid(payloadJSON) {
		t.Fatalf("payload JSON is invalid: %q", string(payloadJSON))
	}
	if strings.Contains(string(payloadJSON), "channel_id") || strings.Contains(string(payloadJSON), "forced_channel_id") {
		t.Fatalf("payload JSON leaked forced channel fields: %s", string(payloadJSON))
	}
}
