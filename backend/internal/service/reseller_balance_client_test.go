package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResellerBalanceClientFetchSuccess(t *testing.T) {
	var gotAuth string
	var gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/v1/balance", r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"balance":12.34,"user_id":42}`))
	}))
	t.Cleanup(srv.Close)

	client := NewResellerBalanceClient(srv.Client())

	result, err := client.Fetch(context.Background(), ResellerBalanceRequest{
		Endpoint: srv.URL + "/",
		APIKey:   " sk-parent ",
	})

	require.NoError(t, err)
	require.Equal(t, "Bearer sk-parent", gotAuth)
	require.Equal(t, "application/json", gotAccept)
	require.True(t, result.Enabled)
	require.True(t, result.Configured)
	require.Equal(t, srv.URL, result.UpstreamEndpoint)
	require.Equal(t, ResellerBalanceStatusOK, result.Status)
	require.InDelta(t, 12.34, result.Balance, 0.000001)
	require.Equal(t, int64(42), result.UserID)
	require.False(t, result.CheckedAt.IsZero())
}

func TestResellerBalanceClientFetchZeroBalanceSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"balance":0,"user_id":42}`))
	}))
	t.Cleanup(srv.Close)

	client := NewResellerBalanceClient(srv.Client())

	result, err := client.Fetch(context.Background(), ResellerBalanceRequest{
		Endpoint: srv.URL,
		APIKey:   "sk-parent",
	})

	require.NoError(t, err)
	require.Equal(t, ResellerBalanceStatusOK, result.Status)
	require.Zero(t, result.Balance)
	require.Equal(t, int64(42), result.UserID)
}

func TestResellerBalanceClientAuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	client := NewResellerBalanceClient(srv.Client())

	result, err := client.Fetch(context.Background(), ResellerBalanceRequest{
		Endpoint: srv.URL,
		APIKey:   "sk-parent",
	})

	require.NoError(t, err)
	require.Equal(t, ResellerBalanceStatusAuthFailed, result.Status)
	require.True(t, result.Configured)
}

func TestResellerBalanceClientInvalidResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"balance":"bad"}`))
	}))
	t.Cleanup(srv.Close)

	client := NewResellerBalanceClient(srv.Client())

	result, err := client.Fetch(context.Background(), ResellerBalanceRequest{
		Endpoint: srv.URL,
		APIKey:   "sk-parent",
	})

	require.NoError(t, err)
	require.Equal(t, ResellerBalanceStatusInvalidResponse, result.Status)
}

func TestResellerBalanceClientNotConfigured(t *testing.T) {
	client := NewResellerBalanceClient(nil)

	result, err := client.Fetch(context.Background(), ResellerBalanceRequest{})

	require.NoError(t, err)
	require.False(t, result.Configured)
	require.Equal(t, ResellerBalanceStatusNotConfigured, result.Status)
}
