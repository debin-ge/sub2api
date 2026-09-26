package admin

import (
	"context"
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

// dashboardUsageRepoLimitProbe 记录趋势查询实际收到的 limit，用于验证 handler 层夹值。
type dashboardUsageRepoLimitProbe struct {
	service.UsageLogRepository
	lastUsersLimit   atomic.Int64
	lastAPIKeysLimit atomic.Int64
}

func (r *dashboardUsageRepoLimitProbe) GetUserUsageTrend(
	_ context.Context,
	_, _ time.Time,
	_ string,
	limit int,
) ([]usagestats.UserUsageTrendPoint, error) {
	r.lastUsersLimit.Store(int64(limit))
	return []usagestats.UserUsageTrendPoint{}, nil
}

func (r *dashboardUsageRepoLimitProbe) GetAPIKeyUsageTrend(
	_ context.Context,
	_, _ time.Time,
	_ string,
	limit int,
) ([]usagestats.APIKeyUsageTrendPoint, error) {
	r.lastAPIKeysLimit.Store(int64(limit))
	return []usagestats.APIKeyUsageTrendPoint{}, nil
}

func TestParseDashboardTrendLimitClampsRange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cases := []struct {
		name  string
		query string
		def   int
		want  int
	}{
		{name: "missing uses default", query: "", def: 12, want: 12},
		{name: "non numeric uses default", query: "limit=abc", def: 5, want: 5},
		{name: "zero uses default", query: "limit=0", def: 5, want: 5},
		{name: "negative uses default", query: "limit=-3", def: 12, want: 12},
		{name: "normal value kept", query: "limit=50", def: 12, want: 50},
		{name: "upper bound kept", query: "limit=1000", def: 12, want: 1000},
		{name: "beyond upper bound clamps", query: "limit=1000000", def: 12, want: 1000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/x?"+tc.query, nil)
			require.Equal(t, tc.want, parseDashboardTrendLimit(c, tc.def))
		})
	}
}

func TestDashboardHandler_TrendEndpointsClampLimit(t *testing.T) {
	t.Cleanup(resetDashboardReadCachesForTest)
	resetDashboardReadCachesForTest()

	gin.SetMode(gin.TestMode)
	repo := &dashboardUsageRepoLimitProbe{}
	dashboardSvc := service.NewDashboardService(repo, nil, nil, nil)
	handler := NewDashboardHandler(dashboardSvc, nil)
	router := gin.New()
	router.GET("/admin/dashboard/users-trend", handler.GetUserUsageTrend)
	router.GET("/admin/dashboard/api-keys-trend", handler.GetAPIKeyUsageTrend)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/dashboard/users-trend?start_date=2026-03-01&end_date=2026-03-07&granularity=day&limit=99999999", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(dashboardTrendMaxLimit), repo.lastUsersLimit.Load())

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"/admin/dashboard/api-keys-trend?start_date=2026-03-01&end_date=2026-03-07&granularity=day&limit=-1", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(5), repo.lastAPIKeysLimit.Load(), "invalid limit falls back to the endpoint default")
}
