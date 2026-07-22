package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestCommonRoutesMetricsAreClosedByDefault(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterCommonRoutes(router, &config.Config{}, "")
	observability.DefaultRadarMetrics().RecordFetchFailure("status_claude", "network_error", 0, time.Millisecond)

	for _, remoteAddr := range []string{"127.0.0.1:43123", "172.18.0.25:43123", "100.64.0.1:43123", "[fd00::1]:43123", "203.0.113.10:43123"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		request.RemoteAddr = remoteAddr
		request.Header.Set("Authorization", "Bearer ignored-when-closed")
		router.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusNotFound, recorder.Code, remoteAddr)
	}
}

func TestCommonRoutesMetricsRequireBearerTokenRegardlessOfPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	RegisterCommonRoutes(router, &config.Config{Radar: config.RadarConfig{MetricsBearerToken: "test-secret"}}, "")

	for _, tt := range []struct {
		name       string
		remoteAddr string
		header     string
		want       int
	}{
		{name: "loopback missing", remoteAddr: "127.0.0.1:43123", want: http.StatusNotFound},
		{name: "private wrong", remoteAddr: "172.18.0.25:43123", header: "Bearer wrong", want: http.StatusNotFound},
		{name: "shared address missing", remoteAddr: "100.64.0.1:43123", want: http.StatusNotFound},
		{name: "ipv6 wrong scheme", remoteAddr: "[fd00::1]:43123", header: "Basic test-secret", want: http.StatusNotFound},
		{name: "public right", remoteAddr: "203.0.113.10:43123", header: "Bearer test-secret", want: http.StatusOK},
		{name: "private right", remoteAddr: "172.18.0.25:43123", header: "Bearer test-secret", want: http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			request.RemoteAddr = tt.remoteAddr
			if tt.header != "" {
				request.Header.Set("Authorization", tt.header)
			}
			router.ServeHTTP(recorder, request)
			require.Equal(t, tt.want, recorder.Code)
			require.NotContains(t, recorder.Body.String(), "test-secret")
			if tt.want == http.StatusOK {
				require.Contains(t, recorder.Header().Get("Content-Type"), "text/plain")
				require.Contains(t, recorder.Body.String(), "radar_source_age_seconds")
				require.Contains(t, recorder.Body.String(), "process_cpu_seconds_total")
			}
		})
	}

	health := httptest.NewRecorder()
	healthRequest := httptest.NewRequest(http.MethodGet, "/health", nil)
	healthRequest.RemoteAddr = "203.0.113.10:43123"
	router.ServeHTTP(health, healthRequest)
	require.Equal(t, http.StatusOK, health.Code)
}
