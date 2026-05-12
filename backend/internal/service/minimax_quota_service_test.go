package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

type minimaxQuotaCacheStub struct {
	allowed bool
	used    int64
	err     error

	reserveCalls int
	accountID    int64
	requestID    string
	limit        int64
	window       int64

	rollbackCalls     int
	rollbackAccountID int64
	rollbackRequestID string
	rollbackErr       error

	calibrateCalls     int
	calibrateAccountID int64
	calibrateTarget    int64
	calibrateWindow    int64
	calibrateLocalUsed int64
	calibrateAdded     int64
	calibrateRemoved   int64
	calibrateErr       error
}

func (s *minimaxQuotaCacheStub) ReserveTextRequest(ctx context.Context, accountID int64, requestID string, limit int64, windowSeconds int64) (bool, int64, error) {
	s.reserveCalls++
	s.accountID = accountID
	s.requestID = requestID
	s.limit = limit
	s.window = windowSeconds
	return s.allowed, s.used, s.err
}

func (s *minimaxQuotaCacheStub) RollbackTextRequest(ctx context.Context, accountID int64, requestID string) error {
	s.rollbackCalls++
	s.rollbackAccountID = accountID
	s.rollbackRequestID = requestID
	return s.rollbackErr
}

func (s *minimaxQuotaCacheStub) CountTextRequests(ctx context.Context, accountID int64, windowSeconds int64) (int64, error) {
	return s.used, s.err
}

func (s *minimaxQuotaCacheStub) CalibrateTextRequests(ctx context.Context, accountID int64, targetUsed int64, windowSeconds int64) (localUsed int64, syntheticAdded int64, syntheticRemoved int64, err error) {
	s.calibrateCalls++
	s.calibrateAccountID = accountID
	s.calibrateTarget = targetUsed
	s.calibrateWindow = windowSeconds
	return s.calibrateLocalUsed, s.calibrateAdded, s.calibrateRemoved, s.calibrateErr
}

func TestMiniMaxQuotaServiceReserveAllowsWithinLimit(t *testing.T) {
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxQuotaService(cache, nil)

	decision, err := svc.ReserveTextRequest(context.Background(), &Account{
		ID:       101,
		Platform: PlatformMiniMax,
		Extra: map[string]any{
			"text_5h_limit": float64(2),
		},
	}, " req-1 ")

	if err != nil {
		t.Fatalf("ReserveTextRequest error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected allowed decision, got %+v", decision)
	}
	if decision.Used != 1 || decision.Limit != 2 || decision.Reason != "" {
		t.Fatalf("decision = %+v", decision)
	}
	if cache.reserveCalls != 1 || cache.accountID != 101 || cache.requestID != "req-1" {
		t.Fatalf("cache reserve call = calls %d accountID %d requestID %q", cache.reserveCalls, cache.accountID, cache.requestID)
	}
	if cache.limit != 2 || cache.window != MiniMaxTokenPlanTextWindowSeconds {
		t.Fatalf("cache limit/window = %d/%d", cache.limit, cache.window)
	}
}

