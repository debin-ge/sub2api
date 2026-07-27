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

const unknownBuildVersion = "unknown"

type healthPayload struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Slot    string `json:"slot"`
}

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

	// version/slot 是蓝绿部署身份门禁的稳定契约。APP_SLOT 未配置表示非蓝绿部署；
	// 一旦配置，只接受 blue/green，避免拼写错误的实例被 Docker 判定为健康。
	r.GET("/health", healthHandler(version, os.Getenv("APP_SLOT")))

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

func healthHandler(version, slot string) gin.HandlerFunc {
	version = strings.TrimSpace(version)
	if version == "" {
		version = unknownBuildVersion
	}
	slot = strings.ToLower(strings.TrimSpace(slot))
	validSlot := slot == "" || slot == "blue" || slot == "green"

	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, max-age=0")
		status := "ok"
		statusCode := http.StatusOK
		if !validSlot {
			status = "error"
			statusCode = http.StatusServiceUnavailable
		}
		c.JSON(statusCode, healthPayload{
			Status:  status,
			Version: version,
			Slot:    slot,
		})
	}
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
