package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

func TestRadarMetricsMiddlewareRecordsOnlyFixedRouteAndStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	registry := prometheus.NewRegistry()
	metrics, err := observability.NewRadarMetrics(registry)
	require.NoError(t, err)
	router := gin.New()
	router.Use(radarMetricsMiddleware(metrics))
	router.GET("/api/v1/public/radar/service-health", func(c *gin.Context) {
		c.Status(http.StatusServiceUnavailable)
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/public/radar/service-health?token=secret", nil))
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)

	scrape := httptest.NewRecorder()
	observability.MetricsHandler(registry).ServeHTTP(scrape, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := scrape.Body.String()
	require.Contains(t, body, `radar_http_requests_total{route="/api/v1/public/radar/service-health",status="5xx"} 1`)
	require.Contains(t, body, `radar_http_request_duration_seconds_bucket{route="/api/v1/public/radar/service-health"`)
	require.NotContains(t, body, "token")
	require.NotContains(t, body, "secret")
}
