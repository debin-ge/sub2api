package repository

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	gocache "github.com/patrickmn/go-cache"
)

const rawUsageLogModelColumn = "model"

// rawUsageLogModelColumn preserves the exact stored usage_logs.model semantics for direct filters.
// Historical rows may contain upstream/billing model values, while newer rows store requested_model.
// Requested/upstream/mapping analytics must use resolveModelDimensionExpression instead.

// usageLogSuccessFilterUL 用于把"失败请求 usage log"（tokens=0、cost=0、不计费的占位记录）
// 从统计性聚合中排除，避免污染 Dashboard / 用量拆分等指标。
//
// schema 中没有 success bool 列；新增列要做迁移，风险大；这里用 actual_cost > 0 作为代理：
// 任何成功落账的请求都会产生 actual_cost（包括 token 计费、纯图片 token 计费、按次/按图计费），
// 反之 failed-request usage log 的 actual_cost 为 0。
// 早期版本用 4 项 token 和 > 0 判定会把"按次/按图计费"与"image_output_tokens 独立计费"的纯图片
// 请求误判为失败，导致这部分请求从用量统计里消失，故改用 actual_cost。
// 配合 `FROM usage_logs ul` JOIN 查询使用。
const usageLogSuccessFilterUL = "ul.actual_cost > 0"

// usageLogEffectivePlatformExpr 用于按"有效平台"维度聚合 usage_logs：
// 优先取请求实际走的分组 platform，若分组未设置 platform 再 fallback 到 account.platform。
// 配套要求查询里 LEFT JOIN groups g ON g.id = ul.group_id 与 LEFT JOIN accounts a ON a.id = ul.account_id。
const usageLogEffectivePlatformExpr = "COALESCE(NULLIF(g.platform,''), a.platform)"

// dateFormatWhitelist 将 granularity 参数映射为 PostgreSQL TO_CHAR 格式字符串，防止外部输入直接拼入 SQL
var dateFormatWhitelist = map[string]string{
	"hour":  "YYYY-MM-DD HH24:00",
	"day":   "YYYY-MM-DD",
	"week":  "IYYY-IW",
	"month": "YYYY-MM",
}

// safeDateFormat 根据白名单获取 dateFormat，未匹配时返回默认值
func safeDateFormat(granularity string) string {
	if f, ok := dateFormatWhitelist[granularity]; ok {
		return f
	}
	return "YYYY-MM-DD"
}

// appendRawUsageLogModelWhereCondition keeps direct model filters on the raw model column for backward
// compatibility with historical rows. Requested/upstream analytics must use
// resolveModelDimensionExpression instead.
func appendRawUsageLogModelWhereCondition(conditions []string, args []any, model string) ([]string, []any) {
	if strings.TrimSpace(model) == "" {
		return conditions, args
	}
	conditions = append(conditions, fmt.Sprintf("%s = $%d", rawUsageLogModelColumn, len(args)+1))
	args = append(args, model)
	return conditions, args
}

func appendUsageLogBillingModeWhereCondition(conditions []string, args []any, billingMode string) ([]string, []any) {
	return appendUsageLogBillingModeWhereConditionWithAlias(conditions, args, billingMode, "")
}

func appendUsageLogBillingModeWhereConditionWithAlias(conditions []string, args []any, billingMode string, alias string) ([]string, []any) {
	mode := strings.TrimSpace(billingMode)
	if mode == "" {
		return conditions, args
	}
	column := func(name string) string {
		if alias == "" {
			return name
		}
		return alias + "." + name
	}
	placeholder := fmt.Sprintf("$%d", len(args)+1)
	switch service.BillingMode(mode) {
	case service.BillingModeImage:
		conditions = append(conditions, fmt.Sprintf("(%s = %s OR ((%s IS NULL OR %s = '') AND COALESCE(%s, 0) > 0))", column("billing_mode"), placeholder, column("billing_mode"), column("billing_mode"), column("image_count")))
	case service.BillingModeVideo:
		conditions = append(conditions, fmt.Sprintf("%s = %s", column("billing_mode"), placeholder))
	case service.BillingModeToken:
		conditions = append(conditions, fmt.Sprintf("(%s = %s OR ((%s IS NULL OR %s = '') AND COALESCE(%s, 0) <= 0))", column("billing_mode"), placeholder, column("billing_mode"), column("billing_mode"), column("image_count")))
	default:
		conditions = append(conditions, fmt.Sprintf("%s = %s", column("billing_mode"), placeholder))
	}
	args = append(args, mode)
	return conditions, args
}

