//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type usageBillingOutboxWorkerRepoStub struct {
	mu sync.Mutex

	events      []UsageBillingOutboxEvent
	completeErr error
	completeRes *UsageBillingApplyResult
	completeNil bool
	updateErr   error
	ackErr      error
	completed   []int64
	updated     []int64
	retried     []int64
	acked       []int64
	quarantined []int64
}

type projectionRepairBillingCache struct {
	BillingCache

	balanceInvalidations      []int64
	subscriptionInvalidations [][2]int64
	rateLimitInvalidations    []int64
	platformRefreshes         [][2]any
}

func (c *projectionRepairBillingCache) GetUserPlatformQuotaCache(
	context.Context,
	int64,
	string,
) (*UserPlatformQuotaCacheEntry, bool, error) {
	return nil, false, nil
}

func (c *projectionRepairBillingCache) InvalidateUserBalance(_ context.Context, userID int64) error {
	c.balanceInvalidations = append(c.balanceInvalidations, userID)
	return nil
}

func (c *projectionRepairBillingCache) InvalidateSubscriptionCache(_ context.Context, userID, groupID int64) error {
	c.subscriptionInvalidations = append(c.subscriptionInvalidations, [2]int64{userID, groupID})
	return nil
}

func (c *projectionRepairBillingCache) InvalidateAPIKeyRateLimit(_ context.Context, keyID int64) error {
	c.rateLimitInvalidations = append(c.rateLimitInvalidations, keyID)
	return nil
}

func (c *projectionRepairBillingCache) SetUserPlatformQuotaCache(
	_ context.Context,
	userID int64,
	platform string,
	_ *UserPlatformQuotaCacheEntry,
	_ time.Duration,
) error {
	c.platformRefreshes = append(c.platformRefreshes, [2]any{userID, platform})
	return nil
}

type projectionRepairQuotaRepo struct {
	rec *UserPlatformQuotaRecord
}

func (r *projectionRepairQuotaRepo) GetByUserPlatform(context.Context, int64, string) (*UserPlatformQuotaRecord, error) {
	return r.rec, nil
}

func (*projectionRepairQuotaRepo) BulkInsertInitial(context.Context, []UserPlatformQuotaRecord) error {
	return nil
}

func (*projectionRepairQuotaRepo) IncrementUsageWithReset(context.Context, int64, string, float64, time.Time) error {
	return nil
}

func (*projectionRepairQuotaRepo) ListByUser(context.Context, int64) ([]UserPlatformQuotaRecord, error) {
	return nil, nil
}

func (*projectionRepairQuotaRepo) UpsertForUser(context.Context, int64, []UserPlatformQuotaRecord) error {
	return nil
}

func (*projectionRepairQuotaRepo) ResetExpiredWindow(context.Context, int64, string, string, time.Time) error {
	return nil
}

func (*projectionRepairQuotaRepo) BatchSnapshotUsage(context.Context, []UserPlatformQuotaSnapshot, time.Time) error {
	return nil
}

type projectionRepairAuthInvalidator struct {
	userIDs []int64
}

func (i *projectionRepairAuthInvalidator) InvalidateAuthCacheByUserID(_ context.Context, userID int64) {
	i.userIDs = append(i.userIDs, userID)
}

func (s *usageBillingOutboxWorkerRepoStub) Apply(context.Context, *UsageBillingCommand) (*UsageBillingApplyResult, error) {
	return nil, nil
}

func (s *usageBillingOutboxWorkerRepoStub) ReserveBatchImageBalance(context.Context, *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) {
	return nil, nil
}

func (s *usageBillingOutboxWorkerRepoStub) CaptureBatchImageBalance(context.Context, *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) {
	return nil, nil
}

func (s *usageBillingOutboxWorkerRepoStub) ReleaseBatchImageBalance(context.Context, *BatchImageBalanceHoldCommand) (*BatchImageBalanceHoldResult, error) {
	return nil, nil
}

func (s *usageBillingOutboxWorkerRepoStub) ApplyAndRecord(context.Context, *UsageBillingCommand, *UsageLog) (*UsageBillingApplyResult, error) {
	return nil, nil
}

func (s *usageBillingOutboxWorkerRepoStub) ClaimUsageBillingOutbox(context.Context, string, int, time.Duration) ([]UsageBillingOutboxEvent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := append([]UsageBillingOutboxEvent(nil), s.events...)
	s.events = nil
	return events, nil
}

