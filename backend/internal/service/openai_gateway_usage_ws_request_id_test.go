//go:build unit

package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// SEC-027：WebSocket 接入连接内，中继若每个 turn 都回同一个 response id，
// 仅用 "upstream:<id>" 做幂等键会把整条连接压成第一轮的一次扣费。
func TestOpenAIWSBillingRequestID_SameUpstreamIDDifferentTurnsProduceDistinctKeys(t *testing.T) {
	t.Parallel()
	first := &OpenAIForwardResult{RequestID: "resp_shared", OpenAIWSMode: true}
	second := &OpenAIForwardResult{RequestID: "resp_shared", OpenAIWSMode: true}

	keyTurn1 := openAIWSBillingRequestID(first.RequestID, 1)
	keyTurn2 := openAIWSBillingRequestID(second.RequestID, 2)

	require.Equal(t, "upstream:resp_shared#1", keyTurn1)
	require.Equal(t, "upstream:resp_shared#2", keyTurn2)
	require.NotEqual(t, keyTurn1, keyTurn2)
}

func TestOpenAIWSBillingRequestID_SameTurnIsStable(t *testing.T) {
	t.Parallel()
	require.Equal(t,
		openAIWSBillingRequestID("resp_a", 3),
		openAIWSBillingRequestID(" resp_a ", 3),
		"same upstream id and turn must yield the same idempotency key (retry-safe)")
}

func TestOpenAIWSBillingRequestID_WithoutTurnKeepsLegacyKey(t *testing.T) {
	t.Parallel()
	require.Equal(t, "upstream:resp_legacy", openAIWSBillingRequestID("resp_legacy", 0))
	require.Equal(t, "upstream:resp_legacy", openAIWSBillingRequestID("resp_legacy", -1))
}

func TestOpenAIWSBillingRequestID_StaysInUpstreamNamespace(t *testing.T) {
	t.Parallel()
	// 上游 id 伪装成其它命名空间也不能逃出 "upstream:" 前缀。
	key := openAIWSBillingRequestID("client:forged", 2)
	require.Equal(t, "upstream:client:forged#2", key)
}
