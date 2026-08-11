package internalrelay

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestSignerRoundTripAndRejectsInvalidMarkers(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	signer := NewSigner("test-jwt-secret-32-bytes-long-value")
	marker, err := signer.Sign(42, "client:outer-request", now)
	require.NoError(t, err)

	metadata, err := signer.Verify(marker, now.Add(4*time.Minute))
	require.NoError(t, err)
	require.Equal(t, int64(42), metadata.AccountID)
	require.Equal(t, "client:outer-request", metadata.ParentRequestID)
	require.Equal(t, markerVersion, metadata.Version)

	parts := strings.Split(marker, ".")
	require.Len(t, parts, 2)
	tamperedPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"v":"v1","a":43,"iat":1,"p":"client:outer-request"}`))
	_, err = signer.Verify(tamperedPayload+"."+parts[1], now)
	require.Error(t, err)

	_, err = signer.Verify(marker, now.Add(markerLifetime+time.Second))
	require.Error(t, err)

	_, err = NewSigner("different-jwt-secret-32-bytes-long").Verify(marker, now)
	require.Error(t, err)
}

func TestUsageRequestIDRoundTrip(t *testing.T) {
	marked := MarkUsageRequestID("client:outer-request", "client:inner-request")
	require.True(t, strings.HasPrefix(marked, UsageRequestIDPrefix))
	require.Contains(t, marked, ":client:inner-request")

	parent, ok := ParseUsageRequestID(marked)
	require.True(t, ok)
	require.Equal(t, "client:outer-request", parent)
	require.Equal(t, marked, MarkUsageRequestID(parent, marked), "marking must be idempotent")

	_, ok = ParseUsageRequestID("client:ordinary-request")
	require.False(t, ok)
}

func TestIsLoopbackBaseURL(t *testing.T) {
	for _, raw := range []string{
		"http://localhost:8080",
		"HTTP://LOCALHOST:8080",
		"https://127.0.0.1/v1",
		"http://127.255.10.20:9000",
		"http://[::1]:8080/v1",
	} {
		require.Truef(t, IsLoopbackBaseURL(raw), "expected loopback URL: %s", raw)
	}
	for _, raw := range []string{
		"127.0.0.1:8080",
		"ftp://127.0.0.1",
		"https://localhost.example.com",
		"https://10.0.0.1",
		"https://example.com",
	} {
		require.Falsef(t, IsLoopbackBaseURL(raw), "expected non-loopback URL: %s", raw)
	}
}
