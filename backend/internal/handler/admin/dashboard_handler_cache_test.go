package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type dashboardUsageRepoCacheProbe struct {
	service.UsageLogRepository
	trendCalls      atomic.Int32
	usersTrendCalls atomic.Int32
	statsCalls      atomic.Int32
}

func (r *dashboardUsageRepoCacheProbe) GetDashboardStats(ctx context.Context, userTZ string) (*usagestats.DashboardStats, error) {
	r.statsCalls.Add(1)
	return &usagestats.DashboardStats{TotalUsers: 1}, nil
}

func (r *dashboardUsageRepoCacheProbe) GetUsageTrendWithFilters(
	ctx context.Context,
	startTime, endTime time.Time,
	granularity string,
	userID, apiKeyID, accountID, groupID int64,
	model string,
	requestType *int16,
	stream *bool,
	billingType *int8,
) ([]usagestats.TrendDataPoint, error) {
	r.trendCalls.Add(1)
	return []usagestats.TrendDataPoint{{
		Date:        "2026-03-11",
		Requests:    1,
		TotalTokens: 2,
		Cost:        3,
		ActualCost:  4,
	}}, nil
}

func (r *dashboardUsageRepoCacheProbe) GetUserUsageTrend(
	ctx context.Context,
	startTime, endTime time.Time,
	granularity string,
	_ string,
	limit int,
) ([]usagestats.UserUsageTrendPoint, error) {
	r.usersTrendCalls.Add(1)
	return []usagestats.UserUsageTrendPoint{{
		Date:       "2026-03-11",
		UserID:     1,
		Email:      "cache@test.dev",
		Requests:   2,
		Tokens:     20,
		Cost:       2,
		ActualCost: 1,
	}}, nil
}

func resetDashboardReadCachesForTest() {
	dashboardTrendCache = newSnapshotCache(30 * time.Second)
	dashboardUsersTrendCache = newSnapshotCache(30 * time.Second)
	dashboardAPIKeysTrendCache = newSnapshotCache(30 * time.Second)
	dashboardModelStatsCache = newSnapshotCache(30 * time.Second)
	dashboardGroupStatsCache = newSnapshotCache(30 * time.Second)
	dashboardSnapshotV2Cache = newSnapshotCache(30 * time.Second)
}

func TestDashboardHandler_GetUsageTrend_UsesCache(t *testing.T) {
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()

	gin.SetMode(gin.TestMode)
	repo := &dashboardUsageRepoCacheProbe{}
	dashboardSvc := service.NewDashboardService(repo, nil, nil, nil)
	handler := NewDashboardHandler(dashboardSvc, nil)
	router := gin.New()
	router.GET("/admin/dashboard/trend", handler.GetUsageTrend)

	req1 := httptest.NewRequest(http.MethodGet, "/admin/dashboard/trend?start_date=2026-03-01&end_date=2026-03-07&granularity=day", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)
	require.Equal(t, "miss", rec1.Header().Get("X-Snapshot-Cache"))

	req2 := httptest.NewRequest(http.MethodGet, "/admin/dashboard/trend?start_date=2026-03-01&end_date=2026-03-07&granularity=day", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	require.Equal(t, "hit", rec2.Header().Get("X-Snapshot-Cache"))
	require.Equal(t, int32(1), repo.trendCalls.Load())
}

func TestDashboardHandler_GetUserUsageTrend_UsesCache(t *testing.T) {
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()

	gin.SetMode(gin.TestMode)
	repo := &dashboardUsageRepoCacheProbe{}
	dashboardSvc := service.NewDashboardService(repo, nil, nil, nil)
	handler := NewDashboardHandler(dashboardSvc, nil)
	router := gin.New()
	router.GET("/admin/dashboard/users-trend", handler.GetUserUsageTrend)

	req1 := httptest.NewRequest(http.MethodGet, "/admin/dashboard/users-trend?start_date=2026-03-01&end_date=2026-03-07&granularity=day&limit=8", nil)
	rec1 := httptest.NewRecorder()
	router.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusOK, rec1.Code)
	require.Equal(t, "miss", rec1.Header().Get("X-Snapshot-Cache"))

	req2 := httptest.NewRequest(http.MethodGet, "/admin/dashboard/users-trend?start_date=2026-03-01&end_date=2026-03-07&granularity=day&limit=8", nil)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)
	require.Equal(t, "hit", rec2.Header().Get("X-Snapshot-Cache"))
	require.Equal(t, int32(1), repo.usersTrendCalls.Load())
}

