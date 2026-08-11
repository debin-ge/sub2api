package handler

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/internalrelay"
	"github.com/stretchr/testify/require"
)

func TestSubmitUsageRecordTaskCopiesRequestContext(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxkey.ClientRequestID, "client-request-123")
	parent = context.WithValue(parent, ctxkey.RequestID, "request-456")
	relayMetadata := internalrelay.Metadata{
		Version:         "v1",
		AccountID:       42,
		IssuedAt:        time.Unix(1_700_000_000, 0).UTC(),
		ParentRequestID: "client:outer-request-123",
	}
	parent = context.WithValue(parent, ctxkey.InternalRelay, relayMetadata)

	var gotClientRequestID string
	var gotRequestID string
	var gotRelayMetadata internalrelay.Metadata
	h := &GatewayHandler{}
	h.submitUsageRecordTask(parent, func(ctx context.Context) {
		gotClientRequestID, _ = ctx.Value(ctxkey.ClientRequestID).(string)
		gotRequestID, _ = ctx.Value(ctxkey.RequestID).(string)
		gotRelayMetadata, _ = ctx.Value(ctxkey.InternalRelay).(internalrelay.Metadata)
	})

	require.Equal(t, "client-request-123", gotClientRequestID)
	require.Equal(t, "request-456", gotRequestID)
	require.Equal(t, relayMetadata, gotRelayMetadata)
}

func TestOpenAISubmitUsageRecordTaskCopiesRequestContext(t *testing.T) {
	parent := context.WithValue(context.Background(), ctxkey.ClientRequestID, "openai-client-request-123")
	parent = context.WithValue(parent, ctxkey.RequestID, "openai-request-456")
	relayMetadata := internalrelay.Metadata{
		Version:         "v1",
		AccountID:       84,
		IssuedAt:        time.Unix(1_700_000_001, 0).UTC(),
		ParentRequestID: "client:openai-outer-request-123",
	}
	parent = context.WithValue(parent, ctxkey.InternalRelay, relayMetadata)

	var gotClientRequestID string
	var gotRequestID string
	var gotRelayMetadata internalrelay.Metadata
	h := &OpenAIGatewayHandler{}
	h.submitUsageRecordTask(parent, func(ctx context.Context) {
		gotClientRequestID, _ = ctx.Value(ctxkey.ClientRequestID).(string)
		gotRequestID, _ = ctx.Value(ctxkey.RequestID).(string)
		gotRelayMetadata, _ = ctx.Value(ctxkey.InternalRelay).(internalrelay.Metadata)
	})

	require.Equal(t, "openai-client-request-123", gotClientRequestID)
	require.Equal(t, "openai-request-456", gotRequestID)
	require.Equal(t, relayMetadata, gotRelayMetadata)
}
