package routes

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 管理后台的“测试连接”端点会用已保存的密钥向调用者指定的地址发起外呼，
// 与对应的 PUT 一样必须经过 step-up 2FA（SEC-006）。
func TestConnectionTestEndpointsRequireStepUp(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		Backup:            &adminhandler.BackupHandler{},
		ContentModeration: &adminhandler.ContentModerationHandler{},
	}}
	stepUp := middleware.StepUpAuthMiddleware(func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": "STEP_UP_REQUIRED"})
	})
	admin := router.Group("/admin")
	registerBackupRoutes(admin, handlers, stepUp)
	registerContentModerationRoutes(admin, handlers, stepUp)

	for _, path := range []string{
		"/admin/backups/s3-config/test",
		"/admin/backups/image-storage/test",
		"/admin/risk-control/api-keys/test",
	} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, req)
			require.Equal(t, http.StatusForbidden, recorder.Code)
			require.Contains(t, recorder.Body.String(), "STEP_UP_REQUIRED")
		})
	}
}
