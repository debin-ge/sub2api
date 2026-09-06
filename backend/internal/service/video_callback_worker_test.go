package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type videoCallbackRepositoryStub struct {
	claimed          []*VideoCallbackDelivery
	enqueued         *VideoCallbackDelivery
	onEnqueue        func()
	deliveredID      int64
	deliveredStatus  int
	retriedID        int64
	retryAt          time.Time
	retryStatus      int
	retryError       string
	quarantinedID    int64
	quarantineReason string
	claimLimits      []int
	claimOwners      []string
	now              func() time.Time
	onRenew          func(context.Context, int64, string, time.Duration) error
	renewals         atomic.Int64
}

func (r *videoCallbackRepositoryStub) EnqueueVideoCallback(_ context.Context, delivery VideoCallbackDelivery) (*VideoCallbackDelivery, bool, error) {
	r.enqueued = &delivery
	if r.onEnqueue != nil {
		r.onEnqueue()
	}
	return &delivery, true, nil
}
func (r *videoCallbackRepositoryStub) ClaimVideoCallbacks(_ context.Context, owner string, limit int, lease time.Duration) ([]*VideoCallbackDelivery, error) {
	r.claimLimits = append(r.claimLimits, limit)
	r.claimOwners = append(r.claimOwners, owner)
	count := min(limit, len(r.claimed))
	claimed := r.claimed[:count]
	r.claimed = r.claimed[count:]
	now := time.Now()
	if r.now != nil {
		now = r.now()
	}
	for _, delivery := range claimed {
		expires := now.Add(lease)
		delivery.LeaseOwner, delivery.LeaseExpiresAt = &owner, &expires
	}
	return claimed, nil
}
func (r *videoCallbackRepositoryStub) RenewVideoCallbackLease(ctx context.Context, id int64, owner string, lease time.Duration) error {
	r.renewals.Add(1)
	if r.onRenew != nil {
		return r.onRenew(ctx, id, owner, lease)
	}
	return ctx.Err()
}
func (r *videoCallbackRepositoryStub) MarkVideoCallbackDelivered(ctx context.Context, id int64, _ string, status int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.deliveredID, r.deliveredStatus = id, status
	return nil
}
func (r *videoCallbackRepositoryStub) RetryVideoCallback(ctx context.Context, id int64, _ string, next time.Time, status int, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.retriedID, r.retryAt, r.retryStatus, r.retryError = id, next, status, message
	return nil
}
func (r *videoCallbackRepositoryStub) QuarantineVideoCallback(ctx context.Context, id int64, _ string, message string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.quarantinedID, r.quarantineReason = id, message
	return nil
}

type videoCallbackResolverStub struct {
	addresses []netip.Addr
	err       error
}

func (r videoCallbackResolverStub) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.addresses, r.err
}

type videoCallbackDoerFunc func(*http.Request) (*http.Response, error)

func (f videoCallbackDoerFunc) Do(request *http.Request) (*http.Response, error) { return f(request) }

func videoCallbackTestConfig() *config.Config {
	return &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{
		Enabled: true, LeaseSeconds: 30,
		Callback: config.GatewayVideoCallbackConfig{
			Enabled: true, RetryHours: 24, RequestTimeoutSeconds: 5, SigningSecret: "callback-secret",
		},
	}}}
}