func (s *usageBillingOutboxWorkerRepoStub) CompleteUsageBillingOutbox(_ context.Context, _ string, event UsageBillingOutboxEvent) (*UsageBillingApplyResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completed = append(s.completed, event.ID)
	if s.completeErr != nil {
		return nil, s.completeErr
	}
	if s.completeNil {
		return nil, nil
	}
	if s.completeRes != nil {
		return s.completeRes, nil
	}
	return &UsageBillingApplyResult{Applied: true, UsageLogRecorded: true}, nil
}

func (s *usageBillingOutboxWorkerRepoStub) UpdateUsageBillingOutboxCommand(_ context.Context, _ string, eventID int64, _ *UsageBillingCommand) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updated = append(s.updated, eventID)
	return s.updateErr
}

func (s *usageBillingOutboxWorkerRepoStub) AcknowledgeUsageBillingOutbox(_ context.Context, _ string, eventID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.acked = append(s.acked, eventID)
	return s.ackErr
}

func (s *usageBillingOutboxWorkerRepoStub) QuarantineUsageBillingOutbox(_ context.Context, _ string, eventID int64, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quarantined = append(s.quarantined, eventID)
	return nil
}

func (s *usageBillingOutboxWorkerRepoStub) RetryUsageBillingOutbox(_ context.Context, _ string, eventID int64, _ time.Time, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retried = append(s.retried, eventID)
	return nil
}

func TestUsageBillingOutboxWorker_ReleasesFailedClaimForDurableRetry(t *testing.T) {
	repo := &usageBillingOutboxWorkerRepoStub{
		events: []UsageBillingOutboxEvent{{
			ID:       41,
			Attempts: 2,
			Command:  &UsageBillingCommand{RequestID: "req-41", APIKeyID: 7},
			UsageLog: &UsageLog{RequestID: "req-41", APIKeyID: 7},
		}},
		completeErr: errors.New("injected transaction failure"),
	}
	worker := NewUsageBillingOutboxWorker(repo, &UsageBillingPostEffectsService{})

	require.NoError(t, worker.processBatch(context.Background()))

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []int64{41}, repo.completed)
	require.Equal(t, []int64{41}, repo.retried)
	require.Equal(t, uint64(1), worker.failures.Load())
}

func TestUsageBillingOutboxWorker_QuarantinesPermanentConflict(t *testing.T) {
	repo := &usageBillingOutboxWorkerRepoStub{
		events: []UsageBillingOutboxEvent{{
			ID:       42,
			Command:  &UsageBillingCommand{RequestID: "req-conflict", APIKeyID: 7},
			UsageLog: &UsageLog{RequestID: "req-conflict", APIKeyID: 7},
		}},
		completeErr: ErrUsageBillingRequestConflict,
	}
	worker := NewUsageBillingOutboxWorker(repo, &UsageBillingPostEffectsService{})

	require.NoError(t, worker.processBatch(context.Background()))

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []int64{42}, repo.completed)
	require.Equal(t, []int64{42}, repo.quarantined)
	require.Empty(t, repo.retried)
	require.Empty(t, repo.acked)
	require.Equal(t, uint64(1), worker.failures.Load())
}

func TestUsageBillingOutboxWorker_NilPostEffectsRetriesWithoutAck(t *testing.T) {
	repo := &usageBillingOutboxWorkerRepoStub{
		events: []UsageBillingOutboxEvent{{
			ID:       43,
			Command:  &UsageBillingCommand{RequestID: "req-no-finalizer", APIKeyID: 7},
			UsageLog: &UsageLog{RequestID: "req-no-finalizer", APIKeyID: 7},
		}},
	}
	worker := NewUsageBillingOutboxWorker(repo, nil)

	require.NoError(t, worker.processBatch(context.Background()))

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []int64{43}, repo.completed)
	require.Equal(t, []int64{43}, repo.retried)
	require.Empty(t, repo.acked)
	require.Empty(t, repo.quarantined)
}

