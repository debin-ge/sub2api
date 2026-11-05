package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type usageBillingRepository struct {
	db *sql.DB
}

func NewUsageBillingRepository(_ *dbent.Client, sqlDB *sql.DB) *usageBillingRepository {
	return &usageBillingRepository{db: sqlDB}
}

func (r *usageBillingRepository) Apply(ctx context.Context, cmd *service.UsageBillingCommand) (_ *service.UsageBillingApplyResult, err error) {
	if cmd == nil {
		return &service.UsageBillingApplyResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	if cmd.QuotaTime != nil {
		return nil, fmt.Errorf("%w: video quota time requires hold-backed settlement", service.ErrUsageBillingPayloadInvalid)
	}

	cmd.Normalize()
	if cmd.RequestID == "" {
		return nil, service.ErrUsageBillingRequestIDRequired
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	applied, err := r.claimUsageBillingKey(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.UsageBillingApplyResult{Applied: false}, nil
	}

	result := &service.UsageBillingApplyResult{Applied: true}
	if err := r.applyUsageBillingEffects(ctx, tx, cmd, result); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) claimUsageBillingKey(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (bool, error) {
	return r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
}

func (r *usageBillingRepository) claimUsageBillingRequest(ctx context.Context, tx *sql.Tx, requestID string, apiKeyID int64, requestFingerprint string) (bool, error) {
	var id int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO usage_billing_dedup (request_id, api_key_id, request_fingerprint)
		VALUES ($1, $2, $3)
		ON CONFLICT (request_id, api_key_id) DO NOTHING
		RETURNING id
	`, requestID, apiKeyID, requestFingerprint).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		var existingFingerprint string
		if err := tx.QueryRowContext(ctx, `
			SELECT request_fingerprint
			FROM usage_billing_dedup
			WHERE request_id = $1 AND api_key_id = $2
		`, requestID, apiKeyID).Scan(&existingFingerprint); err != nil {
			return false, err
		}
		if strings.TrimSpace(existingFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var archivedFingerprint string
	err = tx.QueryRowContext(ctx, `
		SELECT request_fingerprint
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, requestID, apiKeyID).Scan(&archivedFingerprint)
	if err == nil {
		if strings.TrimSpace(archivedFingerprint) != strings.TrimSpace(requestFingerprint) {
			return false, service.ErrUsageBillingRequestConflict
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	return true, nil
}

func (r *usageBillingRepository) ReserveBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	generic := balanceHoldCommandFromBatchImage(cmd)
	result, err := r.ReserveBalanceHold(ctx, generic)
	return batchImageBalanceHoldResult(result), translateBatchImageBalanceHoldError(err)
}

func (r *usageBillingRepository) CaptureBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	generic := balanceHoldCommandFromBatchImage(cmd)
	result, err := r.CaptureBalanceHold(ctx, generic)
	return batchImageBalanceHoldResult(result), translateBatchImageBalanceHoldError(err)
}

func (r *usageBillingRepository) ReleaseBatchImageBalance(ctx context.Context, cmd *service.BatchImageBalanceHoldCommand) (*service.BatchImageBalanceHoldResult, error) {
	generic := balanceHoldCommandFromBatchImage(cmd)
	result, err := r.ReleaseBalanceHold(ctx, generic)
	return batchImageBalanceHoldResult(result), translateBatchImageBalanceHoldError(err)
}

func balanceHoldCommandFromBatchImage(cmd *service.BatchImageBalanceHoldCommand) *service.BalanceHoldCommand {
	if cmd == nil {
		return nil
	}
	// Normalize here, before entering the generic path, to preserve the legacy
	// Batch Image fingerprint byte-for-byte.
	cmd.Normalize()
	return &service.BalanceHoldCommand{
		RequestID:          cmd.RequestID,
		APIKeyID:           cmd.APIKeyID,
		RequestFingerprint: cmd.RequestFingerprint,
		RequestPayloadHash: cmd.RequestPayloadHash,
		UserID:             cmd.UserID,
		Scope:              service.BalanceHoldScopeBatchImage,
		RefID:              cmd.BatchID,
		HoldAmount:         cmd.HoldAmount,
		ActualAmount:       cmd.ActualAmount,
	}
}

func batchImageBalanceHoldResult(result *service.BalanceHoldResult) *service.BatchImageBalanceHoldResult {
	if result == nil {
		return nil
	}
	return &service.BatchImageBalanceHoldResult{
		Applied:       result.Applied,
		NewBalance:    result.NewBalance,
		FrozenBalance: result.FrozenBalance,
	}
}

func translateBatchImageBalanceHoldError(err error) error {
	switch {
	case errors.Is(err, service.ErrBalanceHoldInsufficientBalance):
		return service.ErrBatchImageInsufficientBalance
	case errors.Is(err, service.ErrBalanceHoldSettlementCostExceedsHold):
		return service.ErrBatchImageSettlementCostExceedsHold
	default:
		return err
	}
}

func (r *usageBillingRepository) ReserveBalanceHold(ctx context.Context, cmd *service.BalanceHoldCommand) (*service.BalanceHoldResult, error) {
	return r.applyBalanceHold(ctx, cmd, reserveUsageBillingBalance)
}

func (r *usageBillingRepository) CaptureBalanceHold(ctx context.Context, cmd *service.BalanceHoldCommand) (*service.BalanceHoldResult, error) {
	return r.applyBalanceHold(ctx, cmd, captureUsageBillingBalance)
}

func (r *usageBillingRepository) ReleaseBalanceHold(ctx context.Context, cmd *service.BalanceHoldCommand) (*service.BalanceHoldResult, error) {
	return r.applyBalanceHold(ctx, cmd, releaseUsageBillingBalance)
}

type balanceHoldApplyFunc func(context.Context, *sql.Tx, *service.BalanceHoldCommand) (*service.BalanceHoldResult, error)

func (r *usageBillingRepository) applyBalanceHold(
	ctx context.Context,
	cmd *service.BalanceHoldCommand,
	apply balanceHoldApplyFunc,
) (_ *service.BalanceHoldResult, err error) {
	if cmd == nil {
		return &service.BalanceHoldResult{}, nil
	}
	if r == nil || r.db == nil {
		return nil, errors.New("usage billing repository db is nil")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()

	result, err := r.applyBalanceHoldTx(ctx, tx, cmd, apply)
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	tx = nil
	return result, nil
}

func (r *usageBillingRepository) reserveBalanceHoldTx(ctx context.Context, tx *sql.Tx, cmd *service.BalanceHoldCommand) (*service.BalanceHoldResult, error) {
	return r.applyBalanceHoldTx(ctx, tx, cmd, reserveUsageBillingBalance)
}

func (r *usageBillingRepository) applyBalanceHoldTx(
	ctx context.Context,
	tx *sql.Tx,
	cmd *service.BalanceHoldCommand,
	apply balanceHoldApplyFunc,
) (*service.BalanceHoldResult, error) {
	if cmd == nil {
		return &service.BalanceHoldResult{}, nil
	}
	if tx == nil {
		return nil, errors.New("usage billing transaction is nil")
	}
	cmd.Normalize()
	if err := cmd.Validate(); err != nil {
		return nil, err
	}

	applied, err := r.claimUsageBillingRequest(ctx, tx, cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
	if err != nil {
		return nil, err
	}
	if !applied {
		return &service.BalanceHoldResult{Applied: false}, nil
	}

	result, err := apply(ctx, tx, cmd)
	if err != nil {
		return nil, err
	}
	if result == nil {
		result = &service.BalanceHoldResult{}
	}
	result.Applied = true
	return result, nil
}

func (r *usageBillingRepository) applyUsageBillingEffects(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand, result *service.UsageBillingApplyResult) error {
	if err := cmd.ValidateQuotaTime(); err != nil {
		return err
	}
	quotaCommand := cmd
	if cmd.QuotaTime != nil {
		var err error
		quotaCommand, err = prepareVideoQuotaPostingCommand(ctx, tx, cmd)
		if err != nil {
			return err
		}
		result.QuotaPostedAt = &quotaCommand.OccurredAt
	}
	if cmd.SubscriptionCost > 0 && cmd.SubscriptionID != nil {
		if err := incrementUsageBillingSubscription(ctx, tx, *cmd.SubscriptionID, cmd.SubscriptionCost); err != nil {
			return err
		}
	}

	if cmd.BalanceCost > 0 {
		newBalance, sufficient, err := deductUsageBillingBalance(ctx, tx, cmd.UserID, cmd.BalanceCost)
		if err != nil {
			return err
		}
		result.NewBalance = &newBalance
		result.BalanceOverdrafted = !sufficient
	}

	// Key 已不存在时跳过其自身的额度/限速计数，其余结算项不受影响。
	if cmd.APIKeyQuotaCost > 0 {
		exhausted, err := incrementUsageBillingAPIKeyQuota(ctx, tx, cmd.APIKeyID, cmd.APIKeyQuotaCost)
		if err != nil && !errors.Is(err, service.ErrAPIKeyNotFound) {
			return err
		}
		result.APIKeyQuotaExhausted = exhausted
	}

	if cmd.APIKeyRateLimitCost > 0 {
		var err error
		if cmd.QuotaTime == nil {
			err = incrementUsageBillingAPIKeyRateLimit(ctx, tx, cmd.APIKeyID, cmd.APIKeyRateLimitCost)
		} else {
			err = incrementUsageBillingAPIKeyQuotaAtEvent(ctx, tx, quotaCommand)
		}
		if err != nil && !errors.Is(err, service.ErrAPIKeyNotFound) {
			return err
		}
	}

	// 账号已不存在时跳过其配额计数，其余结算项不受影响。
	if cmd.AccountQuotaCost > 0 && service.AccountTypeSupportsQuota(cmd.AccountType) {
		quotaState, err := incrementUsageBillingAccountQuota(ctx, tx, cmd.AccountID, cmd.AccountQuotaCost, cmd.AccountQuotaHasDailyLimit, cmd.AccountQuotaHasWeeklyLimit)
		if err != nil && !errors.Is(err, service.ErrAccountNotFound) {
			return err
		}
		result.QuotaState = quotaState
	}

	if cmd.PlatformQuotaCost > 0 && strings.TrimSpace(cmd.Platform) != "" {
		if err := applyUsageBillingPlatformQuota(ctx, tx, quotaCommand); err != nil {
			return err
		}
	}

	return nil
}

type usageBillingQuotaWindow struct {
	usage float64
	start *time.Time
}

type usageBillingPlatformQuotaRow struct {
	id      int64
	daily   usageBillingQuotaWindow
	weekly  usageBillingQuotaWindow
	monthly usageBillingQuotaWindow
}

func scanUsageBillingPlatformQuotaRow(
	ctx context.Context,
	tx *sql.Tx,
	userID int64,
	platform string,
) (usageBillingPlatformQuotaRow, bool, error) {
	var (
		row                                   usageBillingPlatformQuotaRow
		dailyStart, weeklyStart, monthlyStart sql.NullTime
	)
	err := tx.QueryRowContext(ctx, `
		SELECT id,
			daily_usage_usd, weekly_usage_usd, monthly_usage_usd,
			daily_window_start, weekly_window_start, monthly_window_start
		FROM user_platform_quotas
		WHERE user_id = $1 AND platform = $2 AND deleted_at IS NULL
		FOR UPDATE
	`, userID, platform).Scan(
		&row.id,
		&row.daily.usage, &row.weekly.usage, &row.monthly.usage,
		&dailyStart, &weeklyStart, &monthlyStart,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return usageBillingPlatformQuotaRow{}, false, nil
	}
	if err != nil {
		return usageBillingPlatformQuotaRow{}, false, err
	}
	if dailyStart.Valid {
		row.daily.start = &dailyStart.Time
	}
	if weeklyStart.Valid {
		row.weekly.start = &weeklyStart.Time
	}
	if monthlyStart.Valid {
		row.monthly.start = &monthlyStart.Time
	}
	return row, true, nil
}

func normalizeUsageBillingFixedWindow(
	window usageBillingQuotaWindow,
	currentStart time.Time,
) usageBillingQuotaWindow {
	if window.start == nil || window.start.Before(currentStart) {
		return usageBillingQuotaWindow{start: &currentStart}
	}
	start := *window.start
	return usageBillingQuotaWindow{usage: window.usage, start: &start}
}

func normalizeUsageBillingMonthlyWindow(
	window usageBillingQuotaWindow,
	now time.Time,
) usageBillingQuotaWindow {
	if window.start == nil || now.Sub(*window.start) >= 30*24*time.Hour {
		return usageBillingQuotaWindow{start: &now}
	}
	start := *window.start
	return usageBillingQuotaWindow{usage: window.usage, start: &start}
}

func mergeUsageBillingQuotaWindow(
	current usageBillingQuotaWindow,
	snapshot usageBillingQuotaWindow,
) usageBillingQuotaWindow {
	if current.start == nil {
		return snapshot
	}
	if snapshot.start == nil {
		return current
	}
	if snapshot.start.After(*current.start) {
		return snapshot
	}
	if current.start.After(*snapshot.start) {
		return current
	}
	if snapshot.usage > current.usage {
		current.usage = snapshot.usage
	}
	return current
}

func usageBillingPlatformQuotaSnapshotWindows(
	snapshot *service.UsageBillingPlatformQuotaSnapshot,
) (usageBillingQuotaWindow, usageBillingQuotaWindow, usageBillingQuotaWindow) {
	if snapshot == nil {
		return usageBillingQuotaWindow{}, usageBillingQuotaWindow{}, usageBillingQuotaWindow{}
	}
	return usageBillingQuotaWindow{usage: snapshot.DailyUsageUSD, start: snapshot.DailyWindowStart},
		usageBillingQuotaWindow{usage: snapshot.WeeklyUsageUSD, start: snapshot.WeeklyWindowStart},
		usageBillingQuotaWindow{usage: snapshot.MonthlyUsageUSD, start: snapshot.MonthlyWindowStart}
}

func reconciledUsageBillingPlatformQuota(
	row usageBillingPlatformQuotaRow,
	snapshot *service.UsageBillingPlatformQuotaSnapshot,
	cost float64,
	now time.Time,
) usageBillingPlatformQuotaRow {
	dailySnapshot, weeklySnapshot, monthlySnapshot := usageBillingPlatformQuotaSnapshotWindows(snapshot)
	dailyStart := timezone.StartOfDay(now)
	weeklyStart := timezone.StartOfWeek(now)

	row.daily = mergeUsageBillingQuotaWindow(
		normalizeUsageBillingFixedWindow(row.daily, dailyStart),
		normalizeUsageBillingFixedWindow(dailySnapshot, dailyStart),
	)
	row.weekly = mergeUsageBillingQuotaWindow(
		normalizeUsageBillingFixedWindow(row.weekly, weeklyStart),
		normalizeUsageBillingFixedWindow(weeklySnapshot, weeklyStart),
	)
	row.monthly = normalizeUsageBillingMonthlyWindow(row.monthly, now)
	// A missing or already-expired snapshot has no current rolling-window
	// evidence. Normalizing such a snapshot to this event's OccurredAt would
	// manufacture a newer anchor and could discard usage committed by an
	// earlier waiter on the same row lock. Only an explicit, still-current
	// snapshot may participate in newer-window selection. A future anchor is
	// retained as newer evidence rather than being treated as expired.
	if monthlySnapshot.start != nil &&
		now.Sub(*monthlySnapshot.start) < 30*24*time.Hour {
		row.monthly = mergeUsageBillingQuotaWindow(
			row.monthly,
			normalizeUsageBillingMonthlyWindow(monthlySnapshot, now),
		)
	}
	row.daily.usage += cost
	row.weekly.usage += cost
	row.monthly.usage += cost
	return row
}

func applyUsageBillingPlatformQuota(
	ctx context.Context,
	tx *sql.Tx,
	cmd *service.UsageBillingCommand,
) error {
	if cmd == nil || cmd.PlatformQuotaCost <= 0 || strings.TrimSpace(cmd.Platform) == "" {
		return nil
	}
	if math.IsNaN(cmd.PlatformQuotaCost) || math.IsInf(cmd.PlatformQuotaCost, 0) {
		return service.ErrUsageBillingPayloadInvalid
	}
	now := cmd.OccurredAt
	if now.IsZero() {
		now = time.Now().UTC()
	}
	platform := strings.TrimSpace(cmd.Platform)

	for attempt := 0; attempt < 2; attempt++ {
		row, exists, err := scanUsageBillingPlatformQuotaRow(ctx, tx, cmd.UserID, platform)
		if err != nil {
			return err
		}
		if cmd.QuotaTime == nil {
			row = reconciledUsageBillingPlatformQuota(row, cmd.PlatformQuotaSnapshot, cmd.PlatformQuotaCost, now)
		} else {
			row = reconciledUsageBillingPlatformQuotaAtEvent(row, cmd.PlatformQuotaSnapshot, cmd.PlatformQuotaCost, now, cmd.QuotaTime)
		}
		if exists {
			result, err := tx.ExecContext(ctx, `
				UPDATE user_platform_quotas
				SET daily_usage_usd = $2,
					weekly_usage_usd = $3,
					monthly_usage_usd = $4,
					daily_window_start = $5,
					weekly_window_start = $6,
					monthly_window_start = $7,
					updated_at = NOW()
				WHERE id = $1 AND deleted_at IS NULL
			`, row.id, row.daily.usage, row.weekly.usage, row.monthly.usage,
				row.daily.start, row.weekly.start, row.monthly.start)
			if err != nil {
				return err
			}
			affected, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if affected != 1 {
				return errors.New("usage billing platform quota row was concurrently removed")
			}
			return nil
		}

		result, err := tx.ExecContext(ctx, `
			INSERT INTO user_platform_quotas (
				user_id, platform,
				daily_usage_usd, weekly_usage_usd, monthly_usage_usd,
				daily_window_start, weekly_window_start, monthly_window_start,
				created_at, updated_at
			)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW(), NOW())
			ON CONFLICT (user_id, platform) WHERE deleted_at IS NULL DO NOTHING
		`, cmd.UserID, platform, row.daily.usage, row.weekly.usage, row.monthly.usage,
			row.daily.start, row.weekly.start, row.monthly.start)
		if err != nil {
			return err
		}
		affected, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if affected == 1 {
			return nil
		}
	}
	return errors.New("usage billing platform quota row conflicted repeatedly")
}

func incrementUsageBillingSubscription(ctx context.Context, tx *sql.Tx, subscriptionID int64, costUSD float64) error {
	const updateSQL = `
		UPDATE user_subscriptions
		SET
			daily_usage_usd = daily_usage_usd + $1,
			weekly_usage_usd = weekly_usage_usd + $1,
			monthly_usage_usd = monthly_usage_usd + $1,
			updated_at = NOW()
		WHERE id = $2
	`
	res, err := tx.ExecContext(ctx, updateSQL, costUSD, subscriptionID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected > 0 {
		return nil
	}
	return service.ErrSubscriptionNotFound
}

func deductUsageBillingBalance(ctx context.Context, tx *sql.Tx, userID int64, amount float64) (float64, bool, error) {
	var newBalance float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2 AND balance >= $1
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if err == nil {
		return newBalance, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			updated_at = NOW()
		WHERE id = $2
		RETURNING balance
	`, amount, userID).Scan(&newBalance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, service.ErrUserNotFound
	}
	if err != nil {
		return 0, false, err
	}
	return newBalance, false, nil
}

func reserveUsageBillingBalance(ctx context.Context, tx *sql.Tx, cmd *service.BalanceHoldCommand) (*service.BalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance - $1,
			frozen_balance = COALESCE(frozen_balance, 0) + $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND balance >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, service.ErrBalanceHoldInsufficientBalance
}

func captureUsageBillingBalance(ctx context.Context, tx *sql.Tx, cmd *service.BalanceHoldCommand) (*service.BalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 && cmd.ActualAmount <= 0 {
		return &service.BalanceHoldResult{}, nil
	}
	if cmd.Scope == service.BalanceHoldScopeVideoTask {
		held, err := balanceHoldClaimExists(
			ctx,
			tx,
			service.BalanceHoldReserveRequestID(cmd.Scope, cmd.RefID),
			cmd.APIKeyID,
		)
		if err != nil {
			return nil, err
		}
		if !held {
			return nil, service.ErrBalanceHoldReserveNotFound
		}
	}
	if cmd.Scope != service.BalanceHoldScopeVideoTask && cmd.ActualAmount-cmd.HoldAmount > 0.00000001 {
		return nil, service.ErrBalanceHoldSettlementCostExceedsHold
	}
	var balance, frozen float64
	err := tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance
				+ CASE WHEN $1 > $2 THEN $1 - $2 ELSE 0 END
				- CASE WHEN $2 > $1 THEN $2 - $1 ELSE 0 END,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $3 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.ActualAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BalanceHoldResult{
			NewBalance:         &balance,
			FrozenBalance:      &frozen,
			BalanceOverdrafted: balance < 0,
		}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("balance hold frozen balance is insufficient")
}

func releaseUsageBillingBalance(ctx context.Context, tx *sql.Tx, cmd *service.BalanceHoldCommand) (*service.BalanceHoldResult, error) {
	if cmd.HoldAmount <= 0 {
		return &service.BalanceHoldResult{}, nil
	}
	holdRequestID := service.BalanceHoldReserveRequestID(cmd.Scope, cmd.RefID)
	held, err := balanceHoldClaimExists(ctx, tx, holdRequestID, cmd.APIKeyID)
	if err != nil {
		return nil, err
	}
	if !held {
		logger.LegacyPrintf("repository.usage_billing", "balance hold release skipped because reserve was never committed: scope=%s ref=%s", cmd.Scope, cmd.RefID)
		return &service.BalanceHoldResult{}, nil
	}
	var balance, frozen float64
	err = tx.QueryRowContext(ctx, `
		UPDATE users
		SET balance = balance + $1,
			frozen_balance = COALESCE(frozen_balance, 0) - $1,
			updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL AND COALESCE(frozen_balance, 0) >= $1
		RETURNING balance, frozen_balance
	`, cmd.HoldAmount, cmd.UserID).Scan(&balance, &frozen)
	if err == nil {
		return &service.BalanceHoldResult{NewBalance: &balance, FrozenBalance: &frozen}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if exists, existsErr := userExistsForBilling(ctx, tx, cmd.UserID); existsErr != nil {
		return nil, existsErr
	} else if !exists {
		return nil, service.ErrUserNotFound
	}
	return nil, errors.New("balance hold frozen balance is insufficient")
}

func balanceHoldClaimExists(ctx context.Context, tx *sql.Tx, holdRequestID string, apiKeyID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	err = tx.QueryRowContext(ctx, `
		SELECT 1
		FROM usage_billing_dedup_archive
		WHERE request_id = $1 AND api_key_id = $2
	`, holdRequestID, apiKeyID).Scan(&exists)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return false, err
}

func userExistsForBilling(ctx context.Context, tx *sql.Tx, userID int64) (bool, error) {
	var exists int
	err := tx.QueryRowContext(ctx, `
		SELECT 1
		FROM users
		WHERE id = $1 AND deleted_at IS NULL
	`, userID).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func incrementUsageBillingAPIKeyQuota(ctx context.Context, tx *sql.Tx, apiKeyID int64, amount float64) (bool, error) {
	var exhausted bool
	err := tx.QueryRowContext(ctx, `
		UPDATE api_keys
		SET quota_used = quota_used + $1,
			status = CASE
				WHEN quota > 0
					AND status = $3
					AND quota_used < quota
					AND quota_used + $1 >= quota
				THEN $4
				ELSE status
			END,
			updated_at = NOW()
		WHERE id = $2
		RETURNING quota > 0 AND quota_used >= quota AND quota_used - $1 < quota
	`, amount, apiKeyID, service.StatusAPIKeyActive, service.StatusAPIKeyQuotaExhausted).Scan(&exhausted)
	if errors.Is(err, sql.ErrNoRows) {
		// The API key is an attribution/counter target, not the source of
		// funds. A hard-deleted historical key must not roll back the user's
		// balance/subscription charge after the upstream request succeeded.
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return exhausted, nil
}

func incrementUsageBillingAPIKeyRateLimit(ctx context.Context, tx *sql.Tx, apiKeyID int64, cost float64) error {
	res, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			usage_5h = CASE WHEN window_5h_start IS NOT NULL AND window_5h_start + INTERVAL '5 hours' <= NOW() THEN $1 ELSE usage_5h + $1 END,
			usage_1d = CASE WHEN window_1d_start IS NOT NULL AND window_1d_start + INTERVAL '24 hours' <= NOW() THEN $1 ELSE usage_1d + $1 END,
			usage_7d = CASE WHEN window_7d_start IS NOT NULL AND window_7d_start + INTERVAL '7 days' <= NOW() THEN $1 ELSE usage_7d + $1 END,
			window_5h_start = CASE WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= NOW() THEN NOW() ELSE window_5h_start END,
			window_1d_start = CASE WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= NOW() THEN date_trunc('day', NOW()) ELSE window_1d_start END,
			window_7d_start = CASE WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= NOW() THEN date_trunc('day', NOW()) ELSE window_7d_start END,
			updated_at = NOW()
			WHERE id = $2
	`, cost, apiKeyID)
	if err != nil {
		return err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		// Rate-limit accounting is ancillary. Missing historical keys are safe
		// to skip and must not cancel the primary financial transaction.
		return nil
	}
	return nil
}

// accountQuotaTotalIncrementExpr 累加总额度已用量。
const accountQuotaTotalIncrementExpr = `COALESCE(extra, '{}'::jsonb)
			|| jsonb_build_object('quota_used', COALESCE((extra->>'quota_used')::numeric, 0) + $1)`

// accountQuotaDailyIncrementExpr 在库中设置了日上限时累加日用量，周期过期时从本次金额重新计数。
const accountQuotaDailyIncrementExpr = `
			|| CASE WHEN COALESCE((extra->>'quota_daily_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_daily_used',
					CASE WHEN ` + dailyExpiredExpr + `
					THEN $1
					ELSE COALESCE((extra->>'quota_daily_used')::numeric, 0) + $1 END,
					'quota_daily_start',
					CASE WHEN ` + dailyExpiredExpr + `
					THEN ` + nowUTC + `
					ELSE COALESCE(extra->>'quota_daily_start', ` + nowUTC + `) END
				)
				|| CASE WHEN ` + dailyExpiredExpr + ` AND ` + nextDailyResetAtExpr + ` IS NOT NULL
				   THEN jsonb_build_object('quota_daily_reset_at', ` + nextDailyResetAtExpr + `)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END`

// accountQuotaWeeklyIncrementExpr 在库中设置了周上限时累加周用量，周期过期时从本次金额重新计数。
const accountQuotaWeeklyIncrementExpr = `
			|| CASE WHEN COALESCE((extra->>'quota_weekly_limit')::numeric, 0) > 0 THEN
				jsonb_build_object(
					'quota_weekly_used',
					CASE WHEN ` + weeklyExpiredExpr + `
					THEN $1
					ELSE COALESCE((extra->>'quota_weekly_used')::numeric, 0) + $1 END,
					'quota_weekly_start',
					CASE WHEN ` + weeklyExpiredExpr + `
					THEN ` + nowUTC + `
					ELSE COALESCE(extra->>'quota_weekly_start', ` + nowUTC + `) END
				)
				|| CASE WHEN ` + weeklyExpiredExpr + ` AND ` + nextWeeklyResetAtExpr + ` IS NOT NULL
				   THEN jsonb_build_object('quota_weekly_reset_at', ` + nextWeeklyResetAtExpr + `)
				   ELSE '{}'::jsonb END
			ELSE '{}'::jsonb END`

// accountQuotaExtraIsObjectCond 要求 extra 为 JSON 对象：省略某个维度的语句只在对象上与完整语句等价。
//
// accountQuotaDailyLimitUnsetCond / accountQuotaWeeklyLimitUnsetCond 在库中未设置日/周上限时为真：
// 键不存在、值为 JSON null 或不大于 0 的数字。其他取值一律为假，交给包含该维度的语句处理。
// 三个条件都只使用 ->、jsonb_typeof 与 jsonb 比较，对任何行求值都不会报错。
const (
	accountQuotaExtraIsObjectCond   = `jsonb_typeof(extra) = 'object'`
	accountQuotaDailyLimitUnsetCond = `(extra->'quota_daily_limit' IS NULL
			OR jsonb_typeof(extra->'quota_daily_limit') = 'null'
			OR (jsonb_typeof(extra->'quota_daily_limit') = 'number' AND extra->'quota_daily_limit' <= '0'::jsonb))`
	accountQuotaWeeklyLimitUnsetCond = `(extra->'quota_weekly_limit' IS NULL
			OR jsonb_typeof(extra->'quota_weekly_limit') = 'null'
			OR (jsonb_typeof(extra->'quota_weekly_limit') = 'number' AND extra->'quota_weekly_limit' <= '0'::jsonb))`
)

const accountQuotaIncrementReturning = `
		RETURNING
			COALESCE((extra->>'quota_used')::numeric, 0),
			COALESCE((extra->>'quota_limit')::numeric, 0),
			COALESCE((extra->>'quota_daily_used')::numeric, 0),
			COALESCE((extra->>'quota_daily_limit')::numeric, 0),
			COALESCE((extra->>'quota_weekly_used')::numeric, 0),
			COALESCE((extra->>'quota_weekly_limit')::numeric, 0)`

// accountQuotaIncrementSQLs 按 accountQuotaIncrementSQLIndex 存放账号配额累加语句。省略日或周维度的语句
// 在 WHERE 中附加 accountQuotaExtraIsObjectCond 与被省略维度的未设置条件，条件不成立时命中 0 行；
// 同时包含日、周维度的语句不附加这些条件，对任何账号配置都给出完整的累加结果。
var accountQuotaIncrementSQLs = buildAccountQuotaIncrementSQLs()

func accountQuotaIncrementSQLIndex(daily, weekly bool) int {
	index := 0
	if daily {
		index |= 1
	}
	if weekly {
		index |= 2
	}
	return index
}

func buildAccountQuotaIncrementSQLs() [4]string {
	var sqls [4]string
	for _, daily := range []bool{false, true} {
		for _, weekly := range []bool{false, true} {
			set := accountQuotaTotalIncrementExpr
			where := "id = $2 AND deleted_at IS NULL"
			if !daily || !weekly {
				where += "\n\t\t\tAND " + accountQuotaExtraIsObjectCond
			}
			if daily {
				set += accountQuotaDailyIncrementExpr
			} else {
				where += "\n\t\t\tAND " + accountQuotaDailyLimitUnsetCond
			}
			if weekly {
				set += accountQuotaWeeklyIncrementExpr
			} else {
				where += "\n\t\t\tAND " + accountQuotaWeeklyLimitUnsetCond
			}
			sqls[accountQuotaIncrementSQLIndex(daily, weekly)] = "UPDATE accounts SET extra = (\n\t\t\t" + set +
				"\n\t\t), updated_at = NOW()\n\t\tWHERE " + where + accountQuotaIncrementReturning
		}
	}
	return sqls
}

// incrementUsageBillingAccountQuota 累加账号配额并返回累加后的状态。hasDaily/hasWeekly 是调用方所见的
// 账号日/周上限设置，只决定先执行哪条语句；该语句的附加条件不成立而命中 0 行时，再执行同时包含日、周维度的语句。
func incrementUsageBillingAccountQuota(ctx context.Context, tx *sql.Tx, accountID int64, amount float64, hasDaily, hasWeekly bool) (*service.AccountQuotaState, error) {
	index := accountQuotaIncrementSQLIndex(hasDaily, hasWeekly)
	fullIndex := accountQuotaIncrementSQLIndex(true, true)
	state, err := execAccountQuotaIncrement(ctx, tx, accountQuotaIncrementSQLs[index], accountID, amount)
	if err != nil {
		return nil, err
	}
	if state == nil && index != fullIndex {
		state, err = execAccountQuotaIncrement(ctx, tx, accountQuotaIncrementSQLs[fullIndex], accountID, amount)
		if err != nil {
			return nil, err
		}
	}
	if state == nil {
		return nil, service.ErrAccountNotFound
	}
	// 任意维度额度在本次递增中从"未超"跨越到"已超"时，必须刷新调度快照，
	// 否则 Redis 中缓存的 Account 仍显示旧的 used 值，后续请求会继续选中本账号，
	// 最终观察到 daily_used / weekly_used 大幅超过配置的 limit。
	// 对于日/周额度，即使本次触发了周期重置（pre=0、post=amount），
	// 判定式 (post-amount) < limit 同样成立，逻辑与总额度保持一致。
	crossedTotal := state.TotalLimit > 0 && state.TotalUsed >= state.TotalLimit && (state.TotalUsed-amount) < state.TotalLimit
	crossedDaily := state.DailyLimit > 0 && state.DailyUsed >= state.DailyLimit && (state.DailyUsed-amount) < state.DailyLimit
	crossedWeekly := state.WeeklyLimit > 0 && state.WeeklyUsed >= state.WeeklyLimit && (state.WeeklyUsed-amount) < state.WeeklyLimit
	if crossedTotal || crossedDaily || crossedWeekly {
		if err := enqueueSchedulerOutbox(ctx, tx, service.SchedulerOutboxEventAccountChanged, &accountID, nil, nil); err != nil {
			logger.LegacyPrintf("repository.usage_billing", "[SchedulerOutbox] enqueue quota exceeded failed: account=%d err=%v", accountID, err)
			return nil, err
		}
	}
	return state, nil
}

// execAccountQuotaIncrement 执行一条账号配额累加语句，未命中任何行时返回 nil 状态。
func execAccountQuotaIncrement(ctx context.Context, tx *sql.Tx, query string, accountID int64, amount float64) (*service.AccountQuotaState, error) {
	rows, err := tx.QueryContext(ctx, query, amount, accountID)
	if err != nil {
		return nil, err
	}

	var state *service.AccountQuotaState
	if rows.Next() {
		state = &service.AccountQuotaState{}
		if err := rows.Scan(
			&state.TotalUsed, &state.TotalLimit,
			&state.DailyUsed, &state.DailyLimit,
			&state.WeeklyUsed, &state.WeeklyLimit,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	// 必须在执行下一条 SQL 前显式关闭 rows：pq 驱动在同一连接上
	// 不允许前一条查询的结果集未耗尽时启动新查询，否则会返回
	// "unexpected Parse response" 错误。
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return state, nil
}
