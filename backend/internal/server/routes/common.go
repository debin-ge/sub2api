package routes

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"os"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/observability"
	"github.com/gin-gonic/gin"
)

// RegisterCommonRoutes 注册通用路由（健康检查、状态等）。
// version 为构建版本号，用于蓝绿发布时校验实际运行的镜像。
func RegisterCommonRoutes(r *gin.Engine, cfg *config.Config, version string) {
	// Initialize Radar collectors before exposing the process gatherer.
	observability.DefaultRadarMetrics()
	token := ""
	if cfg != nil {
		token = strings.TrimSpace(cfg.Radar.MetricsBearerToken)
	}
	r.GET("/metrics", gin.WrapH(metricsBearerOnly(token, observability.MetricsHandler(nil))))

	// 健康检查。version/slot 供蓝绿发布脚本校验「起来的确实是预期镜像」：
	// slot 由部署编排通过 APP_SLOT 注入（blue/green），非蓝绿部署时为空串。
	slot := os.Getenv("APP_SLOT")
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":  "ok",
			"version": version,
			"slot":    slot,
		})
	})

	// Claude Code 遥测日志（忽略，直接返回200）
	r.POST("/api/event_logging/batch", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	// Setup status endpoint (always returns needs_setup: false in normal mode)
	// This is used by the frontend to detect when the service has restarted after setup
	r.GET("/setup/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{
				"needs_setup": false,
				"step":        "completed",
			},
		})
	})
}

func metricsBearerOnly(expected string, next http.Handler) http.Handler {
	expectedDigest := sha256.Sum256([]byte(expected))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if expected == "" {
			http.NotFound(w, r)
			return
		}
		authorization := r.Header.Get("Authorization")
		scheme, provided, ok := strings.Cut(authorization, " ")
		providedDigest := sha256.Sum256([]byte(provided))
		if !ok || scheme != "Bearer" || provided == "" || subtle.ConstantTimeCompare(expectedDigest[:], providedDigest[:]) != 1 {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}
