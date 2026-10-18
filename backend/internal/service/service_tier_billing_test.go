package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveBillingServiceTier(t *testing.T) {
	tests := []struct {
		name     string
		req      string
		observed string
		billing  string
		mismatch bool
	}{
		{name: "openai priority answered as default", req: "priority", observed: "default", billing: "priority", mismatch: true},
		{name: "anthropic fast answered as standard", req: "fast", observed: "standard", billing: "fast", mismatch: true},
		{name: "ultrafast answered as priority", req: "ultrafast", observed: "priority", billing: "ultrafast", mismatch: true},
		{name: "priority answered as flex", req: "priority", observed: "flex", billing: "priority", mismatch: true},
		{name: "untiered request answered as priority", req: "", observed: "priority", billing: "", mismatch: true},
		{name: "flex answered as default", req: "flex", observed: "default", billing: "flex", mismatch: true},
		{name: "priority honoured", req: "priority", observed: "priority", billing: "priority"},
		{name: "fast and priority are the same price level", req: "fast", observed: "priority", billing: "fast"},
		{name: "no declaration keeps request", req: "priority", observed: "", billing: "priority"},
		{name: "no request no declaration", req: "", observed: "", billing: ""},
		{name: "default echoed for untiered request", req: "", observed: "default", billing: ""},
		{name: "unknown response tier ignored", req: "priority", observed: "turbo", billing: "priority"},
		{name: "case and whitespace normalised", req: " Priority ", observed: "DEFAULT", billing: "priority", mismatch: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveBillingServiceTier(tt.req, tt.observed)
			require.Equal(t, tt.billing, got.Requested)
			require.Equal(t, tt.mismatch, got.Mismatch)
		})
	}
}

func TestCodexOAuthResponseTierIsNonAuthoritative(t *testing.T) {
	require.True(t, codexOAuthResponseTierIsNonAuthoritative("default"))
	require.False(t, codexOAuthResponseTierIsNonAuthoritative("standard"))
	require.False(t, codexOAuthResponseTierIsNonAuthoritative("flex"))
}