func TestBuildVideoCallbackDeliveryContainsOnlySafeTerminalProjection(t *testing.T) {
	providerID := "video_upstream"
	target := "encrypted-target"
	access := "encrypted-provider-access"
	actual := 1.25
	task := &VideoTask{
		ID: 7, PublicID: "video_0123456789abcdef0123456789abcdef", UserID: 42,
		Provider: VideoProviderOpenAI, ProviderTaskID: &providerID,
		ProviderAccessEnc: &access, CallbackURLEnc: &target,
		GenerationState: VideoGenerationCompleted, BillingState: VideoBillingCaptured,
		ActualCost: &actual, CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}
	now := time.Unix(1_700_000_100, 0).UTC()

	delivery, needed, err := BuildVideoCallbackDelivery(task, videoCallbackTestConfig(), now, config.VideoDisclosureIdentity)

	require.NoError(t, err)
	require.True(t, needed)
	require.Equal(t, "video.completed", delivery.EventType)
	require.Equal(t, target, delivery.TargetURLEnc)
	raw, err := json.Marshal(delivery.Payload)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"provider":"openai"`)
	require.NotContains(t, string(raw), providerID)
	require.NotContains(t, string(raw), "provider_task_id")
	require.NotContains(t, string(raw), access)
	require.NotContains(t, string(raw), "actual_cost")
}

func TestBuildVideoCallbackDeliveryHonorsNoDisclosurePolicy(t *testing.T) {
	providerID := "video_upstream"
	target := "encrypted-target"
	task := &VideoTask{
		ID: 7, PublicID: "video_0123456789abcdef0123456789abcdef", Provider: VideoProviderOpenAI,
		ProviderTaskID: &providerID, CallbackURLEnc: &target,
		GenerationState: VideoGenerationCompleted, BillingState: VideoBillingCaptured,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	}

	delivery, needed, err := BuildVideoCallbackDelivery(task, videoCallbackTestConfig(), time.Now(), config.VideoDisclosureNone)

	require.NoError(t, err)
	require.True(t, needed)
	raw, err := json.Marshal(delivery.Payload)
	require.NoError(t, err)
	require.NotContains(t, string(raw), providerID)
	require.NotContains(t, string(raw), `"provider"`)
	require.NotContains(t, string(raw), "provider_task_id")
}

func TestVideoCallbackWorkerSignsAndMarksDelivered(t *testing.T) {
	now := time.Unix(1_700_000_100, 0).UTC()
	repository := &videoCallbackRepositoryStub{claimed: []*VideoCallbackDelivery{{
		ID: 9, EventID: "video_evt_123", EventType: "video.completed",
		Payload:      map[string]any{"id": "video_evt_123", "type": "video.completed"},
		TargetURLEnc: "enc:https://callback.example/hooks", ExpiresAt: now.Add(time.Hour),
	}}}
	worker := NewVideoCallbackWorker(repository, videoEncryptorStub{}, nil, videoCallbackTestConfig())
	worker.now = func() time.Time { return now }
	worker.resolver = videoCallbackResolverStub{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	worker.client = videoCallbackDoerFunc(func(request *http.Request) (*http.Response, error) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		hash := sha256.Sum256(body)
		hashHex := hex.EncodeToString(hash[:])
		require.Equal(t, hashHex, request.Header.Get("X-Sub2API-Content-SHA256"))
		require.Equal(t, signVideoCallback("callback-secret", strconvUnix(now), "video_evt_123", hashHex), request.Header.Get("X-Sub2API-Signature"))
		require.Empty(t, request.Header.Get("Authorization"))
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: http.NoBody}, nil
	})

	err := worker.ProcessBatch(context.Background(), 10)

	require.NoError(t, err)
	require.Equal(t, int64(9), repository.deliveredID)
	require.Equal(t, http.StatusNoContent, repository.deliveredStatus)
	require.Zero(t, repository.retriedID)
	require.Zero(t, repository.quarantinedID)
}

func TestVideoCallbackWorkerQuarantinesRedirect(t *testing.T) {
	now := time.Unix(1_700_000_100, 0).UTC()
	repository := &videoCallbackRepositoryStub{claimed: []*VideoCallbackDelivery{{
		ID: 10, EventID: "video_evt_redirect", Payload: map[string]any{"id": "video_evt_redirect"},
		TargetURLEnc: "enc:https://callback.example/hooks", ExpiresAt: now.Add(time.Hour),
	}}}
	worker := NewVideoCallbackWorker(repository, videoEncryptorStub{}, nil, videoCallbackTestConfig())
	worker.now = func() time.Time { return now }
	worker.resolver = videoCallbackResolverStub{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}}
	worker.client = videoCallbackDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://other.example"}}, Body: http.NoBody}, nil
	})

	err := worker.ProcessBatch(context.Background(), 10)

	require.NoError(t, err)
	require.Equal(t, int64(10), repository.quarantinedID)
	require.Contains(t, repository.quarantineReason, "permanent status 302")
}

func TestValidateVideoCallbackURLRejectsPrivateAndMixedDNS(t *testing.T) {
	for _, addresses := range [][]netip.Addr{
		{netip.MustParseAddr("127.0.0.1")},
		{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.1")},
	} {
		_, err := validateVideoCallbackURLWithResolver(
			context.Background(), "https://callback.example/hooks", videoCallbackResolverStub{addresses: addresses},
		)
		require.Error(t, err)
	}
	_, err := validateVideoCallbackURLWithResolver(
		context.Background(), "https://user:pass@callback.example/hooks", videoCallbackResolverStub{addresses: []netip.Addr{netip.MustParseAddr("93.184.216.34")}},
	)
	require.Error(t, err)
}

func strconvUnix(value time.Time) string {
	return strconv.FormatInt(value.Unix(), 10)
}
