package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterResellerRoutesUpstreamBalance(t *testing.T) {
	gin.SetMode(gin.TestMode)

	router := gin.New()
	adminGroup := router.Group("/api/v1/admin")
	registerResellerRoutes(
		adminGroup,
		&handler.Handlers{
			Admin: &handler.AdminHandlers{
				Reseller: admin.NewResellerHandler(&config.Config{}, nil),
			},
		},
	)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/reseller/upstream-balance", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"status":"disabled"`)
}