func appendUsageLogBillingModeQueryFilter(query string, args []any, billingMode string, alias string) (string, []any) {
	conditions, args := appendUsageLogBillingModeWhereConditionWithAlias(nil, args, billingMode, alias)
	if len(conditions) == 0 {
		return query, args
	}
	return query + " AND " + conditions[0], args
}

func appendUsageLogModelWhereCondition(conditions []string, args []any, model string, source string) ([]string, []any) {
	if strings.TrimSpace(source) == "" {
		return appendRawUsageLogModelWhereCondition(conditions, args, model)
	}
	if strings.TrimSpace(model) == "" {
		return conditions, args
	}
	conditions = append(conditions, fmt.Sprintf("%s = $%d", resolveModelDimensionExpression(source), len(args)+1))
	args = append(args, model)
	return conditions, args
}

// appendRawUsageLogModelQueryFilter keeps direct model filters on the raw model column for backward
// compatibility with historical rows. Requested/upstream analytics must use
// resolveModelDimensionExpression instead.
func appendRawUsageLogModelQueryFilter(query string, args []any, model string) (string, []any) {
	if strings.TrimSpace(model) == "" {
		return query, args
	}
	query += fmt.Sprintf(" AND %s = $%d", rawUsageLogModelColumn, len(args)+1)
	args = append(args, model)
	return query, args
}

func appendUsageLogModelQueryFilter(query string, args []any, model string, source string) (string, []any) {
	if strings.TrimSpace(source) == "" {
		return appendRawUsageLogModelQueryFilter(query, args, model)
	}
	if strings.TrimSpace(model) == "" {
		return query, args
	}
	query += fmt.Sprintf(" AND %s = $%d", resolveModelDimensionExpression(source), len(args)+1)
	args = append(args, model)
	return query, args
}

type usageLogRepository struct {
	client *dbent.Client
	sql    sqlExecutor
	db     *sql.DB

	createBatchOnce     sync.Once
	createBatchCh       chan usageLogCreateRequest
	bestEffortBatchOnce sync.Once
	bestEffortBatchCh   chan usageLogBestEffortRequest
	bestEffortRecent    *gocache.Cache
}

func NewUsageLogRepository(client *dbent.Client, sqlDB *sql.DB) *usageLogRepository {
	return newUsageLogRepositoryWithSQL(client, sqlDB)
}

func newUsageLogRepositoryWithSQL(client *dbent.Client, sqlq sqlExecutor) *usageLogRepository {
	// 使用 scanSingleRow 替代 QueryRowContext，保证 ent.Tx 作为 sqlExecutor 可用。
	repo := &usageLogRepository{client: client, sql: sqlq}
	if db, ok := sqlq.(*sql.DB); ok {
		repo.db = db
	}
	repo.bestEffortRecent = gocache.New(usageLogBestEffortRecentTTL, time.Minute)
	return repo
}

