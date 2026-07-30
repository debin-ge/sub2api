package handler

import (
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSTurnPayloadHash_IsScopedToExactTurn(t *testing.T) {
	var slot atomic.Pointer[openAIWSTurnPayloadHashSnapshot]
	firstPayload := []byte(`{"type":"response.create","input":"first"}`)
	secondPayload := []byte(`{"type":"response.create","input":"second"}`)

	firstHash := sealOpenAIWSTurnPayloadHash(&slot, 1, firstPayload)
	require.Equal(t, service.HashUsageRequestPayload(firstPayload), firstHash)
	require.Equal(t, firstHash, loadOpenAIWSTurnPayloadHash(&slot, 1, "fallback"))
	require.Equal(t, "fallback", loadOpenAIWSTurnPayloadHash(&slot, 2, "fallback"))

	secondHash := sealOpenAIWSTurnPayloadHash(&slot, 2, secondPayload)
	require.Equal(t, service.HashUsageRequestPayload(secondPayload), secondHash)
	require.NotEqual(t, firstHash, secondHash)
	require.Equal(t, secondHash, loadOpenAIWSTurnPayloadHash(&slot, 2, "fallback"))

	// A same-turn upstream retry deliberately reuses the sealed payload hash.
	require.Equal(t, secondHash, loadOpenAIWSTurnPayloadHash(&slot, 2, "fallback"))
}
