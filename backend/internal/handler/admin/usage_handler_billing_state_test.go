package admin

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/pkg/usagestats"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func newAdminUsageBillingStateTestRouter(repo *adminUsageRepoCapture) *gin.Engine {
	gin.SetMode(gin.TestMode)
	usageSvc := service.NewUsageService(repo, nil, nil, nil)
	handler := NewUsageHandler(usageSvc, nil, nil, nil)
	router := gin.New()
	router.GET("/admin/usage", handler.List)
	router.GET("/admin/usage/stats", handler.Stats)
	return router
}

func TestAdminUsageListFiltersUnsettled(t *testing.T) {
	repo := &adminUsageRepoCapture{}
	router := newAdminUsageBillingStateTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage?billing_state_unsettled=true", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.listFilters.BillingStateUnsettled)
	require.Nil(t, repo.listFilters.BillingState, "只看未结算是开区间，不该被收窄成某一档")
}

func TestAdminUsageListFiltersExactBillingState(t *testing.T) {
	repo := &adminUsageRepoCapture{}
	router := newAdminUsageBillingStateTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage?billing_state=1", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, repo.listFilters.BillingState)
	require.Equal(t, service.BillingStatePricingUnavailable, *repo.listFilters.BillingState)
	require.False(t, repo.listFilters.BillingStateUnsettled)
}

func TestAdminUsageStatsFiltersBillingState(t *testing.T) {
	repo := &adminUsageRepoCapture{}
	router := newAdminUsageBillingStateTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage/stats?billing_state_unsettled=true&billing_state=2", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.statsFilters.BillingStateUnsettled)
	require.NotNil(t, repo.statsFilters.BillingState)
	require.Equal(t, service.BillingStatePricingRecovered, *repo.statsFilters.BillingState)
}

// 打错的档位必须报错而不是静默返回空集：这个视图的用途就是"看得见欠账"，
// 一个空列表会被读成"没有欠账"。
func TestAdminUsageRejectsInvalidBillingStateFilters(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"out of range state", "billing_state=9"},
		{"non numeric state", "billing_state=pending"},
		{"non boolean unsettled", "billing_state_unsettled=maybe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{"/admin/usage?", "/admin/usage/stats?"} {
				repo := &adminUsageRepoCapture{}
				router := newAdminUsageBillingStateTestRouter(repo)

				req := httptest.NewRequest(http.MethodGet, path+tc.query, nil)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, req)

				require.Equal(t, http.StatusBadRequest, rec.Code, path)
			}
		})
	}
}

func TestAdminUsageNoBillingStateFilterByDefault(t *testing.T) {
	repo := &adminUsageRepoCapture{}
	router := newAdminUsageBillingStateTestRouter(repo)

	req := httptest.NewRequest(http.MethodGet, "/admin/usage", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Nil(t, repo.listFilters.BillingState)
	require.False(t, repo.listFilters.BillingStateUnsettled)
}

// 缓存键漏字段会让待结算统计复用全量统计的结果，把"有欠账"读成"没欠账"。
func TestUsageStatsCacheKeyDistinguishesBillingStateFilters(t *testing.T) {
	pending := service.BillingStatePricingUnavailable
	recovered := service.BillingStatePricingRecovered

	base := usagestats.UsageLogFilters{}
	unsettled := usagestats.UsageLogFilters{BillingStateUnsettled: true}
	exactPending := usagestats.UsageLogFilters{BillingState: &pending}
	exactRecovered := usagestats.UsageLogFilters{BillingState: &recovered}

	keys := map[string]string{
		"base":            usageStatsCacheKey(base),
		"unsettled":       usageStatsCacheKey(unsettled),
		"exact_pending":   usageStatsCacheKey(exactPending),
		"exact_recovered": usageStatsCacheKey(exactRecovered),
	}
	seen := make(map[string]string, len(keys))
	for name, key := range keys {
		if other, dup := seen[key]; dup {
			t.Fatalf("%s 与 %s 共用了缓存键", name, other)
		}
		seen[key] = name
	}
}
