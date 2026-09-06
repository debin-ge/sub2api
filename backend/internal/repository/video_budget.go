package repository

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const videoBudgetReservationsSQL = `
	SELECT
		COALESCE(SUM(hold_amount) FILTER (WHERE api_key_id = $2), 0) AS key_reserved,
		COALESCE(SUM(hold_amount) FILTER (WHERE provider = $3), 0) AS platform_reserved,
		COALESCE(BOOL_OR((api_key_id = $2 OR provider = $3) AND
			(hold_amount IS NULL OR hold_amount < 0 OR hold_amount::text IN ('NaN', 'Infinity', '-Infinity'))), false) AS invalid
	FROM video_tasks
	WHERE user_id = $1 AND billing_state IN ('held', 'capture_pending', 'release_pending', 'manual_review')
`

type videoBudgetOwner struct {
	Status               string
	Balance              float64
	Deleted              sql.NullTime
	IsVIP                bool
	RestrictPublicGroups bool
}

func lockVideoBudgetOwnerTx(ctx context.Context, tx *sql.Tx, userID int64) (*videoBudgetOwner, error) {
	owner := &videoBudgetOwner{}
	err := tx.QueryRowContext(ctx, `SELECT status, balance, deleted_at, is_vip, restrict_public_groups FROM users WHERE id = $1 FOR NO KEY UPDATE`, userID).
		Scan(&owner.Status, &owner.Balance, &owner.Deleted, &owner.IsVIP, &owner.RestrictPublicGroups)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return owner, err
}

func checkVideoKeyBudgetTx(ctx context.Context, tx *sql.Tx, params service.VideoCreateTaskParams, owner *videoBudgetOwner, now time.Time) error {
	if owner == nil {
		return service.ErrUserNotFound
	}
	if owner.Status != service.StatusActive || owner.Deleted.Valid {
		return service.ErrVideoInvalidRequest
	}
	if math.IsNaN(owner.Balance) || math.IsInf(owner.Balance, 0) {
		return service.ErrBillingServiceUnavailable
	}
	if math.IsNaN(params.HoldAmount) || math.IsInf(params.HoldAmount, 0) || params.HoldAmount < 0 || strings.TrimSpace(params.Provider) == "" {
		return service.ErrVideoInvalidRequest
	}
	if owner.Balance < params.HoldAmount {
		return service.ErrVideoInsufficientBalance
	}
	var userID int64
	var groupID sql.NullInt64
	var status string
	var expires sql.NullTime
	var exhausted, exceeded5h, exceeded1d, exceeded7d, invalid bool
	err := tx.QueryRowContext(ctx, `
		WITH reservations AS (`+videoBudgetReservationsSQL+`)
		SELECT k.user_id, k.group_id, k.status, k.expires_at,
			k.quota > 0 AND (k.quota_used + r.key_reserved >= k.quota OR k.quota_used + r.key_reserved + $4::numeric > k.quota),
			k.rate_limit_5h > 0 AND (
				CASE WHEN k.window_5h_start IS NULL OR k.window_5h_start + INTERVAL '5 hours' <= $5 THEN 0 ELSE k.usage_5h END
				+ r.key_reserved >= k.rate_limit_5h OR
				CASE WHEN k.window_5h_start IS NULL OR k.window_5h_start + INTERVAL '5 hours' <= $5 THEN 0 ELSE k.usage_5h END
				+ r.key_reserved + $4::numeric > k.rate_limit_5h),
			k.rate_limit_1d > 0 AND (
				CASE WHEN k.window_1d_start IS NULL OR k.window_1d_start + INTERVAL '24 hours' <= $5 THEN 0 ELSE k.usage_1d END
				+ r.key_reserved >= k.rate_limit_1d OR
				CASE WHEN k.window_1d_start IS NULL OR k.window_1d_start + INTERVAL '24 hours' <= $5 THEN 0 ELSE k.usage_1d END
				+ r.key_reserved + $4::numeric > k.rate_limit_1d),
			k.rate_limit_7d > 0 AND (
				CASE WHEN k.window_7d_start IS NULL OR k.window_7d_start + INTERVAL '7 days' <= $5 THEN 0 ELSE k.usage_7d END
				+ r.key_reserved >= k.rate_limit_7d OR
				CASE WHEN k.window_7d_start IS NULL OR k.window_7d_start + INTERVAL '7 days' <= $5 THEN 0 ELSE k.usage_7d END
				+ r.key_reserved + $4::numeric > k.rate_limit_7d),
			r.invalid OR EXISTS (
				SELECT 1 FROM (VALUES (k.quota), (k.quota_used), (k.rate_limit_5h), (k.rate_limit_1d),
					(k.rate_limit_7d), (k.usage_5h), (k.usage_1d), (k.usage_7d)) AS amounts(value)
				WHERE value < 0 OR value::text IN ('NaN', 'Infinity', '-Infinity'))
		FROM api_keys k CROSS JOIN reservations r
		WHERE k.id = $2 AND k.deleted_at IS NULL FOR NO KEY UPDATE OF k
	`, params.Owner.UserID, params.Owner.APIKeyID, params.Provider, params.HoldAmount, now).
		Scan(&userID, &groupID, &status, &expires, &exhausted, &exceeded5h, &exceeded1d, &exceeded7d, &invalid)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrAPIKeyNotFound
	}
	if err != nil {
		return err
	}
	if userID != params.Owner.UserID || (status != service.StatusActive && status != service.StatusAPIKeyQuotaExhausted) {
		return service.ErrVideoInvalidRequest
	}
	if groupID.Valid != (params.Owner.GroupID != nil) || (groupID.Valid && groupID.Int64 != *params.Owner.GroupID) {
		return service.ErrVideoNoAccountAvailable
	}
	if expires.Valid && !now.Before(expires.Time) {
		return service.ErrAPIKeyExpired
	}
	if invalid {
		return service.ErrBillingServiceUnavailable
	}
	if exhausted || status == service.StatusAPIKeyQuotaExhausted {
		return service.ErrAPIKeyQuotaExhausted
	}
	if exceeded5h {
		return service.ErrAPIKeyRateLimit5hExceeded
	}
	if exceeded1d {
		return service.ErrAPIKeyRateLimit1dExceeded
	}
	if exceeded7d {
		return service.ErrAPIKeyRateLimit7dExceeded
	}
	return nil
}