func TestUsageBillingOutboxWorker_MissingRequiredProjectionDependencyRetriesWithoutAck(t *testing.T) {
	repo := &usageBillingOutboxWorkerRepoStub{
		events: []UsageBillingOutboxEvent{{
			ID: 431,
			Command: &UsageBillingCommand{
				RequestID:   "req-no-cache-projection",
				APIKeyID:    7,
				UserID:      9,
				BalanceCost: 1.25,
			},
			UsageLog: &UsageLog{RequestID: "req-no-cache-projection", APIKeyID: 7},
		}},
	}
	worker := NewUsageBillingOutboxWorker(repo, &UsageBillingPostEffectsService{})

	require.NoError(t, worker.processBatch(context.Background()))

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []int64{431}, repo.completed)
	require.Equal(t, []int64{431}, repo.retried)
	require.Empty(t, repo.acked)
	require.Empty(t, repo.quarantined)
}

func TestUsageBillingOutboxWorker_MissingRequiredAuthInvalidatorRetriesWithoutAck(t *testing.T) {
	repo := &usageBillingOutboxWorkerRepoStub{
		events: []UsageBillingOutboxEvent{{
			ID:       432,
			Command:  &UsageBillingCommand{RequestID: "req-no-auth-invalidator", APIKeyID: 7, UserID: 9},
			UsageLog: &UsageLog{RequestID: "req-no-auth-invalidator", APIKeyID: 7},
		}},
		completeRes: &UsageBillingApplyResult{
			Applied:              true,
			UsageLogRecorded:     true,
			APIKeyQuotaExhausted: true,
		},
	}
	worker := NewUsageBillingOutboxWorker(repo, &UsageBillingPostEffectsService{})

	require.NoError(t, worker.processBatch(context.Background()))

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []int64{432}, repo.completed)
	require.Equal(t, []int64{432}, repo.retried)
	require.Empty(t, repo.acked)
	require.Empty(t, repo.quarantined)
}

func TestUsageBillingOutboxWorker_NilCompleteResultRetriesWithoutFinalizeOrAck(t *testing.T) {
	repo := &usageBillingOutboxWorkerRepoStub{
		events: []UsageBillingOutboxEvent{{
			ID:       46,
			Command:  &UsageBillingCommand{RequestID: "req-nil-result", APIKeyID: 7},
			UsageLog: &UsageLog{RequestID: "req-nil-result", APIKeyID: 7},
		}},
		completeNil: true,
	}
	worker := NewUsageBillingOutboxWorker(repo, &UsageBillingPostEffectsService{})

	require.NoError(t, worker.processBatch(context.Background()))

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []int64{46}, repo.completed)
	require.Equal(t, []int64{46}, repo.retried)
	require.Empty(t, repo.acked)
	require.Empty(t, repo.quarantined)
	require.Equal(t, uint64(1), worker.failures.Load())
}

func TestUsageBillingOutboxWorker_MissingPayloadIsQuarantinedWithoutCompleteOrAck(t *testing.T) {
	tests := []struct {
		name  string
		event UsageBillingOutboxEvent
	}{
		{
			name: "nil command",
			event: UsageBillingOutboxEvent{
				ID:       47,
				UsageLog: &UsageLog{RequestID: "req-nil-command", APIKeyID: 7},
			},
		},
		{
			name: "nil usage log",
			event: UsageBillingOutboxEvent{
				ID:      48,
				Command: &UsageBillingCommand{RequestID: "req-nil-log", APIKeyID: 7},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &usageBillingOutboxWorkerRepoStub{
				events: []UsageBillingOutboxEvent{tt.event},
			}
			worker := NewUsageBillingOutboxWorker(repo, &UsageBillingPostEffectsService{})

			require.NoError(t, worker.processBatch(context.Background()))

			repo.mu.Lock()
			defer repo.mu.Unlock()
			require.Empty(t, repo.completed)
			require.Empty(t, repo.retried)
			require.Empty(t, repo.acked)
			require.Equal(t, []int64{tt.event.ID}, repo.quarantined)
			require.Equal(t, uint64(1), worker.failures.Load())
		})
	}
}

func TestCaptureUsageBillingPlatformQuotaSnapshot_TypedNilDependenciesDoNotPanic(t *testing.T) {
	var typedNilQuotaRepo *projectionRepairQuotaRepo
	var typedNilCache *projectionRepairBillingCache

	t.Run("typed nil quota repository", func(t *testing.T) {
		require.NotPanics(t, func() {
			snapshot, snapshotNeeded, track := captureUsageBillingPlatformQuotaSnapshot(
				context.Background(),
				42,
				"openai",
				&billingDeps{userPlatformQuotaRepo: typedNilQuotaRepo},
			)
			require.Nil(t, snapshot)
			require.False(t, snapshotNeeded)
			require.False(t, track)
		})
	})

	t.Run("typed nil billing cache", func(t *testing.T) {
		require.NotPanics(t, func() {
			snapshot, snapshotNeeded, track := captureUsageBillingPlatformQuotaSnapshot(
				context.Background(),
				42,
				"openai",
				&billingDeps{
					billingCacheService: &BillingCacheService{cache: typedNilCache},
					userPlatformQuotaRepo: &projectionRepairQuotaRepo{
						rec: nil,
					},
				},
			)
			require.Nil(t, snapshot)
			require.False(t, snapshotNeeded)
			require.False(t, track)
		})
	})
}

