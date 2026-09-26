//go:build unit

package service

import (
	"context"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

func TestResolveUsageBillingRequestID_ForcedWebSearchBeatsClientID(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "client-shared-id")
	got := resolveUsageBillingRequestID(ctx, "web_search:uuid-1")
	require.Equal(t, "web_search:uuid-1", got)
}

func TestResolveUsageBillingRequestID_ClientWinsOverPlainUpstream(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "client-shared-id")
	got := resolveUsageBillingRequestID(ctx, "resp_abc")
	require.Equal(t, "client:client-shared-id", got)
}

// SEC-016：ctxkey.RequestID 由 RequestLogger 从客户端 X-Request-ID 头透传，攻击者可固定它；
// 因此它绝不能参与计费幂等键。下面三组用例锁定"没有 local: 回退"这一约束。
func TestResolveUsageBillingRequestID_ClientControlledRequestIDFallsThroughToUpstream(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(context.Background(), ctxkey.RequestID, "attacker-fixed-x-request-id")
	got := resolveUsageBillingRequestID(ctx, "resp_upstream_1")
	require.Equal(t, "upstream:resp_upstream_1", got)
}

func TestResolveUsageBillingRequestID_ClientControlledRequestIDWithoutUpstreamIsGenerated(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(context.Background(), ctxkey.RequestID, "attacker-fixed-x-request-id")
	first := resolveUsageBillingRequestID(ctx, "")
	second := resolveUsageBillingRequestID(ctx, "   ")
	require.True(t, strings.HasPrefix(first, "generated:"), "got %q", first)
	require.True(t, strings.HasPrefix(second, "generated:"), "got %q", second)
	require.NotContains(t, first, "attacker-fixed-x-request-id")
	require.NotContains(t, first, "local:")
	// 同一个被固定的 X-Request-ID 重放两次必须得到两把不同的幂等键，不能被去重成一笔
	require.NotEqual(t, first, second)
}

func TestResolveUsageBillingRequestID_NilContextWithoutUpstreamIsGenerated(t *testing.T) {
	t.Parallel()
	got := resolveUsageBillingRequestID(nil, "") //nolint:staticcheck // 显式覆盖 nil ctx 分支
	require.True(t, strings.HasPrefix(got, "generated:"), "got %q", got)
	require.Greater(t, len(got), len("generated:"))
}

func TestResolveUsageBillingRequestID_ClientRequestIDStillWinsOverLocalAndUpstream(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "client-stable-1")
	ctx = context.WithValue(ctx, ctxkey.RequestID, "attacker-fixed-x-request-id")
	got := resolveUsageBillingRequestID(ctx, "resp_upstream_1")
	require.Equal(t, "client:client-stable-1", got)
}

func TestIsForcedUsageBillingRequestID(t *testing.T) {
	t.Parallel()
	require.True(t, isForcedUsageBillingRequestID("web_search:x"))
	require.True(t, isForcedUsageBillingRequestID("grok-video:task-1"))
	require.True(t, isForcedUsageBillingRequestID("grok_audio:up-1"))
	require.True(t, isForcedUsageBillingRequestID("grok_realtime:sess-1"))
	require.False(t, isForcedUsageBillingRequestID("resp_abc"))
}

func TestStableGrokAudioBillingRequestID(t *testing.T) {
	t.Parallel()
	require.Equal(t, "grok_audio:up-1", StableGrokAudioBillingRequestID("up-1"))
	require.Equal(t, "grok_audio:up-1", StableGrokAudioBillingRequestID("grok_audio:up-1"))
	got := StableGrokAudioBillingRequestID("")
	require.True(t, strings.HasPrefix(got, "grok_audio:"))
	require.Greater(t, len(got), len("grok_audio:"))
}

func TestStableGrokRealtimeBillingRequestID(t *testing.T) {
	t.Parallel()
	require.Equal(t, "grok_realtime:s1", StableGrokRealtimeBillingRequestID("s1"))
	require.Equal(t, "grok_realtime:s1", StableGrokRealtimeBillingRequestID("grok_realtime:s1"))
	got := StableGrokRealtimeBillingRequestID("")
	require.True(t, strings.HasPrefix(got, "grok_realtime:"))
}

func TestResolveUsageBillingRequestID_ForcedGrokAudioBeatsClientID(t *testing.T) {
	t.Parallel()
	ctx := context.WithValue(context.Background(), ctxkey.ClientRequestID, "client-shared-id")
	got := resolveUsageBillingRequestID(ctx, StableGrokAudioBillingRequestID("up-9"))
	require.Equal(t, "grok_audio:up-9", got)
}
