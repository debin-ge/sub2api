package admin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type availableModelsAdminService struct {
	*stubAdminService
	account service.Account
}

func (s *availableModelsAdminService) GetAccount(_ context.Context, id int64) (*service.Account, error) {
	if s.account.ID == id {
		acc := s.account
		return &acc, nil
	}
	return s.stubAdminService.GetAccount(context.Background(), id)
}

func setupAvailableModelsRouter(adminSvc service.AdminService, catalog adminModelCatalog) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.modelCatalog = catalog
	router.GET("/api/v1/admin/accounts/:id/models", handler.GetAvailableModels)
	return router
}

type syncUpstreamHTTPUpstream struct {
	resp *http.Response
	err  error
}

func (u *syncUpstreamHTTPUpstream) Do(req *http.Request, proxyURL string, accountID int64, accountConcurrency int) (*http.Response, error) {
	if u.err != nil {
		return nil, u.err
	}
	return u.resp, nil
}

func (u *syncUpstreamHTTPUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func setupSyncUpstreamModelsRouter(adminSvc service.AdminService, upstream service.HTTPUpstream) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	cfg := &config.Config{Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{Enabled: false}}}
	discoverer := service.NewUpstreamModelDiscoverer(nil, nil, nil, nil, nil, nil, upstream, cfg, nil)
	accountTestSvc := service.NewAccountTestService(
		nil,
		nil,
		nil,
		nil,
		nil,
		upstream,
		cfg,
		nil,
		discoverer,
	)
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, accountTestSvc, nil, nil, nil, nil, nil, nil, nil)
	router.POST("/api/v1/admin/accounts/:id/models/sync-upstream", handler.SyncUpstreamModels)
	return router
}

func TestAccountHandlerGetAvailableModels_GrokUsesXAIModels(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       44,
			Name:     "grok-oauth",
			Platform: service.PlatformGrok,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"grok-4.3": "grok-4.3",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc, &adminModelCatalogStub{
		accountModels: map[int64][]string{44: {"grok-4.3"}},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/44/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "grok-4.3", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_GrokDefaultsToXAIModelsWithoutMapping(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       45,
			Name:     "grok-oauth-defaults",
			Platform: service.PlatformGrok,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
		},
	}
	router := setupAvailableModelsRouter(svc, &adminModelCatalogStub{
		accountModels: map[int64][]string{45: service.DefaultModelCatalogIDs(service.PlatformGrok)},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/45/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)

	var ids []string
	for _, model := range resp.Data {
		id := model.ID
		ids = append(ids, id)
		require.NotContains(t, strings.ToLower(id), "claude")
	}
	require.Contains(t, ids, "grok-4.3")
	require.Contains(t, ids, "grok-build-0.1")
}

func TestAccountHandlerGetAvailableModels_DomesticPlatformsDoNotFallBackToClaude(t *testing.T) {
	tests := []struct {
		name          string
		platform      string
		credentials   map[string]any
		catalogModels []string
		wantContains  string
		wantForbidden string
	}{
		{
			name:     "glm",
			platform: service.PlatformGLM,
			credentials: map[string]any{
				"model_mapping": map[string]any{"glm-4.5-air": "GLM-4.5-air"},
			},
			catalogModels: []string{"glm-4.5-air"},
			wantContains:  "GLM-4.5-air",
			wantForbidden: "claude-fable-5",
		},
		{
			name:          "kimi",
			platform:      service.PlatformKimi,
			wantContains:  "kimi-for-coding",
			wantForbidden: "claude-fable-5",
		},
		{
			name:          "deepseek",
			platform:      service.PlatformDeepSeek,
			wantContains:  "deepseek-v4-flash",
			wantForbidden: "claude-fable-5",
		},
		{
			name:          "windsurf",
			platform:      service.PlatformWindsurf,
			wantContains:  "claude-sonnet-4-6",
			wantForbidden: "claude-fable-5",
		},
		{
			name:          "opencode",
			platform:      service.PlatformOpenCode,
			wantContains:  "opencode/big-pickle",
			wantForbidden: "claude-fable-5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &availableModelsAdminService{
				stubAdminService: newStubAdminService(),
				account: service.Account{
					ID:          46,
					Name:        tt.name + "-apikey",
					Platform:    tt.platform,
					Type:        service.AccountTypeAPIKey,
					Status:      service.StatusActive,
					Credentials: tt.credentials,
				},
			}
			catalogModels := tt.catalogModels
			if catalogModels == nil {
				catalogModels = []string{tt.wantContains}
			}
			router := setupAvailableModelsRouter(svc, &adminModelCatalogStub{
				accountModels: map[int64][]string{46: catalogModels},
			})

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/46/models", nil)
			router.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)

			var resp struct {
				Data []struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
			require.NotEmpty(t, resp.Data)

			ids := make([]string, 0, len(resp.Data))
			for _, model := range resp.Data {
				ids = append(ids, model.ID)
			}
			require.Contains(t, ids, tt.wantContains)
			require.NotContains(t, ids, tt.wantForbidden)
		})
	}
}

