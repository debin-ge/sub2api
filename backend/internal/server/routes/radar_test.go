package routes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type radarPublicProbeKey struct{}
type radarAuthProbeKey struct{}

type radarRoutesService struct {
	calls atomic.Int32
}

func (s *radarRoutesService) verifyPublicContext(ctx context.Context) error {
	if ctx.Value(radarPublicProbeKey{}) != "unauthenticated" {
		return errors.New("public sentinel middleware was not preserved")
	}
	if ctx.Value(radarAuthProbeKey{}) != nil {
		return errors.New("radar route unexpectedly inherited auth middleware")
	}
	s.calls.Add(1)
	return nil
}

func (s *radarRoutesService) GetServiceHealth(ctx context.Context) ([]service.ServiceHealthDTO, error) {
	return make([]service.ServiceHealthDTO, 0), s.verifyPublicContext(ctx)
}

func (s *radarRoutesService) GetQuotaBucketsLatest(ctx context.Context) (*service.QuotaRadarLatestDTO, error) {
	return &service.QuotaRadarLatestDTO{Buckets: make([]service.BucketSnapshotDTO, 0), Stale: true}, s.verifyPublicContext(ctx)
}

func (s *radarRoutesService) GetQuotaBucketsTrend(ctx context.Context, bucket string, days int) (*service.QuotaTrendDTO, error) {
	if bucket != "anthropic/pro" || days != 7 {
		return nil, errors.New("unexpected validated quota trend arguments")
	}
	return &service.QuotaTrendDTO{BucketKey: bucket, Days: days, DataPoints: make([]service.QuotaTrendPointDTO, 0), Stale: true}, s.verifyPublicContext(ctx)
}

func (s *radarRoutesService) GetDegradationLatest(ctx context.Context) (*service.DegradationLatestDTO, error) {
	return &service.DegradationLatestDTO{Models: make([]service.DegradationModelDTO, 0), LMArenaTop5: make([]service.LMArenaEntryDTO, 0), SourcesLastUpdated: map[string]*time.Time{}}, s.verifyPublicContext(ctx)
}

func (s *radarRoutesService) GetDegradationTrend(ctx context.Context, model string, metric service.DegradationMetric, days int) (*service.DegradationTrendDTO, error) {
	if model != "model-a" || metric != service.DegradationMetricCodingIndex || days != 90 {
		return nil, errors.New("unexpected validated trend arguments")
	}
	return &service.DegradationTrendDTO{ModelSlug: model, Metric: metric, Days: days, DataPoints: make([]service.MetricPointDTO, 0)}, s.verifyPublicContext(ctx)
}

func (s *radarRoutesService) GetLMArena(ctx context.Context) (*service.LMArenaDTO, error) {
	return &service.LMArenaDTO{Leaderboard: make([]service.LMArenaEntryDTO, 0)}, s.verifyPublicContext(ctx)
}

func (s *radarRoutesService) GetDataSources(ctx context.Context) ([]service.DataSourceMetaDTO, error) {
	return make([]service.DataSourceMetaDTO, 0), s.verifyPublicContext(ctx)
}

func newRadarRoutesTestRouter(t *testing.T) (*gin.Engine, *radarRoutesService, *atomic.Int32) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.Use(func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), radarPublicProbeKey{}, "unauthenticated")
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})

	var authCalls atomic.Int32
	authenticated := v1.Group("/authenticated")
	authenticated.Use(func(c *gin.Context) {
		authCalls.Add(1)
		ctx := context.WithValue(c.Request.Context(), radarAuthProbeKey{}, true)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	authenticated.GET("/probe", func(c *gin.Context) { c.Status(http.StatusNoContent) })

	radarService := &radarRoutesService{}
	radarHandler, err := handler.NewRadarHandler(&config.Config{Radar: config.RadarConfig{
		ArtificialAnalysisModelSlugs: []string{"model-a"},
	}}, radarService)
	require.NoError(t, err)
	RegisterRadarRoutes(v1, radarHandler)
	return router, radarService, &authCalls
}

func TestRegisterRadarRoutesRegistersSevenPublicGETEndpoints(t *testing.T) {
	router, _, _ := newRadarRoutesTestRouter(t)

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
}

func TestRadarRoutesAreReachableWithoutAuthAndDoNotInheritAuthMiddleware(t *testing.T) {
	router, radarService, authCalls := newRadarRoutesTestRouter(t)

	probe := httptest.NewRecorder()
	router.ServeHTTP(probe, httptest.NewRequest(http.MethodGet, "/api/v1/authenticated/probe", nil))
	require.Equal(t, http.StatusNoContent, probe.Code)
	require.Equal(t, int32(1), authCalls.Load(), "auth sentinel must be active on its own group")

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
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, w.Code, "path=%s body=%s", path, w.Body.String())
	}

	require.Equal(t, int32(7), radarService.calls.Load())
	require.Equal(t, int32(1), authCalls.Load(), "public Radar requests must not execute auth middleware")
}

func TestRadarRoutesRejectPostAndUnknownPaths(t *testing.T) {
	router, radarService, _ := newRadarRoutesTestRouter(t)

	paths := []string{
		"/api/v1/public/radar/service-health",
		"/api/v1/public/radar/quota-buckets/latest",
		"/api/v1/public/radar/quota-buckets/trend",
		"/api/v1/public/radar/degradation/latest",
		"/api/v1/public/radar/degradation/trend",
		"/api/v1/public/radar/lmarena",
		"/api/v1/public/radar/sources",
	}
	for _, path := range paths {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, nil))
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s", path)
	}

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/public/radar/unknown", nil))
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Zero(t, radarService.calls.Load())
}
