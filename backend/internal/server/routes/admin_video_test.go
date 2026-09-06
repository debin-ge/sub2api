package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterAdminVideoRoutesExposeOnlySafeRecoveryCommands(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerAdminVideoRoutes(router.Group("/api/v1/admin"), &handler.Handlers{Admin: &handler.AdminHandlers{Video: adminhandler.NewVideoHandler(nil)}})

	routes := make(map[string]struct{})
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}
	for _, expected := range []string{
		"GET /api/v1/admin/videos/overview",
		"GET /api/v1/admin/videos/capabilities",
		"PUT /api/v1/admin/videos/capabilities",
		"GET /api/v1/admin/videos/tasks",
		"GET /api/v1/admin/videos/tasks/unknown",
		"GET /api/v1/admin/videos/tasks/:id",
		"GET /api/v1/admin/videos/tasks/:id/events",
		"POST /api/v1/admin/videos/tasks/:id/resolve-not-created",
		"POST /api/v1/admin/videos/tasks/:id/resolve-created",
		"POST /api/v1/admin/videos/tasks/:id/retry-get",
		"POST /api/v1/admin/videos/tasks/:id/retry-settlement",
		"POST /api/v1/admin/videos/tasks/:id/resolve-billing-capture",
		"POST /api/v1/admin/videos/tasks/:id/resolve-billing-release",
		"POST /api/v1/admin/videos/tasks/:id/retry-delete",
		"GET /api/v1/admin/videos/resources",
		"GET /api/v1/admin/videos/resources/:id",
		"GET /api/v1/admin/videos/webhooks/unmatched",
		"GET /api/v1/admin/videos/callbacks",
		"POST /api/v1/admin/videos/callbacks/:id/retry",
	} {
		_, ok := routes[expected]
		require.True(t, ok, expected)
	}
	for route := range routes {
		require.NotContains(t, route, "/videos/grok/")
		require.NotContains(t, route, "/videos/intents")
		require.NotContains(t, route, "replay-create")
		require.NotContains(t, route, "retry-create")
	}

	for _, removedPath := range []string{"/api/v1/admin/videos/grok/jobs/1/correction-reviews/1/apply", "/api/v1/admin/videos/grok/legacy-imports/scan", "/api/v1/admin/videos/intents/1/reviews/confirm-created"} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, removedPath, nil))
		require.Equal(t, http.StatusNotFound, response.Code)
	}

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/videos/tasks/video_0123456789abcdef0123456789abcdef/retry-create", nil)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}
