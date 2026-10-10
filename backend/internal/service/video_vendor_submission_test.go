package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type pauseAccountRepoStub struct {
	AccountRepository
	calls []struct {
		id     int64
		until  time.Time
		reason string
	}
}

func (s *pauseAccountRepoStub) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	s.calls = append(s.calls, struct {
		id     int64
		until  time.Time
		reason string
	}{id, until, reason})
	return nil
}

func TestPauseAccountOnInsufficientBalance(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	repo := &pauseAccountRepoStub{}
	service := &VideoTaskService{accounts: repo, now: func() time.Time { return now }}
	accountID := int64(42)
	task := &VideoTask{PublicID: "video_x", Provider: VideoProviderWaveSpeed, AccountID: &accountID}

	service.pauseAccountOnInsufficientBalance(context.Background(), task, rejectedVideoProviderError("upstream", "insufficient_balance", "top up", http.StatusForbidden))
	require.Len(t, repo.calls, 1)
	require.Equal(t, accountID, repo.calls[0].id)
	require.Equal(t, now.Add(videoBalancePauseCooldown), repo.calls[0].until)
	require.Contains(t, repo.calls[0].reason, "wavespeed")

	// 其他错误码、非 provider 错误、没有账号的任务都不会暂停账号。
	service.pauseAccountOnInsufficientBalance(context.Background(), task, rejectedVideoProviderError("validation", "unsupported_size", "bad", http.StatusBadRequest))
	service.pauseAccountOnInsufficientBalance(context.Background(), task, errors.New("plain error"))
	service.pauseAccountOnInsufficientBalance(context.Background(), &VideoTask{PublicID: "video_y"}, rejectedVideoProviderError("upstream", "insufficient_balance", "top up", http.StatusForbidden))
	require.Len(t, repo.calls, 1)
}

type estimatorProviderStub struct {
	VideoProvider
	price float64
	ok    bool
	err   error
}

func (s *estimatorProviderStub) EstimateUpstreamCost(context.Context, *Account, VideoCreateRequest) (float64, bool, error) {
	return s.price, s.ok, s.err
}

func TestRecordUpstreamQuoteFlagsPricesAboveConfiguredBase(t *testing.T) {
	service := &VideoTaskService{now: time.Now}
	quote := &VideoPriceQuote{UnitPrice: 0.2, MaximumUnits: 5}
	account := &Account{ID: 1}

	snapshot := map[string]any{}
	service.recordUpstreamQuote(context.Background(), &resolvedVideoSubmission{provider: &estimatorProviderStub{price: 1.0, ok: true}, quote: quote, account: account}, snapshot)
	require.Equal(t, 1.0, snapshot["upstream_quote_usd"])
	require.Equal(t, 1.0, snapshot["configured_base_usd"])
	_, flagged := snapshot["upstream_quote_exceeds_price"]
	require.False(t, flagged, "a quote equal to the configured base is not flagged")

	snapshot = map[string]any{}
	service.recordUpstreamQuote(context.Background(), &resolvedVideoSubmission{provider: &estimatorProviderStub{price: 1.2, ok: true}, quote: quote, account: account}, snapshot)
	require.Equal(t, true, snapshot["upstream_quote_exceeds_price"])

	snapshot = map[string]any{}
	service.recordUpstreamQuote(context.Background(), &resolvedVideoSubmission{provider: &estimatorProviderStub{err: errors.New("price api down")}, quote: quote, account: account}, snapshot)
	require.Equal(t, "price api down", snapshot["upstream_quote_error"])
	_, hasQuote := snapshot["upstream_quote_usd"]
	require.False(t, hasQuote)

	snapshot = map[string]any{}
	service.recordUpstreamQuote(context.Background(), &resolvedVideoSubmission{provider: &estimatorProviderStub{ok: false}, quote: quote, account: account}, snapshot)
	require.Empty(t, snapshot, "providers that cannot quote leave the snapshot untouched")

	snapshot = map[string]any{}
	service.recordUpstreamQuote(context.Background(), &resolvedVideoSubmission{provider: newVideoVendorProvider(newRunwareVideoAdapter(), nil, nil), quote: quote, account: account}, snapshot)
	require.Empty(t, snapshot, "runware has no price endpoint")
}

func TestWaveSpeedEstimateUpstreamCost(t *testing.T) {
	var captured map[string]any
	var path string
	provider, server := newWaveSpeedTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(body, &captured))
		_, _ = w.Write([]byte(`{"code":200,"message":"success","data":{"model_id":"bytedance/seedance-2.0/text-to-video","price":1.2,"discounted_price":1.08,"discount_rate":90,"currency":"USD"}}`))
	})
	account := newWaveSpeedTestAccount(server.URL + "/api/v3")
	request := VideoCreateRequest{Operation: VideoOperationGenerate, Model: "bytedance/seedance-2.0/text-to-video", Prompt: "p", Seconds: 5, Resolution: "720p", Ratio: "16:9"}
	price, ok, err := provider.EstimateUpstreamCost(context.Background(), account, request)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, 1.2, price, "the pre-discount price matches the configured base price semantics")
	require.Equal(t, "/api/v3/model/price", path)
	require.Equal(t, "bytedance/seedance-2.0/text-to-video", captured["model_id"])
	inputs := captured["inputs"].(map[string]any)
	require.Equal(t, "p", inputs["prompt"])
	require.Equal(t, float64(5), inputs["duration"])
	require.Equal(t, "720p", inputs["resolution"])

	// 图生视频没有 URL 素材时不询价。
	_, ok, err = provider.EstimateUpstreamCost(context.Background(), account, VideoCreateRequest{Operation: VideoOperationGenerate, Model: "bytedance/seedance-2.5/image-to-video", Prompt: "p", Seconds: 5})
	require.Error(t, err, "a required image missing is a validation error")
	require.False(t, ok)

	// 价格接口报业务错误时返回错误而不是 0 价。
	provider, server = newWaveSpeedTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":4005,"message":"Failed to retrieve model pricing."}`))
	})
	_, ok, err = provider.EstimateUpstreamCost(context.Background(), newWaveSpeedTestAccount(server.URL+"/api/v3"), request)
	require.Error(t, err)
	require.False(t, ok)

	// Runware 没有询价接口。
	runware := newVideoVendorProvider(newRunwareVideoAdapter(), nil, nil)
	_, ok, err = runware.EstimateUpstreamCost(context.Background(), newRunwareTestAccount(""), VideoCreateRequest{Operation: VideoOperationGenerate, Model: "bytedance:seedance@2.0", Prompt: "p", Seconds: 5})
	require.NoError(t, err)
	require.False(t, ok)
}