func TestMiniMaxQuotaServiceReserveBlocksWhenLimitReached(t *testing.T) {
	svc := NewMiniMaxQuotaService(&minimaxQuotaCacheStub{allowed: false, used: 2}, nil)

	decision, err := svc.ReserveTextRequest(context.Background(), &Account{
		ID:       101,
		Platform: PlatformMiniMax,
		Extra: map[string]any{
			"text_5h_limit": float64(2),
		},
	}, "req-3")

	if err != nil {
		t.Fatalf("ReserveTextRequest error = %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected blocked decision")
	}
	if decision.Used != 2 || decision.Limit != 2 || decision.Reason != "quota_exhausted" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestMiniMaxQuotaServiceReserveBlocksWhenOfficialRemainsFreshAndEmpty(t *testing.T) {
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxQuotaService(cache, nil)

	decision, err := svc.ReserveTextRequest(context.Background(), &Account{
		ID:       101,
		Platform: PlatformMiniMax,
		Extra: map[string]any{
			"text_5h_limit":                   float64(2),
			"minimax_text_5h_remaining":       float64(0),
			"minimax_remains_synced_at":       time.Now().UTC().Format(time.RFC3339),
			"minimax_remains_sync_status":     "ok",
			"minimax_remains_calibrated_at":   time.Now().UTC().Format(time.RFC3339),
			"minimax_remains_local_used":      float64(2),
			"minimax_remains_synthetic_added": float64(1),
		},
	}, "req-1")

	if err != nil {
		t.Fatalf("ReserveTextRequest error = %v", err)
	}
	if decision.Allowed || decision.Reason != "official_remains_exhausted" {
		t.Fatalf("decision = %+v", decision)
	}
	if cache.reserveCalls != 0 {
		t.Fatalf("expected no reserve when official remains is exhausted, got %d", cache.reserveCalls)
	}
}

func TestMiniMaxQuotaServiceReserveIgnoresStaleOfficialRemains(t *testing.T) {
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxQuotaService(cache, nil)

	decision, err := svc.ReserveTextRequest(context.Background(), &Account{
		ID:       101,
		Platform: PlatformMiniMax,
		Extra: map[string]any{
			"text_5h_limit":             float64(2),
			"minimax_text_5h_remaining": float64(0),
			"minimax_remains_synced_at": time.Now().Add(-time.Hour).UTC().Format(time.RFC3339),
		},
	}, "req-1")

	if err != nil {
		t.Fatalf("ReserveTextRequest error = %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected stale official remains to be ignored, got %+v", decision)
	}
	if cache.reserveCalls != 1 {
		t.Fatalf("expected reserve call, got %d", cache.reserveCalls)
	}
}

func TestMiniMaxQuotaServiceReserveFailsClosedOnCacheError(t *testing.T) {
	svc := NewMiniMaxQuotaService(&minimaxQuotaCacheStub{used: 1, err: errors.New("redis down")}, nil)

	decision, err := svc.ReserveTextRequest(context.Background(), &Account{
		ID:       101,
		Platform: PlatformMiniMax,
		Extra: map[string]any{
			"text_5h_limit": float64(2),
		},
	}, "req-1")

	if err == nil {
		t.Fatalf("expected cache error")
	}
	if decision.Allowed {
		t.Fatalf("expected fail-closed decision")
	}
	if decision.Used != 1 || decision.Limit != 2 || decision.Reason != "quota_cache_error" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestMiniMaxQuotaServiceReserveRejectsInvalidAccount(t *testing.T) {
	tests := []struct {
		name    string
		account *Account
	}{
		{
			name: "wrong_platform",
			account: &Account{
				ID:       101,
				Platform: PlatformOpenAI,
			},
		},
		{
			name: "zero_account_id",
			account: &Account{
				ID:       0,
				Platform: PlatformMiniMax,
			},
		},
		{
			name: "negative_account_id",
			account: &Account{
				ID:       -1,
				Platform: PlatformMiniMax,
			},
		},
		{
			name:    "nil_account",
			account: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &minimaxQuotaCacheStub{allowed: true}
			svc := NewMiniMaxQuotaService(cache, nil)

			decision, err := svc.ReserveTextRequest(context.Background(), tt.account, "req-1")
			if err == nil {
				t.Fatalf("expected invalid account error")
			}
			if decision.Allowed || decision.Reason != "invalid_minimax_account" {
				t.Fatalf("decision = %+v", decision)
			}
			if cache.reserveCalls != 0 {
				t.Fatalf("expected no cache call, got %d", cache.reserveCalls)
			}
		})
	}
}

func TestMiniMaxQuotaServiceReserveFailsClosedWithNilCache(t *testing.T) {
	svc := NewMiniMaxQuotaService(nil, nil)

	decision, err := svc.ReserveTextRequest(context.Background(), &Account{
		ID:       101,
		Platform: PlatformMiniMax,
	}, "req-1")

	if err == nil {
		t.Fatalf("expected nil cache error")
	}
	if decision.Allowed || decision.Reason != "quota_cache_unavailable" {
		t.Fatalf("decision = %+v", decision)
	}
}

func TestMiniMaxQuotaServiceReserveRejectsBlankRequestID(t *testing.T) {
	cache := &minimaxQuotaCacheStub{allowed: true}
	svc := NewMiniMaxQuotaService(cache, nil)

	decision, err := svc.ReserveTextRequest(context.Background(), &Account{
		ID:       101,
		Platform: PlatformMiniMax,
	}, " \t\n ")

	if err == nil {
		t.Fatalf("expected blank request id error")
	}
	if decision.Allowed || decision.Reason != "request_id_required" {
		t.Fatalf("decision = %+v", decision)
	}
	if cache.reserveCalls != 0 {
		t.Fatalf("expected no cache call, got %d", cache.reserveCalls)
	}
}

func TestMiniMaxQuotaServiceReserveUsesDefaultTextLimit(t *testing.T) {
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxQuotaService(cache, nil)

	decision, err := svc.ReserveTextRequest(context.Background(), &Account{
		ID:       101,
		Platform: PlatformMiniMax,
	}, "req-1")

	if err != nil {
		t.Fatalf("ReserveTextRequest error = %v", err)
	}
	if !decision.Allowed || decision.Limit != MiniMaxTokenPlanDefaultText5hLimit {
		t.Fatalf("decision = %+v", decision)
	}
	if cache.limit != MiniMaxTokenPlanDefaultText5hLimit {
		t.Fatalf("cache limit = %d", cache.limit)
	}
}

func TestMiniMaxQuotaServiceReserveRejectsInvalidTextLimit(t *testing.T) {
	tests := []struct {
		name  string
		value any
	}{
		{name: "nil_value", value: nil},
		{name: "bad_string", value: "bad"},
		{name: "zero_string", value: "0"},
		{name: "negative_string", value: "-1"},
		{name: "zero_int", value: 0},
		{name: "negative_int", value: -1},
		{name: "fractional_float", value: 1.5},
		{name: "over_int64_float", value: miniMaxQuotaMaxInt64ExclusiveFloat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
			svc := NewMiniMaxQuotaService(cache, nil)

			decision, err := svc.ReserveTextRequest(context.Background(), &Account{
				ID:       101,
				Platform: PlatformMiniMax,
				Extra: map[string]any{
					"text_5h_limit": tt.value,
				},
			}, "req-1")

			if err == nil {
				t.Fatalf("expected invalid limit error")
			}
			if decision.Allowed || decision.Reason != "invalid_quota_limit" {
				t.Fatalf("decision = %+v", decision)
			}
			if cache.reserveCalls != 0 {
				t.Fatalf("expected no cache call, got %d", cache.reserveCalls)
			}
		})
	}
}

