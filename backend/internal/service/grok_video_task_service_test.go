package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type grokTaskMemoryRepo struct {
	GrokVideoTaskRepository
	mu           sync.Mutex
	task         *GrokVideoTask
	saved        int
	usage        *UsageLog
	simpleWrites int
	failSave     bool
}

func cloneGrokTask(t *GrokVideoTask) *GrokVideoTask {
	if t == nil {
		return nil
	}
	b, _ := json.Marshal(t)
	var copy GrokVideoTask
	_ = json.Unmarshal(b, &copy)
	return &copy
}
func (r *grokTaskMemoryRepo) Get(context.Context, string) (*GrokVideoTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.task == nil {
		return nil, ErrGrokVideoNotFound
	}
	return cloneGrokTask(r.task), nil
}
func (r *grokTaskMemoryRepo) FindOwned(_ context.Context, u, k, g int64, id string) (*GrokVideoTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.task == nil || r.task.UserID != u || r.task.APIKeyID != k || r.task.GroupID != g || r.task.ProviderTaskID == nil || *r.task.ProviderTaskID != id {
		return nil, ErrGrokVideoNotFound
	}
	return cloneGrokTask(r.task), nil
}
func (r *grokTaskMemoryRepo) Create(_ context.Context, t *GrokVideoTask) (*GrokVideoTask, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.task = cloneGrokTask(t)
	r.task.ID = 1
	r.task.CreatedAt = time.Now()
	return cloneGrokTask(r.task), true, nil
}
func (r *grokTaskMemoryRepo) Save(_ context.Context, t *GrokVideoTask, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failSave {
		return errors.New("db down")
	}
	if r.task != nil && (t.Version != r.task.Version || t.LeaseEpoch != r.task.LeaseEpoch) {
		return ErrGrokVideoBusy
	}
	t.Version++
	r.task = cloneGrokTask(t)
	r.saved++
	return nil
}
func (r *grokTaskMemoryRepo) Claim(_ context.Context, id, owner string, _ bool) (*GrokVideoTask, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.task.LeaseOwner != nil {
		return nil, ErrGrokVideoBusy
	}
	r.task.LeaseEpoch++
	r.task.Version++
	r.task.LeaseOwner = &owner
	until := time.Now().Add(time.Minute)
	r.task.LeaseExpiresAt = &until
	return cloneGrokTask(r.task), nil
}
func (r *grokTaskMemoryRepo) RecordedUsage(context.Context, string, int64) (*UsageLog, error) {
	return r.usage, nil
}
func (r *grokTaskMemoryRepo) ExistingBilling(context.Context, string, int64) (*PreparedGrokVideoBilling, error) {
	return nil, nil
}
func (r *grokTaskMemoryRepo) RecordSimpleUsage(_ context.Context, log *UsageLog) error {
	if r.usage == nil {
		r.simpleWrites++
		r.usage = log
	}
	return nil
}
func (r *grokTaskMemoryRepo) FindIdempotent(_ context.Context, u, k int64, op, key string) (*GrokVideoTask, error) {
	if r.task != nil && r.task.IdempotencyKey != nil && *r.task.IdempotencyKey == key && r.task.UserID == u && r.task.APIKeyID == k && r.task.Operation == op {
		return cloneGrokTask(r.task), nil
	}
	return nil, ErrGrokVideoNotFound
}

type grokTrackingCipher struct{}

func (grokTrackingCipher) Encrypt(s string) (string, error) { return "encrypted:" + s, nil }
func (grokTrackingCipher) Decrypt(s string) (string, error) {
	if !strings.HasPrefix(s, "encrypted:") {
		return "", errors.New("bad ciphertext")
	}
	return strings.TrimPrefix(s, "encrypted:"), nil
}

type grokTaskAccounts struct {
	AccountRepository
	account *Account
}

func (r grokTaskAccounts) GetByID(context.Context, int64) (*Account, error) { return r.account, nil }

type grokTrackingHTTP struct {
	HTTPUpstream
	calls  int
	body   string
	status int
	before func()
}

