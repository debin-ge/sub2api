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
		"GET /api/v1/admin/videos/tasks/:id",
		"GET /api/v1/admin/videos/tasks/:id/events",
		"POST /api/v1/admin/videos/tasks/:id/retry-get",
		"POST /api/v1/admin/videos/tasks/:id/retry-settlement",
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
	// The surviving admin surface is pure retry. Nothing here carries an
	// operator's decision, because manual review no longer exists.
	for route := range routes {
		require.NotContains(t, route, "/videos/grok/")
		require.NotContains(t, route, "/videos/intents")
		require.NotContains(t, route, "replay-create")
		require.NotContains(t, route, "retry-create")
		require.NotContains(t, route, "review")
		require.NotContains(t, route, "resolve-")
		require.NotContains(t, route, "/tasks/unknown")
	}

	task := "video_0123456789abcdef0123456789abcdef"
	for _, removedPath := range []string{
		"/api/v1/admin/videos/grok/jobs/1/correction-reviews/1/apply",
		"/api/v1/admin/videos/grok/legacy-imports/scan",
		"/api/v1/admin/videos/intents/1/reviews/confirm-created",
		// Every manual-review entry point is gone, not merely unlinked.
		"/api/v1/admin/videos/tasks/" + task + "/resolve-not-created",
		"/api/v1/admin/videos/tasks/" + task + "/resolve-created",
		"/api/v1/admin/videos/tasks/" + task + "/resolve-billing-capture",
		"/api/v1/admin/videos/tasks/" + task + "/resolve-billing-release",
		"/api/v1/admin/videos/tasks/" + task + "/retry-character-resource",
		"/api/v1/admin/videos/tasks/" + task + "/billing-reviews/1/approve",
		"/api/v1/admin/videos/tasks/" + task + "/billing-reviews/1/reject",
		"/api/v1/admin/videos/tasks/" + task + "/submission-reviews/1/approve",
		"/api/v1/admin/videos/tasks/" + task + "/submission-reviews/1/reject",
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, removedPath, nil))
		require.Equal(t, http.StatusNotFound, response.Code, removedPath)
	}
	for _, removedPath := range []string{
		"/api/v1/admin/videos/tasks/" + task + "/billing-reviews",
		"/api/v1/admin/videos/tasks/" + task + "/submission-reviews",
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, removedPath, nil))
		require.Equal(t, http.StatusNotFound, response.Code, removedPath)
	}

	// The unknown-task queue is gone. Its old path now falls through to the
	// task detail route, which rejects "unknown" as a malformed public ID
	// rather than serving a listing.
	unknownQueue := httptest.NewRecorder()
	router.ServeHTTP(unknownQueue, httptest.NewRequest(http.MethodGet, "/api/v1/admin/videos/tasks/unknown", nil))
	require.Equal(t, http.StatusBadRequest, unknownQueue.Code)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/admin/videos/tasks/"+task+"/retry-create", nil)
	router.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusNotFound, recorder.Code)
}
