package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newGatewayRoutesTestRouter() *gin.Engine {
	return newGatewayRoutesTestRouterForPlatform(service.PlatformOpenAI)
}

func newGatewayRoutesTestRouterForPlatform(platform string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	RegisterGatewayRoutes(
		router,
		&handler.Handlers{
			Gateway:        &handler.GatewayHandler{},
			OpenAIGateway:  &handler.OpenAIGatewayHandler{},
			MiniMaxGateway: &handler.MiniMaxGatewayHandler{},
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			groupID := int64(1)
			c.Set(string(servermiddleware.ContextKeyAPIKey), &service.APIKey{
				GroupID: &groupID,
				Group:   &service.Group{Platform: platform},
			})
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: 101, Concurrency: 1})
			c.Next()
		}),
		nil,
		nil,
		nil,
		nil,
		&config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1024 * 1024}},
	)

	return router
}

func TestGatewayRoutesOpenAIResponsesCompactPathIsRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/responses/compact",
		"/responses/compact",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-5"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI responses handler", path)
	}
}

func TestGatewayRoutesOpenAIImagesPathsAreRegistered(t *testing.T) {
	router := newGatewayRoutesTestRouter()

	for _, path := range []string{
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"gpt-image-2","prompt":"draw a cat"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.NotEqual(t, http.StatusNotFound, w.Code, "path=%s should hit OpenAI images handler", path)
	}
}

func TestGatewayRoutesMiniMaxMessagesDispatchesToMiniMaxHandler(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformMiniMax)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), "minimax gateway service unavailable")
}

func TestGatewayRoutesMiniMaxUnsupportedEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformMiniMax)

	for _, path := range []string{
		"/v1/responses",
		"/v1/responses/compact",
		"/v1/messages/count_tokens",
		"/responses",
		"/responses/compact",
		"/backend-api/codex/responses",
		"/backend-api/codex/responses/compact",
		"/v1/chat/completions",
		"/chat/completions",
		"/v1/images/generations",
		"/v1/images/edits",
		"/images/generations",
		"/images/edits",
	} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"model":"claude-sonnet-4-5"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be MiniMax unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "MiniMax gateway supports /v1/messages only", "path=%s", path)
	}
}

func TestGatewayRoutesMiniMaxUnsupportedGetEndpointsReturnNotFound(t *testing.T) {
	router := newGatewayRoutesTestRouterForPlatform(service.PlatformMiniMax)

	for _, path := range []string{
		"/v1/models",
		"/v1/usage",
		"/v1/responses",
		"/responses",
		"/backend-api/codex/responses",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)
		require.Equal(t, http.StatusNotFound, w.Code, "path=%s should be MiniMax unsupported", path)
		require.Contains(t, w.Body.String(), "not_found_error", "path=%s", path)
		require.Contains(t, w.Body.String(), "MiniMax gateway supports /v1/messages only", "path=%s", path)
	}
}
