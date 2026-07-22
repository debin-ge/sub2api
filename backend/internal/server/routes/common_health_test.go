package routes

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// /health 的 version/slot 字段是蓝绿发布脚本的版本校验依据（FR-8.1），
// 字段名或语义变更需同步 deploy/blue-green/bin/s2a-deploy。
func TestHealthEndpointReportsVersionAndSlot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("APP_SLOT", "green")

	router := gin.New()
	RegisterCommonRoutes(router, &config.Config{}, "v1.4.2")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var body struct {
		Status  string `json:"status"`
		Version string `json:"version"`
		Slot    string `json:"slot"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "ok", body.Status)
	require.Equal(t, "v1.4.2", body.Version)
	require.Equal(t, "green", body.Slot)
}

func TestHealthEndpointSlotDefaultsToEmpty(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("APP_SLOT", "")

	router := gin.New()
	RegisterCommonRoutes(router, &config.Config{}, "")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Equal(t, http.StatusOK, recorder.Code)
	var body map[string]string
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, "ok", body["status"])
	require.Empty(t, body["slot"])
}