func checkVideoPlatformBudgetTx(ctx context.Context, tx *sql.Tx, params service.VideoCreateTaskParams, now time.Time) error {
	var daily, weekly, monthly, invalid bool
	err := tx.QueryRowContext(ctx, `
		WITH reservations AS (`+videoBudgetReservationsSQL+`)
		SELECT
			q.daily_limit_usd IS NOT NULL AND (
				CASE WHEN q.daily_window_start IS NULL OR q.daily_window_start < $5 THEN 0 ELSE q.daily_usage_usd END
				+ r.platform_reserved >= q.daily_limit_usd OR
				CASE WHEN q.daily_window_start IS NULL OR q.daily_window_start < $5 THEN 0 ELSE q.daily_usage_usd END
				+ r.platform_reserved + $4::numeric > q.daily_limit_usd),
			q.weekly_limit_usd IS NOT NULL AND (
				CASE WHEN q.weekly_window_start IS NULL OR q.weekly_window_start < $6 THEN 0 ELSE q.weekly_usage_usd END
				+ r.platform_reserved >= q.weekly_limit_usd OR
				CASE WHEN q.weekly_window_start IS NULL OR q.weekly_window_start < $6 THEN 0 ELSE q.weekly_usage_usd END
				+ r.platform_reserved + $4::numeric > q.weekly_limit_usd),
			q.monthly_limit_usd IS NOT NULL AND (
				CASE WHEN q.monthly_window_start IS NULL OR q.monthly_window_start + INTERVAL '30 days' <= $7 THEN 0 ELSE q.monthly_usage_usd END
				+ r.platform_reserved >= q.monthly_limit_usd OR
				CASE WHEN q.monthly_window_start IS NULL OR q.monthly_window_start + INTERVAL '30 days' <= $7 THEN 0 ELSE q.monthly_usage_usd END
				+ r.platform_reserved + $4::numeric > q.monthly_limit_usd),
			r.invalid OR EXISTS (
				SELECT 1 FROM (VALUES (q.daily_limit_usd), (q.weekly_limit_usd), (q.monthly_limit_usd),
					(q.daily_usage_usd), (q.weekly_usage_usd), (q.monthly_usage_usd)) AS amounts(value)
				WHERE value < 0 OR value::text IN ('NaN', 'Infinity', '-Infinity'))
		FROM user_platform_quotas q CROSS JOIN reservations r
		WHERE q.user_id = $1 AND q.platform = $3 AND q.deleted_at IS NULL FOR NO KEY UPDATE OF q
	`, params.Owner.UserID, params.Owner.APIKeyID, params.Provider, params.HoldAmount,
		timezone.StartOfDay(now), timezone.StartOfWeek(now), now).Scan(&daily, &weekly, &monthly, &invalid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if invalid {
		return service.ErrBillingServiceUnavailable
	}
	if daily {
		return service.ErrUserPlatformDailyQuotaExhausted
	}
	if weekly {
		return service.ErrUserPlatformWeeklyQuotaExhausted
	}
	if monthly {
		return service.ErrUserPlatformMonthlyQuotaExhausted
	}
	return nil
}

func checkVideoBudgetGroupTx(ctx context.Context, tx *sql.Tx, userID int64, owner *videoBudgetOwner, groupID *int64) error {
	if groupID == nil {
		return nil
	}
	group := &service.Group{ID: *groupID}
	err := tx.QueryRowContext(ctx, `SELECT status, subscription_type, is_exclusive, vip_only FROM groups WHERE id = $1 AND deleted_at IS NULL FOR SHARE`, *groupID).
		Scan(&group.Status, &group.SubscriptionType, &group.IsExclusive, &group.VIPOnly)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrVideoNoAccountAvailable
	}
	if err != nil {
		return err
	}
	if group.Status != service.StatusActive {
		return service.ErrVideoNoAccountAvailable
	}
	if group.SubscriptionType != service.SubscriptionTypeStandard {
		return service.ErrVideoSubscriptionUnsupported
	}
	profile := &service.GroupAccessProfile{UserID: userID, IsVIP: owner.IsVIP, RestrictPublicGroups: owner.RestrictPublicGroups}
	if group.IsExclusive || owner.RestrictPublicGroups {
		var granted int64
		err := tx.QueryRowContext(ctx, `SELECT group_id FROM user_allowed_groups WHERE user_id = $1 AND group_id = $2 FOR SHARE`, userID, *groupID).Scan(&granted)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err == nil {
			profile.AllowedGroups = []int64{granted}
		}
	}
	return service.NewGroupAccessPolicy().Evaluate(profile, group, service.GroupAccessPrimaryAuth).Error()
}

