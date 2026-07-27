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

// /health 的 version/slot 字段是外部发布工具的版本校验依据，
// 字段名或语义变更需同步发布工具的健康门禁。
func TestHealthEndpointReportsVersionAndSlot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("APP_SLOT", " GREEN ")

	router := gin.New()
	RegisterCommonRoutes(router, &config.Config{}, " v1.4.2\n")

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
	require.Equal(t, "no-store, max-age=0", recorder.Header().Get("Cache-Control"))
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
	require.Equal(t, unknownBuildVersion, body["version"])
	require.Empty(t, body["slot"])
}

func TestHealthEndpointRejectsInvalidDeploymentSlot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv("APP_SLOT", "canary")

	router := gin.New()
	RegisterCommonRoutes(router, &config.Config{}, "1.6.9")

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/health", nil))

	require.Equal(t, http.StatusServiceUnavailable, recorder.Code)
	var body healthPayload
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &body))
	require.Equal(t, healthPayload{Status: "error", Version: "1.6.9", Slot: "canary"}, body)
}