func TestMiniMaxQuotaServiceReserveReadsTextLimitFromAccountExtra(t *testing.T) {
	cache := &minimaxQuotaCacheStub{allowed: true, used: 1}
	svc := NewMiniMaxQuotaService(cache, nil)

	decision, err := svc.ReserveTextRequest(context.Background(), &Account{
		ID:       101,
		Platform: PlatformMiniMax,
		Extra: map[string]any{
			"text_5h_limit": "123",
		},
	}, "req-1")

	if err != nil {
		t.Fatalf("ReserveTextRequest error = %v", err)
	}
	if !decision.Allowed || decision.Limit != 123 {
		t.Fatalf("decision = %+v", decision)
	}
	if cache.limit != 123 {
		t.Fatalf("cache limit = %d", cache.limit)
	}
}

func TestMiniMaxQuotaServiceRollbackCallsCache(t *testing.T) {
	cache := &minimaxQuotaCacheStub{}
	svc := NewMiniMaxQuotaService(cache, nil)

	if err := svc.RollbackTextRequest(context.Background(), 101, " req-1 "); err != nil {
		t.Fatalf("RollbackTextRequest error = %v", err)
	}
	if cache.rollbackCalls != 1 || cache.rollbackAccountID != 101 || cache.rollbackRequestID != "req-1" {
		t.Fatalf("rollback call = calls %d accountID %d requestID %q", cache.rollbackCalls, cache.rollbackAccountID, cache.rollbackRequestID)
	}
}

func TestMiniMaxQuotaServiceRollbackNoopsWithoutCacheOrRequestID(t *testing.T) {
	if err := (*MiniMaxQuotaService)(nil).RollbackTextRequest(context.Background(), 101, "req-1"); err != nil {
		t.Fatalf("nil service rollback error = %v", err)
	}
	if err := NewMiniMaxQuotaService(nil, nil).RollbackTextRequest(context.Background(), 101, "req-1"); err != nil {
		t.Fatalf("nil cache rollback error = %v", err)
	}

	cache := &minimaxQuotaCacheStub{}
	svc := NewMiniMaxQuotaService(cache, nil)
	if err := svc.RollbackTextRequest(context.Background(), 101, " \t\n "); err != nil {
		t.Fatalf("blank request id rollback error = %v", err)
	}
	if cache.rollbackCalls != 0 {
		t.Fatalf("expected no rollback call, got %d", cache.rollbackCalls)
	}
}

func TestMiniMaxQuotaServiceRollbackPropagatesCacheError(t *testing.T) {
	cache := &minimaxQuotaCacheStub{rollbackErr: errors.New("redis down")}
	svc := NewMiniMaxQuotaService(cache, nil)

	if err := svc.RollbackTextRequest(context.Background(), 101, "req-1"); err == nil {
		t.Fatalf("expected rollback cache error")
	}
}
