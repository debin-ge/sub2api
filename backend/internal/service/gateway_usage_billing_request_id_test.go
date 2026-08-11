package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/internalrelay"
	"github.com/stretchr/testify/require"
)

func TestResolveUsageBillingRequestID_SeparatesIdentityNamespaces(t *testing.T) {
	clientCtx := context.WithValue(
		context.Background(),
		ctxkey.ClientRequestID,
		"shared",
	)
	localCtx := context.WithValue(
		context.Background(),
		ctxkey.RequestID,
		"shared",
	)

	clientID := resolveUsageBillingRequestID(clientCtx, "")
	localID := resolveUsageBillingRequestID(localCtx, "")
	upstreamID := resolveUsageBillingRequestID(context.Background(), "shared")
	generatedID := resolveUsageBillingRequestID(context.Background(), "")

	require.Equal(t, "client:shared", clientID)
	require.Equal(t, "local:shared", localID)
	require.Equal(t, "upstream:shared", upstreamID)
	require.True(t, strings.HasPrefix(generatedID, "generated:"))
	require.NotEqual(t, clientID, localID)
	require.NotEqual(t, clientID, upstreamID)
	require.NotEqual(t, localID, upstreamID)
	require.NotEqual(t, generatedID, clientID)
	require.NotEqual(t, generatedID, localID)
	require.NotEqual(t, generatedID, upstreamID)
}

func TestResolveUsageBillingRequestID_UpstreamCannotImpersonateAnotherNamespace(t *testing.T) {
	require.Equal(
		t,
		"upstream:client:shared",
		resolveUsageBillingRequestID(context.Background(), "client:shared"),
	)
	require.Equal(
		t,
		"upstream:local:shared",
		resolveUsageBillingRequestID(context.Background(), "local:shared"),
	)
	require.Equal(
		t,
		"upstream:generated:shared",
		resolveUsageBillingRequestID(context.Background(), "generated:shared"),
	)
}

func TestResolveUsageBillingRequestID_MarksVerifiedInternalRelayWithoutLosingInnerIdentity(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "inner-request")
	ctx = context.WithValue(ctx, ctxkey.InternalRelay, internalrelay.Metadata{
		ParentRequestID: "client:outer-request",
	})

	first := resolveUsageBillingRequestID(ctx, "")
	parent, ok := internalrelay.ParseUsageRequestID(first)
	require.True(t, ok)
	require.Equal(t, "client:outer-request", parent)
	require.Contains(t, first, ":client:inner-request")

	secondCtx := context.WithValue(ctx, ctxkey.ClientRequestID, "second-inner-request")
	second := resolveUsageBillingRequestID(secondCtx, "")
	require.NotEqual(t, first, second)
	require.Contains(t, second, ":client:second-inner-request")
}