// getPerformanceStats 获取 RPM 和 TPM（近5分钟平均值，可选按用户过滤）
// UserStats 用户使用统计
// DashboardStats 仪表盘统计
// GetUserStatsAggregated returns aggregated usage statistics for a user using database-level aggregation
// GetAPIKeyStatsAggregated returns aggregated usage statistics for an API key using database-level aggregation
// GetAccountStatsAggregated 使用 SQL 聚合统计账号使用数据
//
// 性能优化说明：
// 原实现先查询所有日志记录，再在应用层循环计算统计值：
// 1. 需要传输大量数据到应用层
// 2. 应用层循环计算增加 CPU 和内存开销
//
// 新实现使用 SQL 聚合函数：
// 1. 在数据库层完成 COUNT/SUM/AVG 计算
// 2. 只返回单行聚合结果，大幅减少数据传输量
// 3. 利用数据库索引优化聚合查询性能
// GetModelStatsAggregated 使用 SQL 聚合统计模型使用数据
// 性能优化：数据库层聚合计算，避免应用层循环统计
// GetDailyStatsAggregated 使用 SQL 聚合统计用户的每日使用数据
// 性能优化：使用 GROUP BY 在数据库层按日期分组聚合，避免应用层循环分组统计
// resolveUsageStatsTimezone 获取用于 SQL 分组的时区名称。
// 优先使用应用初始化的时区，其次尝试读取 TZ 环境变量，最后回落为 UTC。
// GetAccountTodayStats 获取账号今日统计
// GetAccountWindowStats 获取账号时间窗口内的统计
// GetAccountWindowStatsBatch 批量获取同一窗口起点下多个账号的统计数据。
// 返回 map[accountID]*AccountStats，未命中的账号会返回零值统计，便于上层直接复用。
// GetAccountModelBreakdownBatch groups account-window usage by the raw
// usage_logs.model value. Account cost and zero-cost row semantics deliberately
// match GetAccountWindowStats.
func (r *usageLogRepository) GetAccountModelBreakdownBatch(ctx context.Context, accountIDs []int64, startTime time.Time) (map[int64]map[string]service.ModelCostStats, error) {
	result := make(map[int64]map[string]service.ModelCostStats)
	normalizedAccountIDs := normalizePositiveInt64IDs(accountIDs)
	if len(normalizedAccountIDs) == 0 {
		return result, nil
	}

	query := `
		SELECT
			account_id,
			model,
			COUNT(*) as requests,
			COALESCE(SUM(input_tokens::bigint + output_tokens::bigint + cache_creation_tokens::bigint + cache_read_tokens::bigint), 0) as tokens,
			COALESCE(SUM(COALESCE(account_stats_cost, total_cost) * COALESCE(account_rate_multiplier, 1)), 0) as account_cost
		FROM usage_logs
		WHERE account_id = ANY($1) AND created_at >= $2
		GROUP BY account_id, model
	`
	rows, err := r.sql.QueryContext(ctx, query, pq.Array(normalizedAccountIDs), startTime)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			accountID int64
			model     string
			stats     service.ModelCostStats
		)
		if err := rows.Scan(&accountID, &model, &stats.Requests, &stats.Tokens, &stats.AccountCost); err != nil {
			return nil, err
		}
		if result[accountID] == nil {
			result[accountID] = make(map[string]service.ModelCostStats)
		}
		result[accountID][model] = stats
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	return result, nil
}

// GetAccountModelBreakdownByWindowBatch aggregates every account inside its
// own provider reset-bound quota period, ending at the passive snapshot's
// sampled_at timestamp. Radar uses the provider-equivalent cost without the
// account billing multiplier.
func (r *usageLogRepository) GetAccountModelBreakdownByWindowBatch(
	ctx context.Context,
	windows []service.RadarQuotaAccountWindow,
) (map[int64]map[string]service.ModelCostStats, error) {
	result := make(map[int64]map[string]service.ModelCostStats)
	normalized := normalizeRadarQuotaAccountWindows(windows)
	if len(normalized) == 0 {
		return result, nil
	}

	accountIDs := make([]int64, len(normalized))
	startTimes := make([]time.Time, len(normalized))
	endTimes := make([]time.Time, len(normalized))
	for i, window := range normalized {
		accountIDs[i] = window.AccountID
		startTimes[i] = window.StartAt
		endTimes[i] = window.EndAt
	}

	query := `
		WITH quota_windows AS (
			SELECT *
			FROM UNNEST($1::bigint[], $2::timestamptz[], $3::timestamptz[])
				AS quota_window(account_id, start_at, end_at)
		)
		SELECT
			usage_logs.account_id,
			usage_logs.model,
			COUNT(*) as requests,
			COALESCE(SUM(usage_logs.input_tokens::bigint + usage_logs.output_tokens::bigint + usage_logs.cache_creation_tokens::bigint + usage_logs.cache_read_tokens::bigint), 0) as tokens,
			COALESCE(SUM(COALESCE(usage_logs.account_stats_cost, usage_logs.total_cost)), 0) as account_cost
		FROM quota_windows
		JOIN usage_logs
			ON usage_logs.account_id = quota_windows.account_id
			AND usage_logs.created_at >= quota_windows.start_at
			AND usage_logs.created_at < quota_windows.end_at
		GROUP BY usage_logs.account_id, usage_logs.model
	`
	rows, err := r.sql.QueryContext(ctx, query, pq.Array(accountIDs), pq.Array(startTimes), pq.Array(endTimes))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			accountID int64
			model     string
			stats     service.ModelCostStats
		)
		if err := rows.Scan(&accountID, &model, &stats.Requests, &stats.Tokens, &stats.AccountCost); err != nil {
			return nil, err
		}
		if result[accountID] == nil {
			result[accountID] = make(map[string]service.ModelCostStats)
		}
		result[accountID][model] = stats
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return result, nil
}

