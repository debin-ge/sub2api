package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAccountProviderIdentityMutationsRequireStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{Account: &adminhandler.AccountHandler{}}}
	stepUp := middleware.StepUpAuthMiddleware(func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "STEP_UP_REQUIRED"})
	})
	registerAccountRoutes(router.Group("/admin"), handlers, stepUp)
	for _, path := range []string{
		"/admin/accounts/42/provider-identity/reviews",
		"/admin/accounts/42/provider-identity/reviews/1/approve",
		"/admin/accounts/42/provider-identity/reviews/1/reject",
		"/admin/accounts/42/provider-identity/revoke",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, path, nil))
			require.Equal(t, http.StatusForbidden, recorder.Code)
			require.Contains(t, recorder.Body.String(), "STEP_UP_REQUIRED")
		})
	}
}
