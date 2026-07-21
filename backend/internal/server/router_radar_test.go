package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type routerRadarServiceStub struct{}

func (*routerRadarServiceStub) GetServiceHealth(context.Context) ([]service.ServiceHealthDTO, error) {
	return []service.ServiceHealthDTO{}, nil
}

func (*routerRadarServiceStub) GetQuotaBucketsLatest(context.Context) (*service.QuotaRadarLatestDTO, error) {
	return &service.QuotaRadarLatestDTO{Buckets: []service.BucketSnapshotDTO{}, Stale: true}, nil
}

func (*routerRadarServiceStub) GetQuotaBucketsTrend(context.Context, string, int) (*service.QuotaTrendDTO, error) {
	return &service.QuotaTrendDTO{DataPoints: []service.QuotaTrendPointDTO{}, Stale: true}, nil
}

func (*routerRadarServiceStub) GetDegradationLatest(context.Context) (*service.DegradationLatestDTO, error) {
	return &service.DegradationLatestDTO{}, nil
}

func (*routerRadarServiceStub) GetDegradationTrend(context.Context, string, service.DegradationMetric, int) (*service.DegradationTrendDTO, error) {
	return &service.DegradationTrendDTO{}, nil
}

func (*routerRadarServiceStub) GetLMArena(context.Context) (*service.LMArenaDTO, error) {
	return &service.LMArenaDTO{}, nil
}

func (*routerRadarServiceStub) GetDataSources(context.Context) ([]service.DataSourceMetaDTO, error) {
	return []service.DataSourceMetaDTO{}, nil
}

func TestRegisterRoutesWiresSevenUnauthenticatedRadarGETs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	cfg := &config.Config{Radar: config.RadarConfig{
		ArtificialAnalysisModelSlugs: []string{"model-a"},
	}}
	cfg.Pricing.DataDir = t.TempDir()
	cfg.Gateway.MaxBodySize = 1024 * 1024
	radarHandler, err := handler.NewRadarHandler(
		cfg,
		&routerRadarServiceStub{},
	)
	require.NoError(t, err)

	var jwtCalls atomic.Int32
	var adminCalls atomic.Int32
	var apiKeyCalls atomic.Int32
	reject := func(calls *atomic.Int32) gin.HandlerFunc {
		return func(c *gin.Context) {
			calls.Add(1)
			c.AbortWithStatus(http.StatusUnauthorized)
		}
	}
	registerRoutes(
		router,
		&handler.Handlers{Radar: radarHandler, Admin: &handler.AdminHandlers{}},
		middleware2.JWTAuthMiddleware(reject(&jwtCalls)),
		middleware2.AdminAuthMiddleware(reject(&adminCalls)),
		middleware2.APIKeyAuthMiddleware(reject(&apiKeyCalls)),
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
	)

	var got []string
	for _, route := range router.Routes() {
		if strings.HasPrefix(route.Path, "/api/v1/public/radar/") {
			got = append(got, route.Method+" "+route.Path)
		}
	}
	sort.Strings(got)
	require.Equal(t, []string{
		"GET /api/v1/public/radar/degradation/latest",
		"GET /api/v1/public/radar/degradation/trend",
		"GET /api/v1/public/radar/lmarena",
		"GET /api/v1/public/radar/quota-buckets/latest",
		"GET /api/v1/public/radar/quota-buckets/trend",
		"GET /api/v1/public/radar/service-health",
		"GET /api/v1/public/radar/sources",
	}, got)

	protectedPaths := []string{
		"/api/v1/user/profile",
		"/api/v1/admin/dashboard/stats",
		"/v1/models",
	}
	for _, path := range protectedPaths {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusUnauthorized, response.Code, "path=%s body=%s", path, response.Body.String())
	}
	require.Equal(t, int32(1), jwtCalls.Load())
	require.Equal(t, int32(1), adminCalls.Load())
	require.Equal(t, int32(1), apiKeyCalls.Load())
	authCallsBeforeRadar := [3]int32{jwtCalls.Load(), adminCalls.Load(), apiKeyCalls.Load()}

	paths := []string{
		"/api/v1/public/radar/service-health",
		"/api/v1/public/radar/quota-buckets/latest",
		"/api/v1/public/radar/quota-buckets/trend?bucket=anthropic%2Fpro",
		"/api/v1/public/radar/degradation/latest",
		"/api/v1/public/radar/degradation/trend?model=model-a&metric=coding_index",
		"/api/v1/public/radar/lmarena",
		"/api/v1/public/radar/sources",
	}
	for _, path := range paths {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, response.Code, "path=%s body=%s", path, response.Body.String())
	}
	require.Equal(t, authCallsBeforeRadar, [3]int32{jwtCalls.Load(), adminCalls.Load(), apiKeyCalls.Load()})
}