func normalizeRadarQuotaAccountWindows(windows []service.RadarQuotaAccountWindow) []service.RadarQuotaAccountWindow {
	byAccountID := make(map[int64]service.RadarQuotaAccountWindow, len(windows))
	for _, window := range windows {
		startAt := window.StartAt.UTC()
		endAt := window.EndAt.UTC()
		if window.AccountID <= 0 || !startAt.Before(endAt) {
			continue
		}
		if _, exists := byAccountID[window.AccountID]; exists {
			continue
		}
		window.StartAt = startAt
		window.EndAt = endAt
		byAccountID[window.AccountID] = window
	}

	normalized := make([]service.RadarQuotaAccountWindow, 0, len(byAccountID))
	for _, window := range byAccountID {
		normalized = append(normalized, window)
	}
	sort.Slice(normalized, func(i, j int) bool { return normalized[i].AccountID < normalized[j].AccountID })
	return normalized
}

// GetGeminiUsageTotalsBatch 批量聚合 Gemini 账号在窗口内的 Pro/Flash 请求与用量。
// 模型分类规则与 service.geminiModelClassFromName 一致：model 包含 flash/lite 视为 flash，其余视为 pro。
// TrendDataPoint represents a single point in trend data
// ModelStat represents usage statistics for a single model
// UserUsageTrendPoint represents user usage trend data point
// UserSpendingRankingItem represents a user spending ranking row.
// APIKeyUsageTrendPoint represents API key usage trend data point
// GetAPIKeyUsageTrend returns usage trend data grouped by API key and date
// GetUserUsageTrend returns usage trend data grouped by user and date
// GetUserSpendingRanking returns user spending ranking aggregated within the time range.
// UserDashboardStats 用户仪表盘统计
// PlatformDashboardStats 单平台用量明细
// GetUserDashboardStats 获取用户专属的仪表盘统计
// getPerformanceStatsByAPIKey 获取指定 API Key 的 RPM 和 TPM（近5分钟平均值）
// GetAPIKeyDashboardStats 获取指定 API Key 的仪表盘统计（按 api_key_id 过滤）
// GetUserUsageTrendByUserID 获取指定用户的使用趋势
// GetUserModelStats 获取指定用户的模型统计
// UsageLogFilters represents filters for usage log queries
// ListWithFilters lists usage logs with optional filters (for admin)
// UsageStats represents usage statistics
// BatchUserUsageStats represents usage stats for a single user
// PlatformUsage represents per-platform usage breakdown
// GetBatchUserUsageStats gets today and total actual_cost for multiple users within a time range.
// If startTime is zero, defaults to 30 days ago.
// BatchAPIKeyUsageStats represents usage stats for a single API key
// GetBatchAPIKeyUsageStats gets today and total actual_cost for multiple API keys within a time range.
// If startTime is zero, defaults to 30 days ago.
// GetUsageTrendWithFilters returns usage trend data with optional filters
// GetModelStatsWithFilters returns model statistics with optional filters
// GetModelStatsWithFiltersBySource returns model statistics with optional filters and model source dimension.
// source: requested | upstream | mapping.
// GetGroupStatsWithFilters returns group usage statistics with optional filters
// GetUserBreakdownStats returns per-user usage breakdown within a specific dimension.
// GetAllGroupUsageSummary returns today's and cumulative actual_cost for every group.
// todayStart is the start-of-day in the caller's timezone (UTC-based).
// TODO(perf): This query scans ALL usage_logs rows for total_cost aggregation.
// When usage_logs exceeds ~1M rows, consider adding a short-lived cache (30s)
// or a materialized view / pre-aggregation table for cumulative costs.
// resolveModelDimensionExpression maps model source type to a safe SQL expression.
// resolveEndpointColumn maps endpoint type to the corresponding DB column name.
// GetGlobalStats gets usage statistics for all users within a time range
// GetStatsWithFilters gets usage statistics with optional filters
// AccountUsageHistory represents daily usage history for an account
// AccountUsageSummary represents summary statistics for an account
// AccountUsageStatsResponse represents the full usage statistics response for an account
// EndpointStat represents endpoint usage statistics row.
// GetEndpointStatsWithFilters returns inbound endpoint statistics with optional filters.
// GetUpstreamEndpointStatsWithFilters returns upstream endpoint statistics with optional filters.
// GetAccountUsageStats returns comprehensive usage statistics for an account over a time range
func buildWhere(conditions []string) string {
	if len(conditions) == 0 {
		return ""
	}
	return "WHERE " + strings.Join(conditions, " AND ")
}

