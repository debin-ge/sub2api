package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func setupAdminRouter() (*gin.Engine, *stubAdminService) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()

	userHandler := NewUserHandler(adminSvc, nil, nil, nil)
	groupHandler := NewGroupHandler(adminSvc, nil, nil, nil)
	groupHandler.modelCatalog = &adminModelCatalogStub{
		groupModels: map[int64][]string{0: service.DefaultModelCatalogIDs(service.PlatformOpenAI)},
	}
	proxyHandler := NewProxyHandler(adminSvc)
	redeemHandler := NewRedeemHandler(adminSvc, nil)

	router.GET("/api/v1/admin/users", userHandler.List)
	router.GET("/api/v1/admin/users/:id", userHandler.GetByID)
	router.POST("/api/v1/admin/users/:id/auth-identities", userHandler.BindAuthIdentity)
	router.POST("/api/v1/admin/users", userHandler.Create)
	router.PUT("/api/v1/admin/users/:id", userHandler.Update)
	router.DELETE("/api/v1/admin/users/:id", userHandler.Delete)
	router.POST("/api/v1/admin/users/:id/balance", userHandler.UpdateBalance)
	router.GET("/api/v1/admin/users/:id/api-keys", userHandler.GetUserAPIKeys)
	router.GET("/api/v1/admin/users/:id/usage", userHandler.GetUserUsage)

	router.GET("/api/v1/admin/groups", groupHandler.List)
	router.GET("/api/v1/admin/groups/all", groupHandler.GetAll)
	router.GET("/api/v1/admin/groups/:id/models-list-candidates", groupHandler.GetModelsListCandidates)
	router.GET("/api/v1/admin/groups/:id", groupHandler.GetByID)
	router.POST("/api/v1/admin/groups", groupHandler.Create)
	router.PUT("/api/v1/admin/groups/:id", groupHandler.Update)
	router.DELETE("/api/v1/admin/groups/:id", groupHandler.Delete)
	router.GET("/api/v1/admin/groups/:id/stats", groupHandler.GetStats)
	router.GET("/api/v1/admin/groups/:id/api-keys", groupHandler.GetGroupAPIKeys)

	router.GET("/api/v1/admin/proxies", proxyHandler.List)
	router.GET("/api/v1/admin/proxies/all", proxyHandler.GetAll)
	router.GET("/api/v1/admin/proxies/:id", proxyHandler.GetByID)
	router.POST("/api/v1/admin/proxies", proxyHandler.Create)
	router.PUT("/api/v1/admin/proxies/:id", proxyHandler.Update)
	router.DELETE("/api/v1/admin/proxies/:id", proxyHandler.Delete)
	router.POST("/api/v1/admin/proxies/batch-delete", proxyHandler.BatchDelete)
	router.POST("/api/v1/admin/proxies/:id/test", proxyHandler.Test)
	router.POST("/api/v1/admin/proxies/:id/quality-check", proxyHandler.CheckQuality)
	router.GET("/api/v1/admin/proxies/:id/stats", proxyHandler.GetStats)
	router.GET("/api/v1/admin/proxies/:id/accounts", proxyHandler.GetProxyAccounts)

	router.GET("/api/v1/admin/redeem-codes", redeemHandler.List)
	router.GET("/api/v1/admin/redeem-codes/:id", redeemHandler.GetByID)
	router.POST("/api/v1/admin/redeem-codes", redeemHandler.Generate)
	router.DELETE("/api/v1/admin/redeem-codes/:id", redeemHandler.Delete)
	router.POST("/api/v1/admin/redeem-codes/batch-delete", redeemHandler.BatchDelete)
	router.POST("/api/v1/admin/redeem-codes/:id/expire", redeemHandler.Expire)
	router.GET("/api/v1/admin/redeem-codes/:id/stats", redeemHandler.GetStats)

	return router, adminSvc
}