func (r *apiKeyRepository) GetVideoBudgetSnapshot(ctx context.Context, userID, keyID int64, platform string) (*service.VideoBudgetSnapshot, error) {
	rows, err := r.sql.QueryContext(ctx, `
		WITH reservations AS (`+videoBudgetReservationsSQL+`)
		SELECT k.id, k.user_id, k.group_id, k.status, k.expires_at, k.quota, k.quota_used,
			k.rate_limit_5h, k.rate_limit_1d, k.rate_limit_7d,
			k.usage_5h, k.usage_1d, k.usage_7d, k.window_5h_start, k.window_1d_start, k.window_7d_start,
			q.id, q.daily_limit_usd, q.weekly_limit_usd, q.monthly_limit_usd,
			COALESCE(q.daily_usage_usd, 0), COALESCE(q.weekly_usage_usd, 0), COALESCE(q.monthly_usage_usd, 0),
			q.daily_window_start, q.weekly_window_start, q.monthly_window_start,
			r.key_reserved, r.platform_reserved, r.invalid
		FROM api_keys k CROSS JOIN reservations r
		LEFT JOIN user_platform_quotas q ON q.user_id = $1 AND q.platform = $3 AND q.deleted_at IS NULL
		WHERE k.id = $2 AND k.user_id = $1 AND k.deleted_at IS NULL
	`, userID, keyID, platform)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, err
		}
		return nil, service.ErrAPIKeyNotFound
	}
	key := &service.APIKey{}
	quota := &service.UserPlatformQuotaRecord{UserID: userID, Platform: platform}
	snapshot := &service.VideoBudgetSnapshot{APIKey: key}
	var quotaID sql.NullInt64
	var invalid bool
	if err := rows.Scan(&key.ID, &key.UserID, &key.GroupID, &key.Status, &key.ExpiresAt, &key.Quota, &key.QuotaUsed,
		&key.RateLimit5h, &key.RateLimit1d, &key.RateLimit7d,
		&key.Usage5h, &key.Usage1d, &key.Usage7d, &key.Window5hStart, &key.Window1dStart, &key.Window7dStart,
		&quotaID, &quota.DailyLimitUSD, &quota.WeeklyLimitUSD, &quota.MonthlyLimitUSD,
		&quota.DailyUsageUSD, &quota.WeeklyUsageUSD, &quota.MonthlyUsageUSD,
		&quota.DailyWindowStart, &quota.WeeklyWindowStart, &quota.MonthlyWindowStart,
		&snapshot.KeyReserved, &snapshot.PlatformReserved, &invalid); err != nil {
		return nil, err
	}
	if invalid {
		return nil, service.ErrBillingServiceUnavailable
	}
	if quotaID.Valid {
		snapshot.Platform = quota
	}
	return snapshot, rows.Err()
}
