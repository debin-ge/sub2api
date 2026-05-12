package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type MiniMaxRemainsFetcher interface {
	FetchRemainsForAccount(ctx context.Context, account *Account) (*MiniMaxTokenPlanRemains, error)
}

type MiniMaxRemainsSyncResult struct {
	AccountID        int64
	Limit            int64
	Remaining        int64
	TargetUsed       int64
	LocalUsed        int64
	SyntheticAdded   int64
	SyntheticRemoved int64
}

type MiniMaxRemainsSyncService struct {
	accountRepo AccountRepository
	fetcher     MiniMaxRemainsFetcher
	quota       *MiniMaxQuotaService
	now         func() time.Time
}

func NewMiniMaxRemainsSyncService(accountRepo AccountRepository, fetcher MiniMaxRemainsFetcher, quota *MiniMaxQuotaService) *MiniMaxRemainsSyncService {
	return &MiniMaxRemainsSyncService{
		accountRepo: accountRepo,
		fetcher:     fetcher,
		quota:       quota,
		now:         time.Now,
	}
}

func (s *MiniMaxRemainsSyncService) SyncAll(ctx context.Context) ([]MiniMaxRemainsSyncResult, error) {
	return s.SyncBatch(ctx, 0)
}

func (s *MiniMaxRemainsSyncService) SyncBatch(ctx context.Context, batchSize int) ([]MiniMaxRemainsSyncResult, error) {
	if s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("minimax remains sync account repo unavailable")
	}
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformMiniMax)
	if err != nil {
		return nil, err
	}
	results := make([]MiniMaxRemainsSyncResult, 0, len(accounts))
	var firstErr error
	processed := 0
	for i := range accounts {
		account := accounts[i]
		if !account.IsMiniMaxTokenPlan() {
			continue
		}
		if batchSize > 0 && processed >= batchSize {
			break
		}
		processed++
		result, err := s.SyncAccount(ctx, &account)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		results = append(results, *result)
	}
	return results, firstErr
}

func (s *MiniMaxRemainsSyncService) SyncAccount(ctx context.Context, account *Account) (*MiniMaxRemainsSyncResult, error) {
	if s == nil || s.fetcher == nil || s.accountRepo == nil || s.quota == nil {
		return nil, fmt.Errorf("minimax remains sync service unavailable")
	}
	if account == nil || !account.IsMiniMaxTokenPlan() {
		return nil, fmt.Errorf("minimax token plan account is required")
	}

	checkedAt := s.now().UTC().Format(time.RFC3339)
	remains, err := s.fetcher.FetchRemainsForAccount(ctx, account)
	if err != nil {
		_ = s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
			"minimax_remains_sync_status": "error",
			"minimax_remains_sync_error":  sanitizeMiniMaxSyncError(err),
			"minimax_remains_checked_at":  checkedAt,
		})
		return nil, err
	}

	targetUsed := remains.Text5hLimit - remains.Text5hRemaining
	if targetUsed < 0 {
		targetUsed = 0
	}
	localUsed, added, removed, err := s.quota.CalibrateTextRequests(ctx, account.ID, targetUsed)
	if err != nil {
		_ = s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
			"minimax_remains_sync_status": "error",
			"minimax_remains_sync_error":  sanitizeMiniMaxSyncError(err),
			"minimax_remains_checked_at":  checkedAt,
		})
		return nil, err
	}

	updates := map[string]any{
		"minimax_text_5h_limit":             remains.Text5hLimit,
		"minimax_text_5h_remaining":         remains.Text5hRemaining,
		"minimax_remains_synced_at":         checkedAt,
		"minimax_remains_checked_at":        checkedAt,
		"minimax_remains_calibrated_at":     checkedAt,
		"minimax_remains_sync_status":       "ok",
		"minimax_remains_sync_error":        "",
		"minimax_remains_raw":               remains.Raw,
		"minimax_remains_local_used":        localUsed,
		"minimax_remains_synthetic_added":   added,
		"minimax_remains_synthetic_removed": removed,
	}
	if remains.Text5hLimit > 0 {
		updates["text_5h_limit"] = remains.Text5hLimit
	}
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, updates); err != nil {
		return nil, err
	}

	return &MiniMaxRemainsSyncResult{
		AccountID:        account.ID,
		Limit:            remains.Text5hLimit,
		Remaining:        remains.Text5hRemaining,
		TargetUsed:       targetUsed,
		LocalUsed:        localUsed,
		SyntheticAdded:   added,
		SyntheticRemoved: removed,
	}, nil
}

func sanitizeMiniMaxSyncError(err error) string {
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "unknown minimax remains sync error"
	}
	return msg
}