func TestUserHandlerEndpoints(t *testing.T) {
	router, _ := setupAdminRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users?page=1&page_size=20", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/1", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	bindBody := map[string]any{
		"provider_type":    "wechat",
		"provider_key":     "wechat-main",
		"provider_subject": "union-123",
		"metadata":         map[string]any{"source": "admin-repair"},
		"channel": map[string]any{
			"channel":         "open",
			"channel_app_id":  "wx-open",
			"channel_subject": "openid-123",
		},
	}
	body, _ := json.Marshal(bindBody)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/1/auth-identities", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	createBody := map[string]any{"email": "new@example.com", "password": "pass123", "balance": 1, "concurrency": 2}
	body, _ = json.Marshal(createBody)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	updateBody := map[string]any{"email": "updated@example.com"}
	body, _ = json.Marshal(updateBody)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/users/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/users/1", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/1/balance", bytes.NewBufferString(`{"balance":1,"operation":"add"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/1/api-keys", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/users/1/usage?period=today", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestUserHandlerBindAuthIdentityMapsRequest(t *testing.T) {
	router, adminSvc := setupAdminRouter()

	body, err := json.Marshal(map[string]any{
		"provider_type":    "oidc",
		"provider_key":     "https://issuer.example",
		"provider_subject": "subject-123",
		"issuer":           "https://issuer.example",
		"metadata":         map[string]any{"report_id": 12},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/users/9/auth-identities", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, int64(9), adminSvc.boundAuthIdentityFor)
	require.NotNil(t, adminSvc.boundAuthIdentity)
	require.Equal(t, "oidc", adminSvc.boundAuthIdentity.ProviderType)
	require.Equal(t, "https://issuer.example", adminSvc.boundAuthIdentity.ProviderKey)
	require.Equal(t, "subject-123", adminSvc.boundAuthIdentity.ProviderSubject)
	require.Nil(t, adminSvc.boundAuthIdentity.Channel)
	require.Equal(t, float64(12), adminSvc.boundAuthIdentity.Metadata["report_id"])
}

func TestGroupHandlerEndpoints(t *testing.T) {
	router, _ := setupAdminRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/all", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/2", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/0/models-list-candidates?platform=openai", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "gpt-5.5")

	body, _ := json.Marshal(map[string]any{"name": "new", "platform": "anthropic", "subscription_type": "standard"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	body, _ = json.Marshal(map[string]any{"name": "update"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/groups/2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/groups/2", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/2/stats", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/groups/2/api-keys", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestGroupHandlerCreateAcceptsMiniMax(t *testing.T) {
	router, _ := setupAdminRouter()

	body, err := json.Marshal(map[string]any{
		"name":              "minimax-code-plan",
		"platform":          "minimax",
		"subscription_type": "subscription",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestGroupHandlerCreateAcceptsGLM(t *testing.T) {
	router, _ := setupAdminRouter()

	body, err := json.Marshal(map[string]any{
		"name":              "glm-coding-plan",
		"platform":          "glm",
		"subscription_type": "subscription",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestGroupHandlerCreateAcceptsKimi(t *testing.T) {
	router, _ := setupAdminRouter()

	body, err := json.Marshal(map[string]any{
		"name":              "kimi-coding-plan",
		"platform":          "kimi",
		"subscription_type": "subscription",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestGroupHandlerCreateAcceptsDeepSeek(t *testing.T) {
	router, _ := setupAdminRouter()

	body, err := json.Marshal(map[string]any{
		"name":              "deepseek-api",
		"platform":          "deepseek",
		"subscription_type": "subscription",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestGroupHandlerCreateAcceptsWindsurf(t *testing.T) {
	router, _ := setupAdminRouter()

	body, err := json.Marshal(map[string]any{
		"name":              "windsurf-api",
		"platform":          "windsurf",
		"subscription_type": "subscription",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/groups", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestGroupHandlerUpdateAcceptsGLM(t *testing.T) {
	router, _ := setupAdminRouter()

	body, err := json.Marshal(map[string]any{
		"platform": "glm",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/groups/2", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestAccountHandlerCreateAcceptsGLMAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts", accountHandler.Create)

	body, err := json.Marshal(map[string]any{
		"name":        "glm-coding-plan",
		"platform":    "glm",
		"type":        "apikey",
		"credentials": map[string]any{"api_key": "sk-glm-test"},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, adminSvc.createdAccounts, 1)
	require.Equal(t, "glm", adminSvc.createdAccounts[0].Platform)
	require.Equal(t, "apikey", adminSvc.createdAccounts[0].Type)
}

func TestAccountHandlerCreateAcceptsKimiAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts", accountHandler.Create)

	body, err := json.Marshal(map[string]any{
		"name":        "kimi-coding-plan",
		"platform":    "kimi",
		"type":        "apikey",
		"credentials": map[string]any{"api_key": "sk-kimi-test"},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, adminSvc.createdAccounts, 1)
	require.Equal(t, service.PlatformKimi, adminSvc.createdAccounts[0].Platform)
	require.Equal(t, service.AccountTypeAPIKey, adminSvc.createdAccounts[0].Type)
}

func TestAccountHandlerCreateAcceptsDeepSeekAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts", accountHandler.Create)

	body, err := json.Marshal(map[string]any{
		"name":        "deepseek-api",
		"platform":    "deepseek",
		"type":        "apikey",
		"credentials": map[string]any{"api_key": "sk-deepseek-test"},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, adminSvc.createdAccounts, 1)
	require.Equal(t, service.PlatformDeepSeek, adminSvc.createdAccounts[0].Platform)
	require.Equal(t, service.AccountTypeAPIKey, adminSvc.createdAccounts[0].Type)
}

func TestAccountHandlerCreateAcceptsWindsurfAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts", accountHandler.Create)

	body, err := json.Marshal(map[string]any{
		"name":        "windsurf-api",
		"platform":    "windsurf",
		"type":        "apikey",
		"credentials": map[string]any{"api_key": "sk-windsurf-test"},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, adminSvc.createdAccounts, 1)
	require.Equal(t, service.PlatformWindsurf, adminSvc.createdAccounts[0].Platform)
	require.Equal(t, service.AccountTypeAPIKey, adminSvc.createdAccounts[0].Type)
}

func TestAccountHandlerCreateRejectsDeepSeekWithoutAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts", accountHandler.Create)

	body, err := json.Marshal(map[string]any{
		"name":        "deepseek-api",
		"platform":    "deepseek",
		"type":        "apikey",
		"credentials": map[string]any{"api_key": " "},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Empty(t, adminSvc.createdAccounts)
	require.Contains(t, rec.Body.String(), "api_key")
}

func TestAccountHandlerCreateRejectsWindsurfWithoutAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts", accountHandler.Create)

	body, err := json.Marshal(map[string]any{
		"name":        "windsurf-api",
		"platform":    "windsurf",
		"type":        "apikey",
		"credentials": map[string]any{"api_key": " "},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Empty(t, adminSvc.createdAccounts)
	require.Contains(t, rec.Body.String(), "api_key")
}

func TestAccountHandlerCreateRejectsKimiWithoutAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts", accountHandler.Create)

	body, err := json.Marshal(map[string]any{
		"name":        "kimi-coding-plan",
		"platform":    "kimi",
		"type":        "apikey",
		"credentials": map[string]any{"api_key": " "},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Empty(t, adminSvc.createdAccounts)
	require.Contains(t, rec.Body.String(), "api_key")
}

func TestAccountHandlerCreateRejectsGLMWithoutAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts", accountHandler.Create)

	body, err := json.Marshal(map[string]any{
		"name":        "glm-coding-plan",
		"platform":    "glm",
		"type":        "apikey",
		"credentials": map[string]any{"api_key": " "},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Empty(t, adminSvc.createdAccounts)
	require.Contains(t, rec.Body.String(), "api_key")
}

func TestAccountHandlerBatchCreateValidatesGLMAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts/batch", accountHandler.BatchCreate)

	body, err := json.Marshal(map[string]any{
		"accounts": []map[string]any{
			{
				"name":        "glm-valid",
				"platform":    "glm",
				"type":        "apikey",
				"credentials": map[string]any{"api_key": "sk-glm-test"},
			},
			{
				"name":        "glm-missing-key",
				"platform":    "glm",
				"type":        "apikey",
				"credentials": map[string]any{"api_key": " "},
			},
			{
				"name":        "glm-invalid-type",
				"platform":    "glm",
				"type":        "oauth",
				"credentials": map[string]any{"api_key": "sk-glm-test"},
			},
		},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/batch", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Len(t, adminSvc.createdAccounts, 1)
	require.Equal(t, service.PlatformGLM, adminSvc.createdAccounts[0].Platform)
	require.Equal(t, service.AccountTypeAPIKey, adminSvc.createdAccounts[0].Type)

	var resp struct {
		Code int `json:"code"`
		Data struct {
			Success int `json:"success"`
			Failed  int `json:"failed"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 1, resp.Data.Success)
	require.Equal(t, 2, resp.Data.Failed)
}