func TestUsageBillingOutboxWorker_FinalizesThenAcknowledges(t *testing.T) {
	repo := &usageBillingOutboxWorkerRepoStub{
		events: []UsageBillingOutboxEvent{{
			ID:       44,
			Command:  &UsageBillingCommand{RequestID: "req-success", APIKeyID: 7},
			UsageLog: &UsageLog{RequestID: "req-success", APIKeyID: 7},
		}},
	}
	worker := NewUsageBillingOutboxWorker(repo, &UsageBillingPostEffectsService{})

	require.NoError(t, worker.processBatch(context.Background()))

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []int64{44}, repo.completed)
	require.Equal(t, []int64{44}, repo.acked)
	require.Empty(t, repo.retried)
	require.Empty(t, repo.quarantined)
	require.Equal(t, uint64(1), worker.processed.Load())
}

func TestUsageBillingOutboxWorker_AcknowledgeFailureKeepsIntentRetryable(t *testing.T) {
	repo := &usageBillingOutboxWorkerRepoStub{
		events: []UsageBillingOutboxEvent{{
			ID:       45,
			Command:  &UsageBillingCommand{RequestID: "req-ack-failure", APIKeyID: 7},
			UsageLog: &UsageLog{RequestID: "req-ack-failure", APIKeyID: 7},
		}},
		ackErr: errors.New("injected acknowledge failure"),
	}
	worker := NewUsageBillingOutboxWorker(repo, &UsageBillingPostEffectsService{})

	require.NoError(t, worker.processBatch(context.Background()))

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []int64{45}, repo.completed)
	require.Equal(t, []int64{45}, repo.acked)
	require.Equal(t, []int64{45}, repo.retried)
	require.Empty(t, repo.quarantined)
	require.Zero(t, worker.processed.Load())
}

func TestUsageBillingOutboxWorker_RestartRepairsLegacyProjectionsWithoutLastUsedOrNotifications(t *testing.T) {
	groupID := int64(13)
	command := &UsageBillingCommand{
		RequestID:           "req-legacy-projection-repair",
		APIKeyID:            7,
		UserID:              9,
		AccountID:           11,
		GroupID:             &groupID,
		Platform:            "anthropic",
		BalanceCost:         1.25,
		APIKeyQuotaCost:     1.25,
		APIKeyRateLimitCost: 1.25,
		PlatformQuotaCost:   1.25,
	}
	repo := &usageBillingOutboxWorkerRepoStub{
		events: []UsageBillingOutboxEvent{{
			ID:       46,
			Stage:    1,
			Command:  command,
			UsageLog: &UsageLog{RequestID: command.RequestID, APIKeyID: command.APIKeyID},
		}},
		completeRes: &UsageBillingApplyResult{
			Applied:                  false,
			UsageLogRecorded:         true,
			ProjectionRepairRequired: true,
		},
	}
	cache := &projectionRepairBillingCache{}
	cfg := &config.Config{}
	cfg.Billing.UserPlatformQuotaCacheTTLSeconds = 60
	quotaRepo := &projectionRepairQuotaRepo{rec: &UserPlatformQuotaRecord{
		UserID:          command.UserID,
		Platform:        command.Platform,
		DailyUsageUSD:   3,
		WeeklyUsageUSD:  4,
		MonthlyUsageUSD: 5,
	}}
	billingCache := &BillingCacheService{
		cache:                 cache,
		userPlatformQuotaRepo: quotaRepo,
		cfg:                   cfg,
	}
	deferred := &DeferredService{}
	auth := &projectionRepairAuthInvalidator{}
	postEffects := &UsageBillingPostEffectsService{
		billingCacheService:  billingCache,
		deferredService:      deferred,
		balanceNotifyService: &BalanceNotifyService{},
		authCacheInvalidator: auth,
	}
	worker := NewUsageBillingOutboxWorker(repo, postEffects)

	require.NoError(t, worker.processBatch(context.Background()))

	repo.mu.Lock()
	require.Equal(t, []int64{46}, repo.completed)
	require.Equal(t, []int64{46}, repo.acked)
	require.Empty(t, repo.retried)
	repo.mu.Unlock()
	require.Equal(t, []int64{command.UserID}, cache.balanceInvalidations)
	require.Equal(t, []int64{command.APIKeyID}, cache.rateLimitInvalidations)
	require.Equal(t, [][2]any{{command.UserID, command.Platform}}, cache.platformRefreshes)
	require.Equal(t, []int64{command.UserID}, auth.userIDs)
	_, lastUsedScheduled := deferred.lastUsedUpdates.Load(command.AccountID)
	require.False(t, lastUsedScheduled, "projection-only recovery must not repeat last-used")
	require.Equal(t, uint64(1), worker.processed.Load())
}