func TestAccountHandlerGetAvailableModels_OpenAIOAuthUsesExplicitModelMapping(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       42,
			Name:     "openai-oauth",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5": "gpt-5.1",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc, &adminModelCatalogStub{
		accountModels: map[int64][]string{42: {"gpt-5"}},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 1)
	require.Equal(t, "gpt-5", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_OpenAIOAuthPassthroughFallsBackToDefaults(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       43,
			Name:     "openai-oauth-passthrough",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5": "gpt-5.1",
				},
			},
			Extra: map[string]any{
				"openai_passthrough": true,
			},
		},
	}
	router := setupAvailableModelsRouter(svc, &adminModelCatalogStub{
		accountModels: map[int64][]string{43: service.DefaultModelCatalogIDs(service.PlatformOpenAI)},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/43/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.NotEmpty(t, resp.Data)
	require.NotEqual(t, "gpt-5", resp.Data[0].ID)
}

func TestAccountHandlerGetAvailableModels_OpenAISparkShadowReturnsMappingModels(t *testing.T) {
	parentID := int64(100)
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:              44,
			Name:            "openai-spark-shadow",
			Platform:        service.PlatformOpenAI,
			Type:            service.AccountTypeOAuth,
			Status:          service.StatusActive,
			ParentAccountID: &parentID,
			QuotaDimension:  service.QuotaDimensionSpark,
			Credentials: map[string]any{
				"model_mapping": map[string]any{
					"gpt-5.3-codex-spark": "gpt-5.3-codex-spark",
				},
			},
		},
	}
	router := setupAvailableModelsRouter(svc, &adminModelCatalogStub{
		accountModels: map[int64][]string{44: {"gpt-5.3-codex-spark"}},
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/44/models", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	ids := make([]string, 0, len(resp.Data))
	for _, m := range resp.Data {
		ids = append(ids, m.ID)
	}
	require.ElementsMatch(t, []string{
		"gpt-5.3-codex-spark",
	}, ids, "影子可用模型由 model_mapping 派生（非写死）")
}

func TestAccountHandlerGetAvailableModels_AntigravityPreservesProviderMetadata(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID: 47, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Status: service.StatusActive,
		},
	}
	router := setupAvailableModelsRouter(svc, &adminModelCatalogStub{
		accountModels: map[int64][]string{47: {"claude-opus-4-6", "gemini-3-pro-low"}},
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/47/models", nil))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			CreatedAt   string `json:"created_at"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "Claude Opus 4.6", resp.Data[0].DisplayName)
	require.Equal(t, "2026-02-05T00:00:00Z", resp.Data[0].CreatedAt)
	require.Equal(t, "Gemini 3 Pro Low", resp.Data[1].DisplayName)
	require.Equal(t, "2025-06-01T00:00:00Z", resp.Data[1].CreatedAt)
}

func TestAccountHandlerGetAvailableModels_DomesticPreservesGenericMetadata(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID: 48, Platform: service.PlatformWindsurf, Type: service.AccountTypeAPIKey, Status: service.StatusActive,
		},
	}
	router := setupAvailableModelsRouter(svc, &adminModelCatalogStub{
		accountModels: map[int64][]string{48: {"claude-sonnet-4-6"}},
	})

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/48/models", nil))

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var resp struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
			CreatedAt   string `json:"created_at"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "claude-sonnet-4-6", resp.Data[0].DisplayName)
	require.Equal(t, "2024-01-01T00:00:00Z", resp.Data[0].CreatedAt)
}

func TestAccountHandlerSyncUpstreamModels_ConfigErrorReturnsBadRequest(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       44,
			Name:     "openai-apikey-missing-key",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"base_url": "https://openai.example.com/v1",
			},
		},
	}
	router := setupSyncUpstreamModelsRouter(svc, &syncUpstreamHTTPUpstream{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/44/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "No OpenAI API key is available")
}

func TestAccountHandlerSyncUpstreamModels_UpstreamErrorDoesNotExposeBody(t *testing.T) {
	svc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       45,
			Name:     "openai-apikey-upstream-error",
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"api_key":  "openai-key",
				"base_url": "https://openai.example.com/v1",
			},
		},
	}
	upstream := &syncUpstreamHTTPUpstream{resp: &http.Response{
		StatusCode: http.StatusBadGateway,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"error":"SECRET_TOKEN should not be exposed"}`)),
	}}
	router := setupSyncUpstreamModelsRouter(svc, upstream)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/45/models/sync-upstream", nil)
	router.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Contains(t, rec.Body.String(), "Upstream model list request failed with HTTP 502")
	require.NotContains(t, rec.Body.String(), "SECRET_TOKEN")
}
