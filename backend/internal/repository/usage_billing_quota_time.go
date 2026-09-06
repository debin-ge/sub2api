package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func prepareVideoQuotaPostingCommand(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) (*service.UsageBillingCommand, error) {
	zone := timezone.Location().String()
	if zone == "" || zone == "Local" {
		zone = "UTC"
	}
	if cmd.QuotaTime.TimeZone != zone {
		return nil, service.ErrUsageBillingQuotaCalendarMismatch
	}
	var recordID int64
	var starts [6]sql.NullTime
	if cmd.APIKeyRateLimitCost > 0 {
		err := tx.QueryRowContext(ctx, `SELECT window_5h_start, window_1d_start, window_7d_start FROM api_keys WHERE id = $1 FOR UPDATE`, cmd.APIKeyID).Scan(&starts[0], &starts[1], &starts[2])
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if cmd.AccountQuotaCost > 0 && (strings.EqualFold(cmd.AccountType, service.AccountTypeAPIKey) || strings.EqualFold(cmd.AccountType, service.AccountTypeBedrock)) {
		err := tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id = $1 FOR UPDATE`, cmd.AccountID).Scan(&recordID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	if cmd.PlatformQuotaCost > 0 && cmd.Platform != "" {
		_, err := tx.ExecContext(ctx, `INSERT INTO user_platform_quotas (user_id, platform) VALUES ($1, $2)
			ON CONFLICT (user_id, platform) WHERE deleted_at IS NULL DO NOTHING`, cmd.UserID, cmd.Platform)
		if err != nil {
			return nil, err
		}
		if err := tx.QueryRowContext(ctx, `SELECT daily_window_start, weekly_window_start, monthly_window_start FROM user_platform_quotas WHERE user_id = $1 AND platform = $2 AND deleted_at IS NULL FOR UPDATE`, cmd.UserID, cmd.Platform).Scan(&starts[3], &starts[4], &starts[5]); err != nil {
			return nil, err
		}
	}
	posted := *cmd
	if err := tx.QueryRowContext(ctx, `SELECT clock_timestamp()`).Scan(&posted.OccurredAt); err != nil {
		return nil, err
	}
	if posted.OccurredAt.Before(cmd.OccurredAt) {
		return nil, fmt.Errorf("%w: video quota posting precedes terminal observation", service.ErrUsageBillingPayloadInvalid)
	}
	for _, start := range starts {
		if start.Valid && start.Time.After(posted.OccurredAt) {
			return nil, fmt.Errorf("%w: video quota window is ahead of database time", service.ErrUsageBillingPayloadInvalid)
		}
	}
	if snapshot := cmd.PlatformQuotaSnapshot; snapshot != nil {
		for _, start := range []*time.Time{snapshot.DailyWindowStart, snapshot.WeeklyWindowStart, snapshot.MonthlyWindowStart} {
			if start != nil && start.After(posted.OccurredAt) {
				return nil, fmt.Errorf("%w: video quota cache window is ahead of database time", service.ErrUsageBillingPayloadInvalid)
			}
		}
	}
	clock, err := service.ResolveUsageBillingQuotaTime(posted.OccurredAt, cmd.QuotaTime.TimeZone)
	if err != nil {
		return nil, err
	}
	posted.QuotaTime = clock
	return &posted, nil
}

func incrementUsageBillingAPIKeyQuotaAtEvent(ctx context.Context, tx *sql.Tx, cmd *service.UsageBillingCommand) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE api_keys SET
			usage_5h = CASE
				WHEN window_5h_start > $3 THEN usage_5h
				WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= $3 THEN $1
				ELSE usage_5h + $1 END,
			usage_1d = CASE
				WHEN window_1d_start > $3 THEN usage_1d
				WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= $3 THEN $1
				ELSE usage_1d + $1 END,
			usage_7d = CASE
				WHEN window_7d_start > $3 THEN usage_7d
				WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= $3 THEN $1
				ELSE usage_7d + $1 END,
			window_5h_start = CASE
				WHEN window_5h_start IS NULL OR window_5h_start + INTERVAL '5 hours' <= $3 THEN $3
				ELSE window_5h_start END,
			window_1d_start = CASE
				WHEN window_1d_start IS NULL OR window_1d_start + INTERVAL '24 hours' <= $3 THEN
					CASE WHEN $4::timestamptz + INTERVAL '24 hours' <= $3 THEN $3 ELSE $4 END
				ELSE window_1d_start END,
			window_7d_start = CASE
				WHEN window_7d_start IS NULL OR window_7d_start + INTERVAL '7 days' <= $3 THEN $4
				ELSE window_7d_start END,
			updated_at = NOW()
		WHERE id = $2
	`, cmd.APIKeyRateLimitCost, cmd.APIKeyID, cmd.OccurredAt, cmd.QuotaTime.DayStart)
	return err
}

func reconciledUsageBillingPlatformQuotaAtEvent(
	row usageBillingPlatformQuotaRow,
	snapshot *service.UsageBillingPlatformQuotaSnapshot,
	cost float64,
	occurredAt time.Time,
	clock *service.UsageBillingQuotaTime,
) usageBillingPlatformQuotaRow {
	dailySnapshot, weeklySnapshot, monthlySnapshot := usageBillingPlatformQuotaSnapshotWindows(snapshot)
	row.daily = mergeUsageBillingQuotaWindow(
		normalizeUsageBillingFixedWindow(row.daily, clock.DayStart),
		normalizeUsageBillingFixedWindow(dailySnapshot, clock.DayStart),
	)
	row.weekly = mergeUsageBillingQuotaWindow(
		normalizeUsageBillingFixedWindow(row.weekly, clock.WeekStart),
		normalizeUsageBillingFixedWindow(weeklySnapshot, clock.WeekStart),
	)
	row.monthly = normalizeUsageBillingMonthlyWindow(row.monthly, occurredAt)
	if monthlySnapshot.start != nil && occurredAt.Sub(*monthlySnapshot.start) < 30*24*time.Hour {
		row.monthly = mergeUsageBillingQuotaWindow(row.monthly, monthlySnapshot)
	}
	for _, window := range []*usageBillingQuotaWindow{&row.daily, &row.weekly, &row.monthly} {
		if !occurredAt.Before(*window.start) {
			window.usage += cost
		}
	}
	return row
}