func (u *grokTrackingHTTP) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.calls++
	if u.before != nil {
		u.before()
	}
	return &http.Response{StatusCode: u.status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(u.body))}, nil
}
func grokTrackingFixture(t *testing.T) (*GrokVideoTaskService, *grokTaskMemoryRepo, *grokTrackingHTTP) {
	t.Helper()
	id := "upstream-task"
	now := time.Now()
	repo := &grokTaskMemoryRepo{task: &GrokVideoTask{ID: 1, PublicID: "grok_video_00000000000000000000000000000001", UserID: 10, APIKeyID: 20, GroupID: 24, AccountID: 1, ProviderTaskID: &id, GenerationState: "queued", SyncStatus: "scheduled", SettlementStatus: "not_due", CreatedAt: now, NextActionAt: &now}}
	a := &Account{ID: 1, Platform: PlatformGrok, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, GroupIDs: []int64{24}, Credentials: map[string]any{"api_key": "test"}}
	accounts := grokTaskAccounts{account: a}
	upstream := &grokTrackingHTTP{body: `{"status":"pending","progress":1}`, status: 200}
	cfg := &config.Config{}
	gateway := &OpenAIGatewayService{accountRepo: accounts, httpUpstream: upstream, cfg: cfg}
	return &GrokVideoTaskService{repo: repo, accounts: accounts, gateway: gateway, encryptor: grokTrackingCipher{}, cfg: cfg}, repo, upstream
}
func TestGrokTrackedObservationPersistsAndDoesNotRegress(t *testing.T) {
	s, r, u := grokTrackingFixture(t)
	ctx := context.Background()
	task, e := r.Claim(ctx, r.task.PublicID, "worker", true)
	require.NoError(t, e)
	require.NoError(t, s.processClaim(ctx, task, false))
	require.Equal(t, "in_progress", r.task.GenerationState)
	u.body = `{"status":"done","progress":100,"video":{"url":"https://vidgen.x.ai/test.mp4","duration":8},"usage":{"cost_in_usd_ticks":6900000000}}`
	task, e = r.Claim(ctx, r.task.PublicID, "worker", true)
	require.NoError(t, e)
	require.NoError(t, s.processClaim(ctx, task, false))
	require.Equal(t, "completed", r.task.GenerationState)
	require.Equal(t, "pending", r.task.SettlementStatus)
	require.Equal(t, json.Number("6900000000"), r.task.UpstreamCostTicks)
	u.body = `{"status":"pending","progress":1}`
	task, e = r.Claim(ctx, r.task.PublicID, "worker", true)
	require.NoError(t, e)
	require.NoError(t, s.processClaim(ctx, task, false))
	require.Equal(t, "completed", r.task.GenerationState)
	require.Equal(t, 2, u.calls)
}
func TestGrokTrackedLookupSurvivesRedisLossAndGenerationCooldown(t *testing.T) {
	s, r, u := grokTrackingFixture(t)
	until := time.Now().Add(time.Hour)
	s.accounts.(grokTaskAccounts).account.RateLimitResetAt = &until
	group := int64(24)
	task, e := s.Resolve(context.Background(), &APIKey{ID: 20, UserID: 10, GroupID: &group}, *r.task.ProviderTaskID)
	require.NoError(t, e)
	task, e = s.Observe(context.Background(), task, false)
	require.NoError(t, e)
	require.Equal(t, "in_progress", task.GenerationState)
	require.Equal(t, 1, u.calls)
	_, e = s.Resolve(context.Background(), &APIKey{ID: 21, UserID: 10, GroupID: &group}, *r.task.ProviderTaskID)
	require.ErrorIs(t, e, ErrGrokVideoNotFound)
}
func TestGrokTrackedErrorsRetainTaskAndLease(t *testing.T) {
	for _, status := range []int{404, 429, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			s, r, u := grokTrackingFixture(t)
			u.status = status
			task, e := r.Claim(context.Background(), r.task.PublicID, "worker", true)
			require.NoError(t, e)
			require.Error(t, s.processClaim(context.Background(), task, false))
			require.Equal(t, "queued", r.task.GenerationState)
			require.Equal(t, "retrying", r.task.SyncStatus)
			require.Nil(t, r.task.LeaseOwner)
		})
	}
}
func TestGrokTrackedSimpleSettlementUsesFrozenSnapshotOnce(t *testing.T) {
	s, r, _ := grokTrackingFixture(t)
	r.task.GenerationState = "completed"
	r.task.SettlementStatus = "pending"
	snapshot := &PreparedGrokVideoBilling{Simple: true, UsageLog: &UsageLog{ID: 88, APIKeyID: 20, RequestID: StableGrokVideoBillingRequestID(*r.task.ProviderTaskID), ActualCost: 0.42}}
	enc, e := s.seal(snapshot)
	require.NoError(t, e)
	r.task.SnapshotEnc = enc
	for range 3 {
		task, e := r.Claim(context.Background(), r.task.PublicID, "worker", true)
		require.NoError(t, e)
		require.NoError(t, s.processClaim(context.Background(), task, true))
	}
	require.Equal(t, 1, r.simpleWrites)
	require.Equal(t, "settled", r.task.SettlementStatus)
	require.Equal(t, 0.42, *r.task.ActualCost)
}
func TestGrokTrackedMissingSnapshotBlocksBilling(t *testing.T) {
	s, r, _ := grokTrackingFixture(t)
	r.task.GenerationState = "completed"
	r.task.BillingSnapshotMissing = true
	task, e := r.Claim(context.Background(), r.task.PublicID, "worker", true)
	require.NoError(t, e)
	require.NoError(t, s.processClaim(context.Background(), task, true))
	require.Equal(t, "blocked", r.task.SettlementStatus)
	require.Zero(t, r.simpleWrites)
}
func TestGrokTrackedCreateBeforeResponseAndReplay(t *testing.T) {
	s, r, _ := grokTrackingFixture(t)
	group := int64(24)
	key := &APIKey{ID: 20, UserID: 10, GroupID: &group, Group: &Group{ID: 24, Platform: PlatformGrok}}
	task, created, e := s.Begin(context.Background(), key, s.accounts.(grokTaskAccounts).account, nil, GrokMediaEndpointVideosGenerations, "idem", "/v1/videos/generations", []byte(`{"model":"grok-imagine-video","prompt":"hi"}`), ChannelUsageFields{})
	require.NoError(t, e)
	require.True(t, created)
	require.Nil(t, r.task.ProviderTaskID)
	require.NoError(t, s.Accepted(context.Background(), task, 200, []byte(`{"request_id":"created"}`)))
	require.Equal(t, "created", *r.task.ProviderTaskID)
	replay, e := s.Replay(context.Background(), key, GrokMediaEndpointVideosGenerations, "idem", []byte(`{ "prompt":"hi", "model":"grok-imagine-video" }`))
	require.NoError(t, e)
	require.NotNil(t, replay)
	_, e = s.Replay(context.Background(), key, GrokMediaEndpointVideosGenerations, "idem", []byte(`{"prompt":"different"}`))
	require.ErrorIs(t, e, ErrGrokVideoConflict)
	body, status, e := s.CreateResponse(replay)
	require.NoError(t, e)
	require.Equal(t, 200, status)
	require.JSONEq(t, `{"request_id":"created"}`, string(body))
}

