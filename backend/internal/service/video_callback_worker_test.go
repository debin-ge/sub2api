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

// Ark 形态的五家 provider 回调体就是 Ark 任务对象本身，不套事件信封；状态词、错误对象与
// 查询接口同构，消费方拿 id 再查一次即可得到签名链接。
func TestBuildVideoCallbackDeliveryUsesBareArkTaskObjectForArkProviders(t *testing.T) {
	providerID := "cgt-20260907-abcdef"
	target := "encrypted-target"
	access := "encrypted-provider-access"
	actual := 1.25
	settled := time.Unix(1_700_000_200, 0).UTC()
	task := &VideoTask{
		ID: 7, PublicID: "video_0123456789abcdef0123456789abcdef", UserID: 42, Provider: VideoProviderByteDance,
		PublicModel: ByteDanceVideoModelSeedance10Pro, ProviderTaskID: &providerID, ProviderAccessEnc: &access, CallbackURLEnc: &target,
		GenerationState: VideoGenerationCompleted, BillingState: VideoBillingCaptured, ActualCost: &actual,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(), UpdatedAt: time.Unix(1_700_000_150, 0).UTC(), SettledAt: &settled,
		ContentVariants:   []string{"video", "last_frame"},
		RequestAttributes: map[string]any{"resolution": "720p", "ratio": ArkVideoRatioAdaptive, "duration": 10, "seconds": 10, "callback_retry_hours": float64(6)},
		ResponseMetadata:  map[string]any{"seconds": float64(10), "ratio": "9:16", "seed": float64(5)},
		UsageSnapshot:     map[string]any{"video_tokens": float64(1234)},
	}

	delivery, needed, err := BuildVideoCallbackDelivery(task, videoCallbackTestConfig(), time.Now(), config.VideoDisclosureIdentity)
	require.NoError(t, err)
	require.True(t, needed)
	require.Equal(t, "video.completed", delivery.EventType)
	require.Equal(t, settled, delivery.CreatedAt)
	require.Equal(t, settled.Add(6*time.Hour), delivery.ExpiresAt, "the frozen retry window wins over the live config")
	payload := delivery.Payload
	require.Equal(t, task.PublicID, payload["id"])
	require.Equal(t, ArkVideoStatusSucceeded, payload["status"])
	require.Equal(t, ByteDanceVideoModelSeedance10Pro, payload["model"])
	require.Equal(t, int64(1_700_000_000), payload["created_at"])
	require.Equal(t, "720p", payload["resolution"])
	require.Equal(t, "9:16", payload["ratio"], "the observed ratio replaces adaptive")
	require.Equal(t, 10, payload["duration"])
	require.Equal(t, int64(5), payload["seed"])
	require.Equal(t, map[string]any{"completion_tokens": int64(1234), "total_tokens": int64(1234)}, payload["usage"])
	require.Equal(t, VideoProviderByteDance, payload["provider"])
	for _, absent := range []string{"object", "type", "data", "content", "error", "completed_at", "actual_cost", "provider_task_id"} {
		require.NotContains(t, payload, absent)
	}
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NotContains(t, string(raw), providerID)
	require.NotContains(t, string(raw), access)
	require.NotContains(t, string(raw), "1.25")

	// 无披露策略：不带 provider。
	delivery, needed, err = BuildVideoCallbackDelivery(task, videoCallbackTestConfig(), time.Now(), config.VideoDisclosureNone)
	require.NoError(t, err)
	require.True(t, needed)
	require.NotContains(t, delivery.Payload, "provider")

	// 取消：事件类型仍是 video.failed（与 OpenAI 形态同一条分发规则），但状态词与错误对象按 Ark 词表。
	code := "ModelNotOpen"
	cancelled := *task
	cancelled.GenerationState, cancelled.BillingState, cancelled.LastErrorCode = VideoGenerationCancelled, VideoBillingReleased, &code
	delivery, needed, err = BuildVideoCallbackDelivery(&cancelled, videoCallbackTestConfig(), time.Now(), config.VideoDisclosureNone)
	require.NoError(t, err)
	require.True(t, needed)
	require.Equal(t, "video.failed", delivery.EventType)
	require.Equal(t, ArkVideoStatusCancelled, delivery.Payload["status"])
	require.Equal(t, map[string]any{"code": "ModelNotOpen", "message": "video generation was cancelled"}, delivery.Payload["error"])
	require.NotContains(t, delivery.Payload, "content")

	// 生成完成但计费未终态：还不发。
	pending := *task
	pending.BillingState = VideoBillingCapturePending
	_, needed, err = BuildVideoCallbackDelivery(&pending, videoCallbackTestConfig(), time.Now(), config.VideoDisclosureNone)
	require.NoError(t, err)
	require.False(t, needed)

	// OpenAI 形态保持事件信封。
	openAI := *task
	openAI.Provider = VideoProviderOpenAI
	delivery, needed, err = BuildVideoCallbackDelivery(&openAI, videoCallbackTestConfig(), time.Now(), config.VideoDisclosureNone)
	require.NoError(t, err)
	require.True(t, needed)
	require.Equal(t, "event", delivery.Payload["object"])
	require.Equal(t, "video.completed", delivery.Payload["type"])
	require.Equal(t, VideoGenerationCompleted, delivery.Payload["data"].(map[string]any)["status"])
}
