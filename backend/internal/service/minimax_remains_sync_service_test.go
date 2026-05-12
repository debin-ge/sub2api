package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

type minimaxRemainsSyncRepoStub struct {
	AccountRepository

	accounts      []Account
	updateID      int64
	updatePayload map[string]any
	updateErr     error
}

func (r *minimaxRemainsSyncRepoStub) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	var out []Account
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out, nil
}

func (r *minimaxRemainsSyncRepoStub) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	r.updateID = id
	r.updatePayload = updates
	return r.updateErr
}

type minimaxRemainsFetcherStub struct {
	remains *MiniMaxTokenPlanRemains
	err     error
}

func (f minimaxRemainsFetcherStub) FetchRemainsForAccount(ctx context.Context, account *Account) (*MiniMaxTokenPlanRemains, error) {
	return f.remains, f.err
}

func TestMiniMaxRemainsSyncServiceSyncAccountUpdatesExtraAndCalibrates(t *testing.T) {
	cache := &minimaxQuotaCacheStub{
		calibrateLocalUsed: 1300,
		calibrateAdded:     300,
		calibrateRemoved:   0,
	}
	repo := &minimaxRemainsSyncRepoStub{}
	svc := NewMiniMaxRemainsSyncService(repo, minimaxRemainsFetcherStub{
		remains: &MiniMaxTokenPlanRemains{
			Text5hLimit:     4500,
			Text5hRemaining: 3200,
			Raw:             map[string]any{"ok": true},
		},
	}, NewMiniMaxQuotaService(cache, nil))
	svc.now = func() time.Time { return time.Date(2026, 5, 12, 3, 4, 5, 0, time.UTC) }

	result, err := svc.SyncAccount(context.Background(), &Account{
		ID:          101,
		Platform:    PlatformMiniMax,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-cp-test"},
	})
	if err != nil {
		t.Fatalf("SyncAccount error = %v", err)
	}
	if result.TargetUsed != 1300 || result.LocalUsed != 1300 || result.SyntheticAdded != 300 || result.SyntheticRemoved != 0 {
		t.Fatalf("result = %+v", result)
	}
	if cache.calibrateCalls != 1 || cache.calibrateAccountID != 101 || cache.calibrateTarget != 1300 || cache.calibrateWindow != MiniMaxTokenPlanTextWindowSeconds {
		t.Fatalf("calibrate call = %+v", cache)
	}
	if repo.updateID != 101 {
		t.Fatalf("updateID = %d", repo.updateID)
	}
	assertExtraValue(t, repo.updatePayload, "minimax_text_5h_limit", int64(4500))
	assertExtraValue(t, repo.updatePayload, "minimax_text_5h_remaining", int64(3200))
	assertExtraValue(t, repo.updatePayload, "text_5h_limit", int64(4500))
	assertExtraValue(t, repo.updatePayload, "minimax_remains_sync_status", "ok")
	assertExtraValue(t, repo.updatePayload, "minimax_remains_synced_at", "2026-05-12T03:04:05Z")
	assertExtraValue(t, repo.updatePayload, "minimax_remains_calibrated_at", "2026-05-12T03:04:05Z")
	assertExtraValue(t, repo.updatePayload, "minimax_remains_local_used", int64(1300))
	assertExtraValue(t, repo.updatePayload, "minimax_remains_synthetic_added", int64(300))
	assertExtraValue(t, repo.updatePayload, "minimax_remains_synthetic_removed", int64(0))
}

func TestMiniMaxRemainsSyncServiceSyncAccountStoresErrorStatus(t *testing.T) {
	repo := &minimaxRemainsSyncRepoStub{}
	svc := NewMiniMaxRemainsSyncService(repo, minimaxRemainsFetcherStub{err: errors.New("upstream down")}, NewMiniMaxQuotaService(&minimaxQuotaCacheStub{}, nil))
	svc.now = func() time.Time { return time.Date(2026, 5, 12, 3, 4, 5, 0, time.UTC) }

	_, err := svc.SyncAccount(context.Background(), &Account{
		ID:          101,
		Platform:    PlatformMiniMax,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-cp-test"},
	})
	if err == nil {
		t.Fatalf("expected sync error")
	}
	assertExtraValue(t, repo.updatePayload, "minimax_remains_sync_status", "error")
	assertExtraValue(t, repo.updatePayload, "minimax_remains_sync_error", "upstream down")
	assertExtraValue(t, repo.updatePayload, "minimax_remains_checked_at", "2026-05-12T03:04:05Z")
}

func assertExtraValue(t *testing.T, payload map[string]any, key string, want any) {
	t.Helper()
	got, ok := payload[key]
	if !ok {
		t.Fatalf("payload missing key %q: %#v", key, payload)
	}
	if got != want {
		t.Fatalf("payload[%q] = %#v, want %#v", key, got, want)
	}
}