func (r *grokTaskMemoryRepo) Due(_ context.Context, _ int) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.task != nil && r.task.NextActionAt != nil && !r.task.NextActionAt.After(time.Now()) && r.task.LeaseOwner == nil {
		return []string{r.task.PublicID}, nil
	}
	return nil, nil
}
func TestGrokTrackedWorkerCompletesWithoutClientPolling(t *testing.T) {
	s, r, u := grokTrackingFixture(t)
	u.body = `{"status":"done","progress":100,"video":{"url":"https://vidgen.x.ai/test.mp4","duration":8}}`
	enc, err := s.seal(&PreparedGrokVideoBilling{Simple: true, UsageLog: &UsageLog{ID: 10, RequestID: StableGrokVideoBillingRequestID(*r.task.ProviderTaskID), APIKeyID: 20}})
	require.NoError(t, err)
	r.task.SnapshotEnc = enc
	s.cfg.GrokVideoTracking.WorkerEnabled = true
	s.Start()
	defer s.Stop()
	require.Eventually(t, func() bool {
		task, e := r.Get(context.Background(), "")
		return e == nil && task.SettlementStatus == "settled"
	}, 8*time.Second, 100*time.Millisecond)
	task, err := r.Get(context.Background(), "")
	require.NoError(t, err)
	require.Equal(t, "completed", task.GenerationState)
	require.Nil(t, task.NextActionAt)
}

type grokLegacyTrackingCache struct {
	*schedulerTestGatewayCache
	pending []byte
}

func (c *grokLegacyTrackingCache) GetGrokVideoPendingBilling(context.Context, string) ([]byte, error) {
	return c.pending, nil
}
func TestGrokTrackedImportsOnlyVerifiedLegacyOwnership(t *testing.T) {
	s, r, _ := grokTrackingFixture(t)
	r.task = nil
	s.cfg.GrokVideoTracking.Enabled = true
	cache := &grokLegacyTrackingCache{schedulerTestGatewayCache: &schedulerTestGatewayCache{}, pending: []byte(`{"model":"grok-imagine-video","video_resolution":"720p","video_duration_seconds":8}`)}
	s.gateway.cache = cache
	group := int64(24)
	key := &APIKey{ID: 20, UserID: 10, GroupID: &group, Group: &Group{ID: 24, Platform: PlatformGrok}}
	require.NoError(t, s.gateway.BindGrokMediaVideoRequestAccount(context.Background(), &group, "legacy", 10, 20, 1))
	task, err := s.Resolve(context.Background(), key, "legacy")
	require.NoError(t, err)
	require.True(t, task.ImportedFromLegacy)
	require.False(t, task.BillingSnapshotMissing)
	require.Equal(t, "720p", task.Resolution)
	// The unguessable provider ID alone cannot authorize backfill for another key.
	other := *key
	other.ID = 21
	_, err = s.Resolve(context.Background(), &other, "legacy")
	require.ErrorIs(t, err, ErrGrokVideoNotFound)
}
func TestGrokTrackedCreationDoesNotStoreReferenceContents(t *testing.T) {
	s, r, _ := grokTrackingFixture(t)
	group := int64(24)
	key := &APIKey{ID: 20, UserID: 10, GroupID: &group}
	_, _, err := s.Begin(context.Background(), key, s.accounts.(grokTaskAccounts).account, nil, GrokMediaEndpointVideosGenerations, "", "/v1/videos", []byte(`{"model":"grok-imagine-video-1.5","prompt":"test","reference_images":[{"url":"data:image/png;base64,secret-pixels"},{"url":"https://example.test/photo?token=secret-token"}]}`), ChannelUsageFields{})
	require.NoError(t, err)
	require.Len(t, r.task.InputManifest, 2)
	encoded, err := json.Marshal(r.task)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-pixels")
	require.NotContains(t, string(encoded), "secret-token")
}