func TestUsageBillingOutboxWorker_PreparesAndPersistsMissingPlatformSnapshot(t *testing.T) {
	windowStart := time.Date(2026, time.July, 29, 0, 0, 0, 0, time.UTC)
	command := &UsageBillingCommand{
		RequestID:                   "req-platform-snapshot",
		APIKeyID:                    7,
		UserID:                      9,
		Platform:                    "anthropic",
		PlatformQuotaCost:           1.25,
		PlatformQuotaSnapshotNeeded: true,
		OccurredAt:                  windowStart,
	}
	repo := &usageBillingOutboxWorkerRepoStub{
		events: []UsageBillingOutboxEvent{{
			ID:       46,
			Command:  command,
			UsageLog: &UsageLog{RequestID: command.RequestID, APIKeyID: command.APIKeyID},
		}},
	}
	quotaRepo := &fakeQuotaRepo{rec: &UserPlatformQuotaRecord{
		UserID:             command.UserID,
		Platform:           command.Platform,
		DailyUsageUSD:      2,
		WeeklyUsageUSD:     3,
		MonthlyUsageUSD:    4,
		DailyWindowStart:   &windowStart,
		WeeklyWindowStart:  &windowStart,
		MonthlyWindowStart: &windowStart,
	}}
	cfg := &config.Config{}
	cfg.Billing.UserPlatformQuotaCacheTTLSeconds = 60
	postEffects := &UsageBillingPostEffectsService{
		userPlatformQuotaRepo: quotaRepo,
		billingCacheService: &BillingCacheService{
			cache:                 &projectionRepairBillingCache{},
			userPlatformQuotaRepo: quotaRepo,
			cfg:                   cfg,
		},
	}
	worker := NewUsageBillingOutboxWorker(repo, postEffects)

	require.NoError(t, worker.processBatch(context.Background()))

	repo.mu.Lock()
	defer repo.mu.Unlock()
	require.Equal(t, []int64{46}, repo.updated)
	require.Equal(t, []int64{46}, repo.completed)
	require.Equal(t, []int64{46}, repo.acked)
	require.Empty(t, repo.retried)
	require.False(t, command.PlatformQuotaSnapshotNeeded)
	require.NotNil(t, command.PlatformQuotaSnapshot)
	require.InDelta(t, 4, command.PlatformQuotaSnapshot.MonthlyUsageUSD, 1e-9)
}

func TestUsageBillingOutboxRetryDelay_IsExponentiallyBounded(t *testing.T) {
	for _, attempt := range []int{-1, 0, 1, 2, 9, 99} {
		delay := usageBillingOutboxRetryDelay(attempt)
		require.Positive(t, delay)
		require.LessOrEqual(t, delay, 5*time.Minute)
	}
}

func TestBoundedUsageBillingOutboxError_IsPostgresSafeUTF8(t *testing.T) {
	message := strings.Repeat("界", 341) + "\xff\x00tail"

	got := boundedUsageBillingOutboxError(errors.New(message))

	require.LessOrEqual(t, len(got), 1024)
	require.True(t, utf8.ValidString(got))
	require.NotContains(t, got, "\x00")
	require.Equal(t, "界", string([]rune(got)[len([]rune(got))-1]))
}

func TestUsageBillingOutboxWorker_APIKeyNotFoundIsNotPermanentlyQuarantined(t *testing.T) {
	require.False(t, isPermanentUsageBillingWorkerError(ErrAPIKeyNotFound))
}
