//go:build embed

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type embeddedRouterSettingRepo struct{}

func (*embeddedRouterSettingRepo) Get(context.Context, string) (*service.Setting, error) {
	return nil, service.ErrSettingNotFound
}

func (*embeddedRouterSettingRepo) GetValue(context.Context, string) (string, error) {
	return "", service.ErrSettingNotFound
}

func (*embeddedRouterSettingRepo) Set(context.Context, string, string) error { return nil }

func (*embeddedRouterSettingRepo) GetMultiple(context.Context, []string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (*embeddedRouterSettingRepo) SetMultiple(context.Context, map[string]string) error { return nil }

func (*embeddedRouterSettingRepo) GetAll(context.Context) (map[string]string, error) {
	return map[string]string{}, nil
}

func (*embeddedRouterSettingRepo) Delete(context.Context, string) error { return nil }

func TestSetupRouterEmbeddedFrontendDoesNotCaptureMetrics(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg := &config.Config{Radar: config.RadarConfig{
		MetricsBearerToken:           "test-secret",
		ArtificialAnalysisModelSlugs: []string{"model-a"},
	}}
	cfg.Pricing.DataDir = t.TempDir()
	cfg.Gateway.MaxBodySize = 1024 * 1024
	settingService := service.NewSettingService(&embeddedRouterSettingRepo{}, cfg)
	radarHandler, err := handler.NewRadarHandler(cfg, &routerRadarServiceStub{})
	require.NoError(t, err)
	pass := func(c *gin.Context) { c.Next() }
	router := SetupRouter(
		gin.New(),
		&handler.Handlers{Radar: radarHandler, Admin: &handler.AdminHandlers{}},
		middleware2.JWTAuthMiddleware(pass),
		middleware2.AdminAuthMiddleware(pass),
		middleware2.APIKeyAuthMiddleware(pass),
		nil,
		nil,
		nil,
		settingService,
		cfg,
		nil,
	)

	for _, tt := range []struct {
		name   string
		header string
		want   int
	}{
		{name: "missing token", want: http.StatusNotFound},
		{name: "wrong token", header: "Bearer wrong", want: http.StatusNotFound},
		{name: "correct token", header: "Bearer test-secret", want: http.StatusOK},
	} {
		t.Run(tt.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if tt.header != "" {
				request.Header.Set("Authorization", tt.header)
			}
			router.ServeHTTP(response, request)

			require.Equal(t, tt.want, response.Code)
			require.NotContains(t, response.Header().Get("Content-Type"), "text/html")
			if tt.want == http.StatusOK {
				require.True(t, strings.HasPrefix(response.Header().Get("Content-Type"), "text/plain"))
				require.Contains(t, response.Body.String(), "radar_fetch_success_total")
			}
		})
	}

	for _, tt := range []struct {
		path        string
		contentType string
	}{
		{path: "/health", contentType: "application/json"},
		{path: "/setup/status", contentType: "application/json"},
		{path: "/api/v1/public/radar/service-health", contentType: "application/json"},
		{path: "/", contentType: "text/html"},
		{path: "/home", contentType: "text/html"},
	} {
		t.Run(tt.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, tt.path, nil))
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.Contains(t, response.Header().Get("Content-Type"), tt.contentType)
		})
	}
}