func TestGrokTrackedBillingPreparationHasNoEffects(t *testing.T) {
	for _, simple := range []bool{false, true} {
		t.Run(map[bool]string{false: "balance", true: "simple"}[simple], func(t *testing.T) {
			s, _, _ := grokTrackingFixture(t)
			s.cfg.Default.RateMultiplier = 1
			if simple {
				s.cfg.RunMode = config.RunModeSimple
			}
			s.gateway.billingService = NewBillingService(s.cfg, nil)
			groupID := int64(24)
			user := &User{ID: 10, Balance: 100}
			key := &APIKey{ID: 20, UserID: 10, GroupID: &groupID, Group: &Group{ID: 24, Platform: PlatformGrok, RateMultiplier: 1}, User: user}
			var prepared *PreparedGrokVideoBilling
			err := s.gateway.RecordUsage(context.Background(), &OpenAIRecordUsageInput{APIKey: key, User: user, Account: s.accounts.(grokTaskAccounts).account, BillingKind: BillingKindVideo, QuotaPlatform: PlatformGrok,
				Result:      &OpenAIForwardResult{RequestID: "grok-video:billing-test", ResponseID: "billing-test", Model: "grok-imagine-video", BillingModel: "grok-imagine-video", VideoCount: 1, VideoResolution: "720p", VideoDurationSeconds: 8},
				PrepareOnly: func(value *PreparedGrokVideoBilling) error { prepared = value; return nil },
			})
			require.NoError(t, err)
			require.NotNil(t, prepared)
			require.NotNil(t, prepared.UsageLog)
			require.Equal(t, simple, prepared.Simple)
			require.Equal(t, "grok-video:billing-test", prepared.UsageLog.RequestID)
			require.InDelta(t, 0.56, prepared.UsageLog.ActualCost, 1e-8)
			if !simple {
				require.NotNil(t, prepared.Command)
				require.InDelta(t, 0.56, prepared.Command.BalanceCost, 1e-8)
			}
			require.Equal(t, float64(100), user.Balance)
			// No usage repository or billing repository is installed: any accidental
			// application would fail this test instead of merely checking a mock call.
		})
	}
}

func TestLegacyGrokReplaySurvivesCompositeRouteRewrite(t *testing.T) {
	svc, repo, _ := grokTrackingFixture(t)
	groupID := int64(24)
	key := &APIKey{ID: 20, UserID: 10, GroupID: &groupID}
	idem := "legacy-key"
	repo.task.IdempotencyKey = &idem
	repo.task.Operation = VideoOperationGenerate
	repo.task.RequestedModel = "grok-imagine-video-1.5"
	repo.task.RequestHash = grokRequestHash([]byte(`{"model":"grok-imagine-video-1.5","prompt":"same"}`))
	var err error
	repo.task.PrivateEnc, err = svc.seal(grokVideoPrivate{ChannelUsageFields: ChannelUsageFields{OriginalModel: "public-video-alias"}})
	require.NoError(t, err)
	repo.task.CreateResponseEnc, err = svc.seal(json.RawMessage(`{"request_id":"original-upstream"}`))
	require.NoError(t, err)
	replay, err := svc.Replay(context.Background(), key, GrokMediaEndpointVideosGenerations, idem, []byte(`{"model":"public-video-alias","prompt":"same"}`))
	require.NoError(t, err)
	require.Equal(t, repo.task.PublicID, replay.PublicID)
	for _, raw := range []string{`{"model":"another-alias","prompt":"same"}`, `{"model":"public-video-alias","prompt":"changed"}`} {
		_, err = svc.Replay(context.Background(), key, GrokMediaEndpointVideosGenerations, idem, []byte(raw))
		require.ErrorIs(t, err, ErrGrokVideoConflict)
	}
}
