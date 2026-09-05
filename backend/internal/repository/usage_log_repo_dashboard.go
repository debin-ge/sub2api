package repository

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

// getPerformanceStats 获取 RPM 和 TPM（近5分钟平均值，可选按用户过滤）
func (r *usageLogRepository) getPerformanceStats(ctx context.Context, userID int64) (rpm, tpm int64, err error) {
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
	query := `
		SELECT
			COUNT(*) as request_count,
			COALESCE(SUM(input_tokens + output_tokens), 0) as token_count
		FROM usage_logs
		WHERE created_at >= $1
		  AND ` + usageLogBusinessStatsFilter("")
	args := []any{fiveMinutesAgo}
	if userID > 0 {
		query += " AND user_id = $2"
		args = append(args, userID)
	}

	var requestCount int64
	var tokenCount int64
	if err := scanSingleRow(ctx, r.sql, query, args, &requestCount, &tokenCount); err != nil {
		return 0, 0, err
	}
	return requestCount / 5, tokenCount / 5, nil
}

// UserStats 用户使用统计
type UserStats struct {
	TotalRequests   int64   `json:"total_requests"`
	TotalTokens     int64   `json:"total_tokens"`
	TotalCost       float64 `json:"total_cost"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
}

func (r *usageLogRepository) GetUserStats(ctx context.Context, userID int64, startTime, endTime time.Time) (*UserStats, error) {
	query := `
		SELECT
			COUNT(*) as total_requests,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as total_tokens,
			COALESCE(SUM(actual_cost), 0) as total_cost,
			COALESCE(SUM(input_tokens), 0) as input_tokens,
			COALESCE(SUM(output_tokens), 0) as output_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as cache_read_tokens
		FROM usage_logs
		WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
		  AND ` + usageLogBusinessStatsFilter("") + `
	`

	stats := &UserStats{}
	if err := scanSingleRow(
		ctx,
		r.sql,
		query,
		[]any{userID, startTime, endTime},
		&stats.TotalRequests,
		&stats.TotalTokens,
		&stats.TotalCost,
		&stats.InputTokens,
		&stats.OutputTokens,
		&stats.CacheReadTokens,
	); err != nil {
		return nil, err
	}
	return stats, nil
}

// DashboardStats 仪表盘统计
type DashboardStats = usagestats.DashboardStats

// GetDashboardStats returns the admin dashboard figures. Cumulative totals come
// from the daily rollup; the "today" block is anchored on the caller's timezone
// so the dashboard card agrees with the usage-records page filtered to the same
// local day. userTZ falls back to the server timezone when empty or unknown.
func (r *usageLogRepository) GetDashboardStats(ctx context.Context, userTZ string) (*DashboardStats, error) {
	stats := &DashboardStats{}
	now := timezone.Now()
	todayStart, todayEnd := timezone.TodayRangeInUserLocation(userTZ)

	if err := r.fillDashboardEntityStats(ctx, stats, todayStart, todayEnd, now); err != nil {
		return nil, err
	}
	if err := r.fillDashboardTotalStatsAggregated(ctx, stats); err != nil {
		return nil, err
	}
	if err := r.fillDashboardTodayStatsAggregated(ctx, stats, todayStart, todayEnd); err != nil {
		return nil, err
	}
	if err := r.fillDashboardHourlyActiveUsersAggregated(ctx, stats, now); err != nil {
		return nil, err
	}

	rpm, tpm, err := r.getPerformanceStats(ctx, 0)
	if err != nil {
		return nil, err
	}
	stats.Rpm = rpm
	stats.Tpm = tpm

	return stats, nil
}

// GetDashboardStatsWithRange is the raw usage_logs fallback used when the
// pre-aggregation job is disabled. Totals cover [start, end); the "today" block
// is anchored on the caller's timezone exactly like GetDashboardStats.
func (r *usageLogRepository) GetDashboardStatsWithRange(ctx context.Context, start, end time.Time, userTZ string) (*DashboardStats, error) {
	startUTC := start.UTC()
	endUTC := end.UTC()
	if !endUTC.After(startUTC) {
		return nil, errors.New("统计时间范围无效")
	}

	stats := &DashboardStats{}
	now := timezone.Now()
	todayStart, todayEnd := timezone.TodayRangeInUserLocation(userTZ)

	if err := r.fillDashboardEntityStats(ctx, stats, todayStart, todayEnd, now); err != nil {
		return nil, err
	}
	if err := r.fillDashboardRangeStatsFromUsageLogs(ctx, stats, startUTC, endUTC); err != nil {
		return nil, err
	}
	if err := r.fillDashboardTodayStatsFromUsageLogs(ctx, stats, todayStart, todayEnd); err != nil {
		return nil, err
	}
	if err := r.fillDashboardHourlyActiveUsersFromUsageLogs(ctx, stats, now); err != nil {
		return nil, err
	}

	rpm, tpm, err := r.getPerformanceStats(ctx, 0)
	if err != nil {
		return nil, err
	}
	stats.Rpm = rpm
	stats.Tpm = tpm

	return stats, nil
}

func (r *usageLogRepository) fillDashboardEntityStats(ctx context.Context, stats *DashboardStats, todayStart, todayEnd, now time.Time) error {
	userStatsQuery := `
		SELECT
			COUNT(*) as total_users,
			COUNT(CASE WHEN created_at >= $1 AND created_at < $2 THEN 1 END) as today_new_users
		FROM users
		WHERE deleted_at IS NULL
	`
	if err := scanSingleRow(
		ctx,
		r.sql,
		userStatsQuery,
		[]any{todayStart, todayEnd},
		&stats.TotalUsers,
		&stats.TodayNewUsers,
	); err != nil {
		return err
	}

	apiKeyStatsQuery := `
		SELECT
			COUNT(*) as total_api_keys,
			COUNT(CASE WHEN status = $1 THEN 1 END) as active_api_keys
		FROM api_keys
		WHERE deleted_at IS NULL
	`
	if err := scanSingleRow(
		ctx,
		r.sql,
		apiKeyStatsQuery,
		[]any{service.StatusActive},
		&stats.TotalAPIKeys,
		&stats.ActiveAPIKeys,
	); err != nil {
		return err
	}

	accountStatsQuery := `
		SELECT
			COUNT(*) as total_accounts,
			COUNT(CASE WHEN status = $1 AND schedulable = true THEN 1 END) as normal_accounts,
			COUNT(CASE WHEN status = $2 THEN 1 END) as error_accounts,
			COUNT(CASE WHEN rate_limited_at IS NOT NULL AND rate_limit_reset_at > $3 THEN 1 END) as ratelimit_accounts,
			COUNT(CASE WHEN overload_until IS NOT NULL AND overload_until > $4 THEN 1 END) as overload_accounts
		FROM accounts
		WHERE deleted_at IS NULL
	`
	if err := scanSingleRow(
		ctx,
		r.sql,
		accountStatsQuery,
		[]any{service.StatusActive, service.StatusError, now, now},
		&stats.TotalAccounts,
		&stats.NormalAccounts,
		&stats.ErrorAccounts,
		&stats.RateLimitAccounts,
		&stats.OverloadAccounts,
	); err != nil {
		return err
	}

	return nil
}

// fillDashboardTotalStatsAggregated sums the whole daily rollup for the
// cumulative block. Timezone-independent: every bucket is counted.
func (r *usageLogRepository) fillDashboardTotalStatsAggregated(ctx context.Context, stats *DashboardStats) error {
	totalStatsQuery := `
		SELECT
			COALESCE(SUM(total_requests), 0) as total_requests,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as total_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as total_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) as total_cost,
			COALESCE(SUM(actual_cost), 0) as total_actual_cost,
			COALESCE(SUM(account_cost), 0) as total_account_cost,
			COALESCE(SUM(total_duration_ms), 0) as total_duration_ms
		FROM usage_dashboard_daily
	`
	var totalDurationMs int64
	if err := scanSingleRow(
		ctx,
		r.sql,
		totalStatsQuery,
		nil,
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheCreationTokens,
		&stats.TotalCacheReadTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&stats.TotalAccountCost,
		&totalDurationMs,
	); err != nil {
		return err
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheCreationTokens + stats.TotalCacheReadTokens
	if stats.TotalRequests > 0 {
		stats.AverageDurationMs = float64(totalDurationMs) / float64(stats.TotalRequests)
	}

	return nil
}

// fillDashboardTodayStatsAggregated sums the hourly rollup over the caller's
// local day [todayStart, todayEnd). The rollup buckets on server-timezone
// hours, so the local midnight must sit on a bucket boundary; a zone whose
// offset differs from the server's by a fraction of an hour (Asia/Kolkata
// against Asia/Shanghai, say) falls back to the raw usage_logs so the figure
// stays exact instead of silently dropping a partial bucket.
func (r *usageLogRepository) fillDashboardTodayStatsAggregated(ctx context.Context, stats *DashboardStats, todayStart, todayEnd time.Time) error {
	if !isBucketAligned(todayStart, "hour") || !isBucketAligned(todayEnd, "hour") {
		return r.fillDashboardTodayStatsFromUsageLogs(ctx, stats, todayStart, todayEnd)
	}

	todayStatsQuery := `
		SELECT
			COALESCE(SUM(total_requests), 0) as today_requests,
			COALESCE(SUM(input_tokens), 0) as today_input_tokens,
			COALESCE(SUM(output_tokens), 0) as today_output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as today_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as today_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) as today_cost,
			COALESCE(SUM(actual_cost), 0) as today_actual_cost,
			COALESCE(SUM(account_cost), 0) as today_account_cost
		FROM usage_dashboard_hourly
		WHERE bucket_start >= $1 AND bucket_start < $2
	`
	if err := scanSingleRow(
		ctx,
		r.sql,
		todayStatsQuery,
		[]any{todayStart, todayEnd},
		&stats.TodayRequests,
		&stats.TodayInputTokens,
		&stats.TodayOutputTokens,
		&stats.TodayCacheCreationTokens,
		&stats.TodayCacheReadTokens,
		&stats.TodayCost,
		&stats.TodayActualCost,
		&stats.TodayAccountCost,
	); err != nil {
		return err
	}
	stats.TodayTokens = stats.TodayInputTokens + stats.TodayOutputTokens + stats.TodayCacheCreationTokens + stats.TodayCacheReadTokens

	// Distinct users across the day's hour buckets. The daily rollup's
	// active_users is a server-day figure and cannot be re-cut per timezone.
	activeUsersQuery := `
		SELECT COUNT(DISTINCT user_id)
		FROM usage_dashboard_hourly_users
		WHERE bucket_start >= $1 AND bucket_start < $2
	`
	return scanSingleRow(ctx, r.sql, activeUsersQuery, []any{todayStart, todayEnd}, &stats.ActiveUsers)
}

// fillDashboardHourlyActiveUsersAggregated reads the current hour's distinct
// user count from the hourly rollup. Hour buckets are the same instants in any
// whole-hour-offset timezone, so no caller-timezone handling is needed.
func (r *usageLogRepository) fillDashboardHourlyActiveUsersAggregated(ctx context.Context, stats *DashboardStats, now time.Time) error {
	hourlyActiveQuery := `
		SELECT active_users
		FROM usage_dashboard_hourly
		WHERE bucket_start = $1
	`
	hourStart := now.In(timezone.Location()).Truncate(time.Hour)
	if err := scanSingleRow(ctx, r.sql, hourlyActiveQuery, []any{hourStart}, &stats.HourlyActiveUsers); err != nil {
		if err != sql.ErrNoRows {
			return err
		}
	}
	return nil
}

// fillDashboardRangeStatsFromUsageLogs fills the cumulative block from raw
// usage_logs over [startUTC, endUTC). Used when pre-aggregation is disabled.
func (r *usageLogRepository) fillDashboardRangeStatsFromUsageLogs(ctx context.Context, stats *DashboardStats, startUTC, endUTC time.Time) error {
	rangeStatsQuery := `
		SELECT
			COUNT(*) AS total_requests,
			COALESCE(SUM(input_tokens), 0) AS total_input_tokens,
			COALESCE(SUM(output_tokens), 0) AS total_output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) AS total_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) AS total_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) AS total_cost,
			COALESCE(SUM(actual_cost), 0) AS total_actual_cost,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) AS total_account_cost,
			COALESCE(SUM(COALESCE(duration_ms, 0)), 0) AS total_duration_ms
		FROM usage_logs
		WHERE created_at >= $1 AND created_at < $2
		  AND ` + usageLogBusinessStatsFilter("") + `
	`
	var totalDurationMs int64
	if err := scanSingleRow(
		ctx,
		r.sql,
		rangeStatsQuery,
		[]any{startUTC, endUTC},
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheCreationTokens,
		&stats.TotalCacheReadTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&stats.TotalAccountCost,
		&totalDurationMs,
	); err != nil {
		return err
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheCreationTokens + stats.TotalCacheReadTokens
	if stats.TotalRequests > 0 {
		stats.AverageDurationMs = float64(totalDurationMs) / float64(stats.TotalRequests)
	}
	return nil
}

// fillDashboardTodayStatsFromUsageLogs fills the "today" block and today's
// active-user count from raw usage_logs over the caller's local day. This is
// the same half-open window and business filter the usage-records page applies,
// so the two views agree by construction.
func (r *usageLogRepository) fillDashboardTodayStatsFromUsageLogs(ctx context.Context, stats *DashboardStats, todayStart, todayEnd time.Time) error {
	todayStatsQuery := `
		SELECT
			COUNT(*) AS today_requests,
			COALESCE(SUM(input_tokens), 0) AS today_input_tokens,
			COALESCE(SUM(output_tokens), 0) AS today_output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) AS today_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) AS today_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) AS today_cost,
			COALESCE(SUM(actual_cost), 0) AS today_actual_cost,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) AS today_account_cost,
			COUNT(DISTINCT user_id) AS active_users
		FROM usage_logs
		WHERE created_at >= $1 AND created_at < $2
		  AND ` + usageLogBusinessStatsFilter("") + `
	`
	if err := scanSingleRow(
		ctx,
		r.sql,
		todayStatsQuery,
		[]any{todayStart, todayEnd},
		&stats.TodayRequests,
		&stats.TodayInputTokens,
		&stats.TodayOutputTokens,
		&stats.TodayCacheCreationTokens,
		&stats.TodayCacheReadTokens,
		&stats.TodayCost,
		&stats.TodayActualCost,
		&stats.TodayAccountCost,
		&stats.ActiveUsers,
	); err != nil {
		return err
	}
	stats.TodayTokens = stats.TodayInputTokens + stats.TodayOutputTokens + stats.TodayCacheCreationTokens + stats.TodayCacheReadTokens
	return nil
}

// fillDashboardHourlyActiveUsersFromUsageLogs counts distinct users in the
// current clock hour from raw usage_logs.
func (r *usageLogRepository) fillDashboardHourlyActiveUsersFromUsageLogs(ctx context.Context, stats *DashboardStats, now time.Time) error {
	hourStart := now.UTC().Truncate(time.Hour)
	hourEnd := hourStart.Add(time.Hour)
	hourlyActiveQuery := `
		SELECT COUNT(DISTINCT user_id)
		FROM usage_logs
		WHERE created_at >= $1 AND created_at < $2
		  AND ` + usageLogBusinessStatsFilter("") + `
	`
	return scanSingleRow(ctx, r.sql, hourlyActiveQuery, []any{hourStart, hourEnd}, &stats.HourlyActiveUsers)
}

// UserDashboardStats 用户仪表盘统计
type UserDashboardStats = usagestats.UserDashboardStats

// PlatformDashboardStats 单平台用量明细
type PlatformDashboardStats = usagestats.PlatformDashboardStats

// GetUserDashboardStats 获取用户专属的仪表盘统计
//
// "今日"窗口是调用方时区的半开区间 [start, end)，与用量列表/统计接口共用同一口径；
// 没有上界的话，created_at 落在未来的行（写入端/DB 时钟偏差、补记账单）会永远算进"今日"。
func (r *usageLogRepository) GetUserDashboardStats(ctx context.Context, userID int64, userTZ string) (*UserDashboardStats, error) {
	stats := &UserDashboardStats{}
	todayStart, todayEnd := timezone.TodayRangeInUserLocation(userTZ)

	// API Key 统计
	if err := scanSingleRow(
		ctx,
		r.sql,
		"SELECT COUNT(*) FROM api_keys WHERE user_id = $1 AND deleted_at IS NULL",
		[]any{userID},
		&stats.TotalAPIKeys,
	); err != nil {
		return nil, err
	}
	if err := scanSingleRow(
		ctx,
		r.sql,
		"SELECT COUNT(*) FROM api_keys WHERE user_id = $1 AND status = $2 AND deleted_at IS NULL",
		[]any{userID, service.StatusActive},
		&stats.ActiveAPIKeys,
	); err != nil {
		return nil, err
	}

	// 累计 Token 统计
	totalStatsQuery := `
		SELECT
			COUNT(*) as total_requests,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as total_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as total_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) as total_cost,
			COALESCE(SUM(actual_cost), 0) as total_actual_cost,
			COALESCE(AVG(duration_ms), 0) as avg_duration_ms
		FROM usage_logs
		WHERE user_id = $1
		  AND ` + usageLogBusinessStatsFilter("") + `
	`
	if err := scanSingleRow(
		ctx,
		r.sql,
		totalStatsQuery,
		[]any{userID},
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheCreationTokens,
		&stats.TotalCacheReadTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&stats.AverageDurationMs,
	); err != nil {
		return nil, err
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheCreationTokens + stats.TotalCacheReadTokens

	// 今日 Token 统计
	todayStatsQuery := `
		SELECT
			COUNT(*) as today_requests,
			COALESCE(SUM(input_tokens), 0) as today_input_tokens,
			COALESCE(SUM(output_tokens), 0) as today_output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as today_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as today_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) as today_cost,
			COALESCE(SUM(actual_cost), 0) as today_actual_cost
		FROM usage_logs
		WHERE user_id = $1 AND created_at >= $2 AND created_at < $3
		  AND ` + usageLogBusinessStatsFilter("") + `
	`
	if err := scanSingleRow(
		ctx,
		r.sql,
		todayStatsQuery,
		[]any{userID, todayStart, todayEnd},
		&stats.TodayRequests,
		&stats.TodayInputTokens,
		&stats.TodayOutputTokens,
		&stats.TodayCacheCreationTokens,
		&stats.TodayCacheReadTokens,
		&stats.TodayCost,
		&stats.TodayActualCost,
	); err != nil {
		return nil, err
	}
	stats.TodayTokens = stats.TodayInputTokens + stats.TodayOutputTokens + stats.TodayCacheCreationTokens + stats.TodayCacheReadTokens

	// 性能指标：RPM 和 TPM（最近1分钟，仅统计该用户的请求）
	rpm, tpm, err := r.getPerformanceStats(ctx, userID)
	if err != nil {
		return nil, err
	}
	stats.Rpm = rpm
	stats.Tpm = tpm

	// 按"有效平台"维度拆分（group.platform 优先，否则 account.platform）。
	// 与 ops 路径口径一致；HAVING 过滤掉无法确定平台的行（避免出现空字符串平台）。
	// 与上面 totalStatsQuery/todayStatsQuery 的总值可能略微差异，原因有二：
	//   1) 无平台归属的极少数行（group/account 都没 platform）会被 HAVING 排除；
	//   2) usageLogSuccessFilterUL 会把 actual_cost = 0 且 billing_state = 0 的失败
	//      placeholder 行排除，而 totalStatsQuery/todayStatsQuery 没有这层过滤、会把这些行的
	//      request 计数算进去。定价缺失/价格已恢复行已确认发生过上游用量，不会被排除。
	platformQuery := `
		SELECT
			` + usageLogEffectivePlatformExpr + ` as platform,
			COUNT(*) as total_requests,
			COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens), 0) as total_tokens,
			COALESCE(SUM(ul.actual_cost), 0) as total_actual_cost,
			COUNT(*) FILTER (WHERE ul.created_at >= $2 AND ul.created_at < $3) as today_requests,
			COALESCE(SUM(ul.input_tokens + ul.output_tokens + ul.cache_creation_tokens + ul.cache_read_tokens) FILTER (WHERE ul.created_at >= $2 AND ul.created_at < $3), 0) as today_tokens,
			COALESCE(SUM(ul.actual_cost) FILTER (WHERE ul.created_at >= $2 AND ul.created_at < $3), 0) as today_actual_cost
		FROM usage_logs ul
		LEFT JOIN groups g ON g.id = ul.group_id
		LEFT JOIN accounts a ON a.id = ul.account_id
		WHERE ul.user_id = $1
		  AND ` + usageLogBusinessStatsFilter("ul") + `
		  AND ` + usageLogSuccessFilterUL + `
		GROUP BY ` + usageLogEffectivePlatformExpr + `
		HAVING ` + usageLogEffectivePlatformExpr + ` IS NOT NULL AND ` + usageLogEffectivePlatformExpr + ` <> ''
		ORDER BY total_actual_cost DESC
	`
	rows, err := r.sql.QueryContext(ctx, platformQuery, userID, todayStart, todayEnd)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p PlatformDashboardStats
		if err := rows.Scan(
			&p.Platform,
			&p.TotalRequests,
			&p.TotalTokens,
			&p.TotalActualCost,
			&p.TodayRequests,
			&p.TodayTokens,
			&p.TodayActualCost,
		); err != nil {
			_ = rows.Close()
			return nil, err
		}
		stats.ByPlatform = append(stats.ByPlatform, p)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return stats, nil
}

// getPerformanceStatsByAPIKey 获取指定 API Key 的 RPM 和 TPM（近5分钟平均值）
func (r *usageLogRepository) getPerformanceStatsByAPIKey(ctx context.Context, apiKeyID int64) (rpm, tpm int64, err error) {
	fiveMinutesAgo := time.Now().Add(-5 * time.Minute)
	query := `
		SELECT
			COUNT(*) as request_count,
			COALESCE(SUM(input_tokens + output_tokens + cache_creation_tokens + cache_read_tokens), 0) as token_count
		FROM usage_logs
		WHERE created_at >= $1 AND api_key_id = $2
		  AND ` + usageLogBusinessStatsFilter("")
	args := []any{fiveMinutesAgo, apiKeyID}

	var requestCount int64
	var tokenCount int64
	if err := scanSingleRow(ctx, r.sql, query, args, &requestCount, &tokenCount); err != nil {
		return 0, 0, err
	}
	return requestCount / 5, tokenCount / 5, nil
}

// GetAPIKeyDashboardStats 获取指定 API Key 的仪表盘统计（按 api_key_id 过滤）
func (r *usageLogRepository) GetAPIKeyDashboardStats(ctx context.Context, apiKeyID int64, userTZ string) (*UserDashboardStats, error) {
	stats := &UserDashboardStats{}
	todayStart, todayEnd := timezone.TodayRangeInUserLocation(userTZ)

	// API Key 维度不需要统计 key 数量，设为 1
	stats.TotalAPIKeys = 1
	stats.ActiveAPIKeys = 1

	// 累计 Token 统计
	totalStatsQuery := `
		SELECT
			COUNT(*) as total_requests,
			COALESCE(SUM(input_tokens), 0) as total_input_tokens,
			COALESCE(SUM(output_tokens), 0) as total_output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as total_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as total_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) as total_cost,
			COALESCE(SUM(actual_cost), 0) as total_actual_cost,
			COALESCE(AVG(duration_ms), 0) as avg_duration_ms
		FROM usage_logs
		WHERE api_key_id = $1
		  AND ` + usageLogBusinessStatsFilter("") + `
	`
	if err := scanSingleRow(
		ctx,
		r.sql,
		totalStatsQuery,
		[]any{apiKeyID},
		&stats.TotalRequests,
		&stats.TotalInputTokens,
		&stats.TotalOutputTokens,
		&stats.TotalCacheCreationTokens,
		&stats.TotalCacheReadTokens,
		&stats.TotalCost,
		&stats.TotalActualCost,
		&stats.AverageDurationMs,
	); err != nil {
		return nil, err
	}
	stats.TotalTokens = stats.TotalInputTokens + stats.TotalOutputTokens + stats.TotalCacheCreationTokens + stats.TotalCacheReadTokens

	// 今日 Token 统计
	todayStatsQuery := `
		SELECT
			COUNT(*) as today_requests,
			COALESCE(SUM(input_tokens), 0) as today_input_tokens,
			COALESCE(SUM(output_tokens), 0) as today_output_tokens,
			COALESCE(SUM(cache_creation_tokens), 0) as today_cache_creation_tokens,
			COALESCE(SUM(cache_read_tokens), 0) as today_cache_read_tokens,
			COALESCE(SUM(total_cost), 0) as today_cost,
			COALESCE(SUM(actual_cost), 0) as today_actual_cost
		FROM usage_logs
		WHERE api_key_id = $1 AND created_at >= $2 AND created_at < $3
		  AND ` + usageLogBusinessStatsFilter("") + `
	`
	if err := scanSingleRow(
		ctx,
		r.sql,
		todayStatsQuery,
		[]any{apiKeyID, todayStart, todayEnd},
		&stats.TodayRequests,
		&stats.TodayInputTokens,
		&stats.TodayOutputTokens,
		&stats.TodayCacheCreationTokens,
		&stats.TodayCacheReadTokens,
		&stats.TodayCost,
		&stats.TodayActualCost,
	); err != nil {
		return nil, err
	}
	stats.TodayTokens = stats.TodayInputTokens + stats.TodayOutputTokens + stats.TodayCacheCreationTokens + stats.TodayCacheReadTokens

	// 性能指标：RPM 和 TPM（最近5分钟，按 API Key 过滤）
	rpm, tpm, err := r.getPerformanceStatsByAPIKey(ctx, apiKeyID)
	if err != nil {
		return nil, err
	}
	stats.Rpm = rpm
	stats.Tpm = tpm

	return stats, nil
}
