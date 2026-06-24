package routes

import (
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
)

func TestBenchmarkRoutesRegisterPublicRadarRoute(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")

	RegisterBenchmarkRoutes(v1, &handler.Handlers{
		Radar: &handler.RadarHandler{},
	})

	assertRouteRegistered(t, router, http.MethodGet, "/api/v1/public/radar")
}

func TestBenchmarkRoutesRegisterAdminBenchmarkRoutes(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")

	RegisterAdminRoutes(v1, &handler.Handlers{
		Admin: &handler.AdminHandlers{},
	}, middleware.AdminAuthMiddleware(func(c *gin.Context) {
		c.Next()
	}))

	assertRouteRegistered(t, router, http.MethodGet, "/api/v1/admin/benchmark/suites")
	assertRouteRegistered(t, router, http.MethodPost, "/api/v1/admin/benchmark/suites")
	assertRouteRegistered(t, router, http.MethodPost, "/api/v1/admin/benchmark/runs/:id/publish")
}

func TestBenchmarkRoutesRegisterAdminBenchmarkScheduleRoutes(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	v1 := router.Group("/api/v1")

	RegisterAdminRoutes(v1, &handler.Handlers{
		Admin: &handler.AdminHandlers{},
	}, middleware.AdminAuthMiddleware(func(c *gin.Context) {
		c.Next()
	}))

	assertRouteRegistered(t, router, http.MethodGet, "/api/v1/admin/benchmark/schedules")
	assertRouteRegistered(t, router, http.MethodPost, "/api/v1/admin/benchmark/schedules")
	assertRouteRegistered(t, router, http.MethodPost, "/api/v1/admin/benchmark/schedules/:id/trigger")
}

func assertRouteRegistered(t *testing.T, router *gin.Engine, method string, path string) {
	t.Helper()

	for _, route := range router.Routes() {
		if route.Method == method && route.Path == path {
			return
		}
	}

	t.Fatalf("route %s %s not registered", method, path)
}
