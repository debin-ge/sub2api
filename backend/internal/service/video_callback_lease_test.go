package service

import (
	"context"
	"net/http"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func callbackLeaseFixture() (*VideoCallbackWorker, *videoCallbackRepositoryStub) {
	now := time.Now()
	repo := &videoCallbackRepositoryStub{claimed: []*VideoCallbackDelivery{{ID: 1, EventID: "lease-test",
		Payload: map[string]any{"id": 1}, TargetURLEnc: "enc:https://callback.example/hooks", ExpiresAt: now.Add(time.Hour)}}}
	worker := NewVideoCallbackWorker(repo, videoEncryptorStub{}, nil, videoCallbackTestConfig())
	worker.resolver = videoCallbackResolverStub{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	return worker, repo
}

func TestVideoCallbackClaimsOnlyWhenReadyToSend(t *testing.T) {
	worker, repo := callbackLeaseFixture()
	now := time.Now()
	worker.now, repo.now = func() time.Time { return now }, func() time.Time { return now }
	second := *repo.claimed[0]
	second.ID = 2
	repo.claimed = append(repo.claimed, &second)
	calls := 0
	worker.client = videoCallbackDoerFunc(func(*http.Request) (*http.Response, error) {
		calls++
		if calls == 1 {
			require.Nil(t, second.LeaseOwner, "waiting callbacks must remain unclaimed")
		}
		now = now.Add(time.Minute)
		return &http.Response{StatusCode: 204, Body: http.NoBody}, nil
	})
	require.NoError(t, worker.ProcessBatch(context.Background(), 2))
	require.Equal(t, 2, calls)
	require.Equal(t, []int{1, 1}, repo.claimLimits)
	require.NotEqual(t, repo.claimOwners[0], repo.claimOwners[1])
	require.Equal(t, int64(4), repo.renewals.Load())
}

func TestVideoCallbackLostLeaseNeverStartsHTTPRequest(t *testing.T) {
	worker, repo := callbackLeaseFixture()
	repo.onRenew = func(context.Context, int64, string, time.Duration) error { return ErrVideoCallbackLeaseLost }
	worker.client = videoCallbackDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP must not run without a live lease")
		return nil, nil
	})
	require.ErrorIs(t, worker.ProcessBatch(context.Background(), 1), ErrVideoCallbackLeaseLost)
	require.Zero(t, repo.deliveredID)
}

func TestVideoCallbackHeartbeatKeepsSlowRequestAlive(t *testing.T) {
	worker, repo := callbackLeaseFixture()
	worker.cfg.Gateway.Video.LeaseSeconds = 1
	renewed := make(chan struct{})
	repo.onRenew = func(context.Context, int64, string, time.Duration) error {
		if repo.renewals.Load() == 6 {
			close(renewed)
		}
		return nil
	}
	worker.client = videoCallbackDoerFunc(func(request *http.Request) (*http.Response, error) {
		select {
		case <-renewed:
			return &http.Response{StatusCode: 204, Body: http.NoBody}, nil
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
	})
	require.NoError(t, worker.ProcessBatch(context.Background(), 1))
	require.GreaterOrEqual(t, repo.renewals.Load(), int64(6))
	require.Equal(t, int64(1), repo.deliveredID)
}

func TestVideoCallbackHeartbeatLossCancelsHTTPRequest(t *testing.T) {
	worker, repo := callbackLeaseFixture()
	worker.cfg.Gateway.Video.LeaseSeconds = 1
	repo.onRenew = func(context.Context, int64, string, time.Duration) error {
		if repo.renewals.Load() >= 3 {
			return ErrVideoCallbackLeaseLost
		}
		return nil
	}
	worker.client = videoCallbackDoerFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	require.ErrorIs(t, worker.ProcessBatch(context.Background(), 1), ErrVideoCallbackLeaseLost)
	require.Zero(t, repo.deliveredID)
	require.Zero(t, repo.retriedID)
	require.Zero(t, repo.quarantinedID)
}