func TestAccountHandlerUpdateRejectsGLMInvalidType(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{
		{ID: 3, Name: "glm", Platform: service.PlatformGLM, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-glm"}, Status: service.StatusActive},
	}
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.PUT("/api/v1/admin/accounts/:id", accountHandler.Update)

	body, err := json.Marshal(map[string]any{
		"type": "oauth",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/3", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "apikey")
}

func TestAccountHandlerUpdateRejectsGLMMissingAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{
		{ID: 3, Name: "glm", Platform: service.PlatformGLM, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-glm"}, Status: service.StatusActive},
	}
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.PUT("/api/v1/admin/accounts/:id", accountHandler.Update)

	body, err := json.Marshal(map[string]any{
		"credentials": map[string]any{
			"model_mapping": map[string]any{"custom-model": "GLM-custom"},
		},
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/3", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "api_key")
}

func TestAccountHandlerUpdateRejectsInvalidExistingGLMOnOrdinaryField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{
		{ID: 3, Name: "glm", Platform: service.PlatformGLM, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": " "}, Status: service.StatusActive},
	}
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.PUT("/api/v1/admin/accounts/:id", accountHandler.Update)

	body, err := json.Marshal(map[string]any{
		"name": "renamed-glm",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/3", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), "api_key")
}

func TestAccountHandlerUpdateAllowsValidExistingGLMOnOrdinaryField(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	adminSvc.accounts = []service.Account{
		{ID: 3, Name: "glm", Platform: service.PlatformGLM, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-glm"}, Status: service.StatusActive},
	}
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.PUT("/api/v1/admin/accounts/:id", accountHandler.Update)

	body, err := json.Marshal(map[string]any{
		"name": "renamed-glm",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/3", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

func TestAccountHandlerUpdatePreservesGetAccountErrorMapping(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()
	adminSvc.getAccountErr = service.ErrAccountNotFound
	accountHandler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	router.PUT("/api/v1/admin/accounts/:id", accountHandler.Update)

	body, err := json.Marshal(map[string]any{
		"name": "renamed-account",
	})
	require.NoError(t, err)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/accounts/404", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code, rec.Body.String())
}

func TestProxyHandlerEndpoints(t *testing.T) {
	router, _ := setupAdminRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/all", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/4", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	body, _ := json.Marshal(map[string]any{"name": "proxy", "protocol": "http", "host": "localhost", "port": 8080})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	body, _ = json.Marshal(map[string]any{"name": "proxy2"})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/api/v1/admin/proxies/4", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/proxies/4", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies/batch-delete", bytes.NewBufferString(`{"ids":[1,2]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies/4/test", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/proxies/4/quality-check", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/4/stats", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/proxies/4/accounts", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestRedeemHandlerEndpoints(t *testing.T) {
	router, _ := setupAdminRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/redeem-codes", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/redeem-codes/5", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	body, _ := json.Marshal(map[string]any{"count": 1, "type": "balance", "value": 10})
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/redeem-codes", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/api/v1/admin/redeem-codes/5", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/redeem-codes/batch-delete", bytes.NewBufferString(`{"ids":[1,2]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/redeem-codes/5/expire", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/redeem-codes/5/stats", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}