func appendRequestTypeOrStreamWhereCondition(conditions []string, args []any, requestType *int16, stream *bool) ([]string, []any) {
	if requestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(len(args)+1, *requestType)
		conditions = append(conditions, condition)
		args = append(args, conditionArgs...)
		return conditions, args
	}
	if stream != nil {
		conditions = append(conditions, fmt.Sprintf("stream = $%d", len(args)+1))
		args = append(args, *stream)
	}
	return conditions, args
}

func appendRequestTypeOrStreamQueryFilter(query string, args []any, requestType *int16, stream *bool) (string, []any) {
	if requestType != nil {
		condition, conditionArgs := buildRequestTypeFilterCondition(len(args)+1, *requestType)
		query += " AND " + condition
		args = append(args, conditionArgs...)
		return query, args
	}
	if stream != nil {
		query += fmt.Sprintf(" AND stream = $%d", len(args)+1)
		args = append(args, *stream)
	}
	return query, args
}

// buildRequestTypeFilterCondition 在 request_type 过滤时兼容 legacy 字段，避免历史数据漏查。
func buildRequestTypeFilterCondition(startArgIndex int, requestType int16) (string, []any) {
	return buildRequestTypeFilterConditionWithAlias(startArgIndex, requestType, "")
}

func buildRequestTypeFilterConditionWithAlias(startArgIndex int, requestType int16, alias string) (string, []any) {
	normalized := service.RequestTypeFromInt16(requestType)
	requestTypeArg := int16(normalized)
	prefix := ""
	if alias != "" {
		prefix = alias + "."
	}
	switch normalized {
	case service.RequestTypeSync:
		return fmt.Sprintf("(%srequest_type = $%d OR (%srequest_type = %d AND %sstream = FALSE AND %sopenai_ws_mode = FALSE))", prefix, startArgIndex, prefix, int16(service.RequestTypeUnknown), prefix, prefix), []any{requestTypeArg}
	case service.RequestTypeStream:
		return fmt.Sprintf("(%srequest_type = $%d OR (%srequest_type = %d AND %sstream = TRUE AND %sopenai_ws_mode = FALSE))", prefix, startArgIndex, prefix, int16(service.RequestTypeUnknown), prefix, prefix), []any{requestTypeArg}
	case service.RequestTypeWSV2:
		return fmt.Sprintf("(%srequest_type = $%d OR (%srequest_type = %d AND %sopenai_ws_mode = TRUE))", prefix, startArgIndex, prefix, int16(service.RequestTypeUnknown), prefix), []any{requestTypeArg}
	default:
		return fmt.Sprintf("%srequest_type = $%d", prefix, startArgIndex), []any{requestTypeArg}
	}
}

// GetPublicModelRecentCallCounts 返回 since 时间之后每个模型的调用次数（按 requested_model 归口）。
// 用于公开模型广场排序。空 requested_model 回落到 model 字段，以覆盖旧数据。
func (r *usageLogRepository) GetPublicModelRecentCallCounts(
	ctx context.Context,
	since time.Time,
) (map[string]int64, error) {
	if r == nil || r.sql == nil {
		return nil, nil
	}
	query := `
		SELECT
			COALESCE(NULLIF(TRIM(requested_model), ''), model) AS model_key,
			COUNT(*)::bigint AS call_count
		FROM usage_logs
		WHERE created_at >= $1
		GROUP BY model_key`
	rows, err := r.sql.QueryContext(ctx, query, since.UTC())
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string]int64, 128)
	for rows.Next() {
		var (
			modelKey sql.NullString
			count    int64
		)
		if err := rows.Scan(&modelKey, &count); err != nil {
			return nil, err
		}
		key := strings.TrimSpace(modelKey.String)
		if key == "" {
			continue
		}
		out[key] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