// The stats block is cut on the caller's local day, so two admins whose
// timezones put "today" at different instants must not share a snapshot entry,
// while zones with the same offset (same window) still do.
func TestDashboardHandler_GetSnapshotV2_StatsCacheScopedByTimezone(t *testing.T) {
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()

	gin.SetMode(gin.TestMode)
	repo := &dashboardUsageRepoCacheProbe{}
	dashboardSvc := service.NewDashboardService(repo, nil, nil, nil)
	handler := NewDashboardHandler(dashboardSvc, nil)
	router := gin.New()
	router.GET("/admin/dashboard/snapshot-v2", handler.GetSnapshotV2)

	get := func(tz string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet,
			"/admin/dashboard/snapshot-v2?start_date=2026-03-01&end_date=2026-03-07&granularity=day&include_trend=false&include_model_stats=false&timezone="+tz, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		return rec
	}

	require.Equal(t, "miss", get("Asia/Shanghai").Header().Get("X-Snapshot-Cache"))
	require.Equal(t, int32(1), repo.statsCalls.Load())

	// Same offset, same today-window: served from the same entry.
	require.Equal(t, "hit", get("Asia/Singapore").Header().Get("X-Snapshot-Cache"))
	require.Equal(t, int32(1), repo.statsCalls.Load())

	// Different today-window: recomputed for that caller.
	require.Equal(t, "miss", get("UTC").Header().Get("X-Snapshot-Cache"))
	require.Equal(t, int32(2), repo.statsCalls.Load())
}

// Trend labels follow the caller's timezone, so identical UTC bounds requested
// from different zones must not share a cache entry.
func TestDashboardHandler_GetUserUsageTrend_CacheScopedByTimezone(t *testing.T) {
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()

	gin.SetMode(gin.TestMode)
	repo := &dashboardUsageRepoCacheProbe{}
	dashboardSvc := service.NewDashboardService(repo, nil, nil, nil)
	handler := NewDashboardHandler(dashboardSvc, nil)
	router := gin.New()
	router.GET("/admin/dashboard/users-trend", handler.GetUserUsageTrend)

	get := func(tz string) string {
		req := httptest.NewRequest(http.MethodGet,
			"/admin/dashboard/users-trend?start_time=2026-03-01T00:00:00Z&end_time=2026-03-08T00:00:00Z&granularity=day&timezone="+tz, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code)
		return rec.Header().Get("X-Snapshot-Cache")
	}

	require.Equal(t, "miss", get("Asia/Shanghai"))
	require.Equal(t, "hit", get("Asia/Shanghai"))
	require.Equal(t, "miss", get("America/Los_Angeles"))
	require.Equal(t, int32(2), repo.usersTrendCalls.Load())
}

// The snapshot echoes the window as calendar dates in the caller's timezone,
// naming the last day the exclusive end still covers.
func TestDashboardHandler_GetSnapshotV2_EchoesDatesInCallerTimezone(t *testing.T) {
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()

	gin.SetMode(gin.TestMode)
	repo := &dashboardUsageRepoCacheProbe{}
	dashboardSvc := service.NewDashboardService(repo, nil, nil, nil)
	handler := NewDashboardHandler(dashboardSvc, nil)
	router := gin.New()
	router.GET("/admin/dashboard/snapshot-v2", handler.GetSnapshotV2)

	req := httptest.NewRequest(http.MethodGet,
		"/admin/dashboard/snapshot-v2?start_time=2026-03-01T03:00:00Z&end_time=2026-03-08T03:00:00Z&include_stats=false&include_trend=false&include_model_stats=false&timezone=America/Los_Angeles", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Data struct {
			StartDate string `json:"start_date"`
			EndDate   string `json:"end_date"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	// 03:00Z is still the previous evening in Los Angeles.
	require.Equal(t, "2026-02-28", body.Data.StartDate)
	require.Equal(t, "2026-03-07", body.Data.EndDate)
}
