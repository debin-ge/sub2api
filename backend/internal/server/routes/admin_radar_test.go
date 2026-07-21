package routes

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAdminRadarRoutesInheritAdminAuthAndComplianceGates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusLocked} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			router := gin.New()
			adminGroup := router.Group("/api/v1/admin")
			adminGroup.Use(func(c *gin.Context) {
				c.AbortWithStatus(status)
			})
			registerAdminRadarRoutes(adminGroup, &handler.Handlers{Admin: &handler.AdminHandlers{Radar: &admin.RadarHandler{}}})

			for _, request := range []struct {
				method string
				path   string
			}{
				{http.MethodGet, "/api/v1/admin/radar/status"},
				{http.MethodPut, "/api/v1/admin/radar/settings"},
				{http.MethodPost, "/api/v1/admin/radar/refresh"},
			} {
				response := httptest.NewRecorder()
				router.ServeHTTP(response, httptest.NewRequest(request.method, request.path, nil))
				require.Equal(t, status, response.Code, "%s %s", request.method, request.path)
			}
		})
	}
}

func TestAdminRadarRouteContract(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerAdminRadarRoutes(router.Group("/api/v1/admin"), &handler.Handlers{Admin: &handler.AdminHandlers{Radar: &admin.RadarHandler{}}})
	got := make([]string, 0, 3)
	for _, route := range router.Routes() {
		got = append(got, route.Method+" "+route.Path)
	}
	sort.Strings(got)
	require.Equal(t, []string{
		"GET /api/v1/admin/radar/status",
		"POST /api/v1/admin/radar/refresh",
		"PUT /api/v1/admin/radar/settings",
	}, got)
}
