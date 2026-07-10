package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type adminModelCatalogStub struct {
	mu             sync.Mutex
	accountModels  map[int64][]string
	groupModels    map[int64][]string
	invalidatedIDs []int64
	refreshedIDs   []int64
	refreshed      []*service.Account
}

type modelCatalogMutationAdminService struct {
	*stubAdminService
	accountsByID      map[int64]*service.Account
	getAccountErrors  map[int64]error
	accountsByIDs     []*service.Account
	accountsByIDsSeq  [][]*service.Account
	accountsByIDsErr  error
	createdByName     map[string]*service.Account
	createErrors      map[string]error
	updatedByID       map[int64]*service.Account
	updateErrors      map[int64]error
	deletedErr        error
	clearedByID       map[int64]*service.Account
	clearErrors       map[int64]error
	schedulableByID   map[int64]*service.Account
	schedulableErrors map[int64]error
	bulkResult        *service.BulkUpdateAccountsResult
	bulkErr           error
}

func newModelCatalogMutationAdminService() *modelCatalogMutationAdminService {
	return &modelCatalogMutationAdminService{
		stubAdminService:  newStubAdminService(),
		accountsByID:      make(map[int64]*service.Account),
		getAccountErrors:  make(map[int64]error),
		createdByName:     make(map[string]*service.Account),
		createErrors:      make(map[string]error),
		updatedByID:       make(map[int64]*service.Account),
		updateErrors:      make(map[int64]error),
		clearedByID:       make(map[int64]*service.Account),
		clearErrors:       make(map[int64]error),
		schedulableByID:   make(map[int64]*service.Account),
		schedulableErrors: make(map[int64]error),
	}
}

func (s *modelCatalogMutationAdminService) GetAccount(ctx context.Context, id int64) (*service.Account, error) {
	if err := s.getAccountErrors[id]; err != nil {
		return nil, err
	}
	if account := s.accountsByID[id]; account != nil {
		return account, nil
	}
	return s.stubAdminService.GetAccount(ctx, id)
}

func (s *modelCatalogMutationAdminService) GetAccountsByIDs(ctx context.Context, ids []int64) ([]*service.Account, error) {
	if s.accountsByIDsErr != nil {
		return nil, s.accountsByIDsErr
	}
	if len(s.accountsByIDsSeq) > 0 {
		accounts := s.accountsByIDsSeq[0]
		s.accountsByIDsSeq = s.accountsByIDsSeq[1:]
		return accounts, nil
	}
	if s.accountsByIDs != nil {
		return s.accountsByIDs, nil
	}
	return s.stubAdminService.GetAccountsByIDs(ctx, ids)
}

func (s *modelCatalogMutationAdminService) CreateAccount(ctx context.Context, input *service.CreateAccountInput) (*service.Account, error) {
	if err := s.createErrors[input.Name]; err != nil {
		return nil, err
	}
	if account := s.createdByName[input.Name]; account != nil {
		return account, nil
	}
	return s.stubAdminService.CreateAccount(ctx, input)
}

func (s *modelCatalogMutationAdminService) UpdateAccount(ctx context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	if err := s.updateErrors[id]; err != nil {
		return nil, err
	}
	if account := s.updatedByID[id]; account != nil {
		return account, nil
	}
	return s.stubAdminService.UpdateAccount(ctx, id, input)
}

func (s *modelCatalogMutationAdminService) DeleteAccount(_ context.Context, _ int64) error {
	return s.deletedErr
}

func (s *modelCatalogMutationAdminService) ClearAccountError(ctx context.Context, id int64) (*service.Account, error) {
	if err := s.clearErrors[id]; err != nil {
		return nil, err
	}
	if account := s.clearedByID[id]; account != nil {
		return account, nil
	}
	return s.stubAdminService.ClearAccountError(ctx, id)
}

func (s *modelCatalogMutationAdminService) SetAccountSchedulable(ctx context.Context, id int64, schedulable bool) (*service.Account, error) {
	if err := s.schedulableErrors[id]; err != nil {
		return nil, err
	}
	if account := s.schedulableByID[id]; account != nil {
		return account, nil
	}
	return s.stubAdminService.SetAccountSchedulable(ctx, id, schedulable)
}

func (s *modelCatalogMutationAdminService) BulkUpdateAccounts(ctx context.Context, input *service.BulkUpdateAccountsInput) (*service.BulkUpdateAccountsResult, error) {
	if s.bulkErr != nil {
		return nil, s.bulkErr
	}
	if s.bulkResult != nil {
		return s.bulkResult, nil
	}
	return s.stubAdminService.BulkUpdateAccounts(ctx, input)
}

type adminClaudeOAuthClientStub struct {
	refreshToken func(string) (*oauth.TokenResponse, error)
}

type adminAntigravityOAuthServiceStub struct {
	tokenInfo *service.AntigravityTokenInfo
	err       error
}

type proxyMutationAccountRepo struct {
	service.AccountRepository
	accounts        map[int64]*service.Account
	shadowsByParent map[int64][]int64
}

func newProxyMutationAccountRepo(accounts ...*service.Account) *proxyMutationAccountRepo {
	repo := &proxyMutationAccountRepo{
		accounts:        make(map[int64]*service.Account, len(accounts)),
		shadowsByParent: make(map[int64][]int64),
	}
	for _, account := range accounts {
		cloned := *account
		repo.accounts[account.ID] = &cloned
		if account.ParentAccountID != nil {
			repo.shadowsByParent[*account.ParentAccountID] = append(repo.shadowsByParent[*account.ParentAccountID], account.ID)
		}
	}
	return repo
}

func (r *proxyMutationAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	account := r.accounts[id]
	if account == nil {
		return nil, service.ErrAccountNotFound
	}
	cloned := *account
	return &cloned, nil
}

func (r *proxyMutationAccountRepo) GetByIDs(ctx context.Context, ids []int64) ([]*service.Account, error) {
	accounts := make([]*service.Account, 0, len(ids))
	for _, id := range ids {
		account, err := r.GetByID(ctx, id)
		if err == nil {
			accounts = append(accounts, account)
		}
	}
	return accounts, nil
}

func (r *proxyMutationAccountRepo) Update(_ context.Context, account *service.Account) error {
	cloned := *account
	r.accounts[account.ID] = &cloned
	return nil
}

func (r *proxyMutationAccountRepo) ListShadowsByParent(ctx context.Context, parentID int64) ([]*service.Account, error) {
	return r.GetByIDs(ctx, r.shadowsByParent[parentID])
}

func (r *proxyMutationAccountRepo) RevertProxyFallback(_ context.Context, accountID int64) error {
	account := r.accounts[accountID]
	if account == nil {
		return service.ErrAccountNotFound
	}
	if account.ProxyFallbackOriginID == nil {
		return service.ErrAccountNotInFallback
	}
	account.ProxyID = account.ProxyFallbackOriginID
	account.ProxyFallbackOriginID = nil
	return nil
}

func (s *adminAntigravityOAuthServiceStub) RefreshAccountToken(context.Context, *service.Account) (*service.AntigravityTokenInfo, error) {
	return s.tokenInfo, s.err
}

func (s *adminAntigravityOAuthServiceStub) BuildAccountCredentials(tokenInfo *service.AntigravityTokenInfo) map[string]any {
	return map[string]any{
		"access_token":  tokenInfo.AccessToken,
		"refresh_token": tokenInfo.RefreshToken,
		"project_id":    tokenInfo.ProjectID,
	}
}

func (s *adminClaudeOAuthClientStub) GetOrganizationUUID(context.Context, string, string) (string, error) {
	panic("unexpected GetOrganizationUUID call")
}

func (s *adminClaudeOAuthClientStub) GetAuthorizationCode(context.Context, string, string, string, string, string, string) (string, error) {
	panic("unexpected GetAuthorizationCode call")
}

func (s *adminClaudeOAuthClientStub) ExchangeCodeForToken(context.Context, string, string, string, string, bool) (*oauth.TokenResponse, error) {
	panic("unexpected ExchangeCodeForToken call")
}

func (s *adminClaudeOAuthClientStub) RefreshToken(_ context.Context, refreshToken, _ string) (*oauth.TokenResponse, error) {
	return s.refreshToken(refreshToken)
}

func setupModelCatalogMutationRouter(adminSvc service.AdminService, oauthSvc *service.OAuthService, catalog adminModelCatalog) *gin.Engine {
	gin.SetMode(gin.TestMode)
	handler := NewAccountHandler(adminSvc, oauthSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.modelCatalog = catalog
	router := gin.New()
	router.POST("/accounts", handler.Create)
	router.PUT("/accounts/:id", handler.Update)
	router.DELETE("/accounts/:id", handler.Delete)
	router.POST("/accounts/:id/refresh", handler.Refresh)
	router.POST("/accounts/:id/apply-oauth-credentials", handler.ApplyOAuthCredentials)
	router.POST("/accounts/:id/schedulable", handler.SetSchedulable)
	router.POST("/accounts/batch", handler.BatchCreate)
	router.POST("/accounts/batch-refresh", handler.BatchRefresh)
	router.POST("/accounts/batch-update-credentials", handler.BatchUpdateCredentials)
	router.POST("/accounts/bulk-update", handler.BulkUpdate)
	return router
}

func setupAntigravityRefreshMutationRouter(adminSvc service.AdminService, catalog adminModelCatalog) *gin.Engine {
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.antigravityOAuthService = &adminAntigravityOAuthServiceStub{tokenInfo: &service.AntigravityTokenInfo{
		AccessToken: "new-access", RefreshToken: "new-refresh", ProjectID: "project-1",
	}}
	handler.modelCatalog = catalog
	router := gin.New()
	router.POST("/accounts/:id/refresh", handler.Refresh)
	router.POST("/accounts/batch-refresh", handler.BatchRefresh)
	return router
}

func setupProxyMutationCatalogRouter(repo service.AccountRepository, catalog adminModelCatalog) *gin.Engine {
	adminSvc := service.NewAdminService(
		nil, nil, repo, nil, nil, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	handler := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	handler.modelCatalog = catalog
	router := gin.New()
	router.PUT("/accounts/:id", handler.Update)
	router.POST("/accounts/:id/revert-proxy-fallback", handler.RevertProxyFallback)
	return router
}

func performCatalogJSONRequest(t *testing.T, router http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	payload, err := json.Marshal(body)
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	return rec
}

func (s *adminModelCatalogStub) ListForAccount(_ context.Context, account *service.Account, _ bool) ([]string, error) {
	return append([]string(nil), s.accountModels[account.ID]...), nil
}

func (s *adminModelCatalogStub) ListGroupCandidates(_ context.Context, groupID int64, _ string) ([]string, error) {
	return append([]string(nil), s.groupModels[groupID]...), nil
}

func (s *adminModelCatalogStub) InvalidateAccount(id int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.invalidatedIDs = append(s.invalidatedIDs, id)
}

func (s *adminModelCatalogStub) RefreshAccountAsync(account *service.Account) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshedIDs = append(s.refreshedIDs, account.ID)
	cloned := *account
	cloned.Credentials = make(map[string]any, len(account.Credentials))
	for key, value := range account.Credentials {
		cloned.Credentials[key] = value
	}
	s.refreshed = append(s.refreshed, &cloned)
}

func requireCatalogMutation(t *testing.T, catalog *adminModelCatalogStub, invalidated, refreshed []int64) {
	t.Helper()
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	require.ElementsMatch(t, invalidated, catalog.invalidatedIDs)
	require.ElementsMatch(t, refreshed, catalog.refreshedIDs)
}

func requireSingleCatalogRefreshCredential(t *testing.T, catalog *adminModelCatalogStub, key string, want any) {
	t.Helper()
	catalog.mu.Lock()
	defer catalog.mu.Unlock()
	require.Len(t, catalog.refreshed, 1)
	require.Equal(t, want, catalog.refreshed[0].Credentials[key])
}

func TestAccountHandlerGetAvailableModels_UsesCatalog(t *testing.T) {
	adminSvc := &availableModelsAdminService{
		stubAdminService: newStubAdminService(),
		account: service.Account{
			ID:       42,
			Platform: service.PlatformOpenAI,
			Type:     service.AccountTypeOAuth,
		},
	}
	catalog := &adminModelCatalogStub{accountModels: map[int64][]string{42: {"gpt-live-new"}}}
	router := setupAvailableModelsRouter(adminSvc, catalog)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/42/models", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), "gpt-live-new")
}

func TestAdminModelBuilders_PreserveProviderMetadataFallbacksAndOrder(t *testing.T) {
	t.Run("openai", func(t *testing.T) {
		known := openai.DefaultModels[0]
		models := buildOpenAIAdminModels([]string{"openai-live-unknown", known.ID})
		require.Equal(t, []openai.Model{
			{ID: "openai-live-unknown", Object: "model", Type: "model", DisplayName: "openai-live-unknown"},
			known,
		}, models)
	})

	t.Run("gemini", func(t *testing.T) {
		known := geminicli.DefaultModels[0]
		models := buildGeminiAdminModels([]string{"gemini-live-unknown", known.ID})
		require.Equal(t, []geminicli.Model{
			{ID: "gemini-live-unknown", Type: "model", DisplayName: "gemini-live-unknown", CreatedAt: ""},
			known,
		}, models)
	})

	t.Run("grok", func(t *testing.T) {
		known := xai.DefaultModels()[0]
		models := buildGrokAdminModels([]string{"grok-live-unknown", known.ID})
		require.Equal(t, []xai.Model{
			{ID: "grok-live-unknown", Object: "model", OwnedBy: "xai", DisplayName: "grok-live-unknown"},
			known,
		}, models)
	})

	t.Run("claude shape", func(t *testing.T) {
		known := claude.DefaultModels[0]
		models := buildClaudeShapeAdminModels([]string{"claude-live-unknown", known.ID})
		require.Equal(t, []claude.Model{
			{ID: "claude-live-unknown", Type: "model", DisplayName: "claude-live-unknown", CreatedAt: ""},
			known,
		}, models)
	})
}

func TestAdminModelBuilders_GLMDefaultOrderPreserved(t *testing.T) {
	require.Equal(t, []string{"GLM-5.1", "GLM-4.7", "GLM-4.5-air"}, service.DefaultGLMModelIDs())

	models := buildGLMAdminModels([]string{"glm-5.1", "GLM-4.7", "glm-4.5-air", "GLM-5.1"})
	modelIDs := make([]string, 0, len(models))
	for _, model := range models {
		modelIDs = append(modelIDs, model.ID)
	}

	require.Equal(t, []string{"GLM-5.1", "GLM-4.7", "GLM-4.5-air"}, modelIDs)
	require.Equal(t, "GLM-5.1", models[0].ID, "newest provider default must remain first")
}

func TestGroupModelsListCandidates_UsesCatalog(t *testing.T) {
	adminSvc := newStubAdminService()
	catalog := &adminModelCatalogStub{groupModels: map[int64][]string{2: {"second", "first"}}}
	handler := NewGroupHandler(adminSvc, nil, nil, nil)
	handler.modelCatalog = catalog
	router := gin.New()
	router.GET("/groups/:id/models-list-candidates", handler.GetModelsListCandidates)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/groups/2/models-list-candidates?platform=openai", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	require.Less(t, strings.Index(rec.Body.String(), "second"), strings.Index(rec.Body.String(), "first"))
}

func TestAccountHandlerModelCatalogAccountChanged(t *testing.T) {
	catalog := &adminModelCatalogStub{}
	handler := &AccountHandler{modelCatalog: catalog}

	handler.modelCatalogAccountChanged(&service.Account{ID: 7, Status: service.StatusActive, Schedulable: true})
	handler.modelCatalogAccountChanged(&service.Account{ID: 8, Status: service.StatusDisabled, Schedulable: true})
	handler.modelCatalogAccountChanged(&service.Account{ID: 9, Status: service.StatusActive, Schedulable: false})

	require.Equal(t, []int64{7, 8, 9}, catalog.invalidatedIDs)
	require.Equal(t, []int64{7}, catalog.refreshedIDs)
}

func TestAccountHandlerModelCatalogChangeHelpers_NilCatalogAreNoops(t *testing.T) {
	handler := &AccountHandler{}

	require.NotPanics(t, func() {
		handler.modelCatalogAccountChanged(&service.Account{ID: 1, Status: service.StatusActive, Schedulable: true})
		handler.modelCatalogAccountsChanged(context.Background(), []int64{1})
	})
}

func TestAccountHandlerModelCatalogMutationFailed_UsesPersistedMutationIDs(t *testing.T) {
	adminSvc := newModelCatalogMutationAdminService()
	adminSvc.accountsByIDs = []*service.Account{
		{ID: 10, Status: service.StatusActive, Schedulable: true},
		{ID: 11, Status: service.StatusDisabled, Schedulable: true},
	}
	catalog := &adminModelCatalogStub{}
	handler := &AccountHandler{adminService: adminSvc, modelCatalog: catalog}

	handler.modelCatalogMutationFailed(context.Background(), &service.AccountMutationError{
		Cause:             errors.New("post-write failure"),
		MutatedAccountIDs: []int64{10, 11},
	})

	requireCatalogMutation(t, catalog, []int64{10, 11}, []int64{10})
}

func TestAdminHandlerConstructors_NilCatalogKeepsNilInterface(t *testing.T) {
	accountHandler := NewAccountHandler(newStubAdminService(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	groupHandler := NewGroupHandler(newStubAdminService(), nil, nil, nil)

	require.True(t, accountHandler.modelCatalog == nil)
	require.True(t, groupHandler.modelCatalog == nil)
}

func TestAccountHandlerCreate_ModelCatalogMutationFollowsWriteResult(t *testing.T) {
	t.Run("successful active schedulable account", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.createdByName["created"] = &service.Account{
			ID: 101, Name: "created", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true,
		}
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts", map[string]any{
			"name": "created", "platform": service.PlatformAnthropic, "type": service.AccountTypeOAuth,
			"credentials": map[string]any{"refresh_token": "refresh"},
		})

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		requireCatalogMutation(t, catalog, []int64{101}, []int64{101})
	})

	t.Run("failed write", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.createErrors["failed"] = errors.New("create failed")
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		performCatalogJSONRequest(t, router, http.MethodPost, "/accounts", map[string]any{
			"name": "failed", "platform": service.PlatformAnthropic, "type": service.AccountTypeOAuth,
			"credentials": map[string]any{"refresh_token": "refresh"},
		})

		requireCatalogMutation(t, catalog, nil, nil)
	})
}

func TestAccountHandlerUpdate_ModelCatalogMutationFollowsWriteResult(t *testing.T) {
	t.Run("successful active schedulable account", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.accountsByID[102] = &service.Account{ID: 102, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth}
		adminSvc.updatedByID[102] = &service.Account{ID: 102, Status: service.StatusActive, Schedulable: true}
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		rec := performCatalogJSONRequest(t, router, http.MethodPut, "/accounts/102", map[string]any{"name": "updated"})

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		requireCatalogMutation(t, catalog, []int64{102}, []int64{102})
	})

	t.Run("failed write", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.accountsByID[103] = &service.Account{ID: 103, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth}
		adminSvc.updateErrors[103] = errors.New("update failed")
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		performCatalogJSONRequest(t, router, http.MethodPut, "/accounts/103", map[string]any{"name": "updated"})

		requireCatalogMutation(t, catalog, nil, nil)
	})
}

func TestAccountHandlerUpdate_ModelCatalogIncludesSuccessfulShadows(t *testing.T) {
	parentID := int64(201)
	oldProxyID := int64(3)
	repo := newProxyMutationAccountRepo(
		&service.Account{
			ID: parentID, Name: "parent", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true, ProxyID: &oldProxyID,
		},
		&service.Account{
			ID: 202, Name: "shadow", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true, ParentAccountID: &parentID, QuotaDimension: "spark",
		},
	)
	catalog := &adminModelCatalogStub{}
	router := setupProxyMutationCatalogRouter(repo, catalog)

	rec := performCatalogJSONRequest(t, router, http.MethodPut, "/accounts/201", map[string]any{"proxy_id": 9})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	requireCatalogMutation(t, catalog, []int64{201, 202}, []int64{201, 202})
}

func TestAccountHandlerRevertProxyFallback_ModelCatalogIncludesSuccessfulShadows(t *testing.T) {
	parentID := int64(211)
	fallbackProxyID := int64(8)
	originalProxyID := int64(4)
	repo := newProxyMutationAccountRepo(
		&service.Account{
			ID: parentID, Name: "parent", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true, ProxyID: &fallbackProxyID, ProxyFallbackOriginID: &originalProxyID,
		},
		&service.Account{
			ID: 212, Name: "shadow", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true, ParentAccountID: &parentID, QuotaDimension: "spark",
		},
	)
	catalog := &adminModelCatalogStub{}
	router := setupProxyMutationCatalogRouter(repo, catalog)

	rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/211/revert-proxy-fallback", nil)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	requireCatalogMutation(t, catalog, []int64{211, 212}, []int64{211, 212})
}

func TestAccountHandlerDelete_ModelCatalogMutationFollowsWriteResult(t *testing.T) {
	t.Run("successful deletion only invalidates", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		rec := performCatalogJSONRequest(t, router, http.MethodDelete, "/accounts/104", nil)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		requireCatalogMutation(t, catalog, []int64{104}, nil)
	})

	t.Run("failed deletion", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.deletedErr = errors.New("delete failed")
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		performCatalogJSONRequest(t, router, http.MethodDelete, "/accounts/105", nil)

		requireCatalogMutation(t, catalog, nil, nil)
	})
}

func TestAccountHandlerBulkUpdate_ModelCatalogUsesOnlySuccessfulSurvivors(t *testing.T) {
	t.Run("mixed surviving account states", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.bulkResult = &service.BulkUpdateAccountsResult{Success: 3, SuccessIDs: []int64{111, 112, 113}}
		adminSvc.accountsByIDs = []*service.Account{
			{ID: 111, Status: service.StatusActive, Schedulable: true},
			{ID: 112, Status: service.StatusDisabled, Schedulable: true},
			{ID: 113, Status: service.StatusActive, Schedulable: false},
		}
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/bulk-update", map[string]any{
			"account_ids": []int64{111, 112, 113, 114}, "name": "updated",
		})

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		requireCatalogMutation(t, catalog, []int64{111, 112, 113}, []int64{111})
	})

	t.Run("reload failure still invalidates successful ids", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.bulkResult = &service.BulkUpdateAccountsResult{Success: 2, SuccessIDs: []int64{115, 116}}
		adminSvc.accountsByIDsErr = errors.New("reload failed")
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/bulk-update", map[string]any{
			"account_ids": []int64{115, 116}, "name": "updated",
		})

		requireCatalogMutation(t, catalog, []int64{115, 116}, nil)
	})

	t.Run("partial group failure invalidates all persisted updates without exposing internal ids", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.bulkResult = &service.BulkUpdateAccountsResult{
			Success: 1, Failed: 1,
			SuccessIDs: []int64{118}, FailedIDs: []int64{119}, UpdatedIDs: []int64{118, 119},
			Results: []service.BulkUpdateAccountResult{
				{AccountID: 118, Success: true},
				{AccountID: 119, Success: false, Error: "bind failed"},
			},
		}
		adminSvc.accountsByIDs = []*service.Account{
			{ID: 118, Status: service.StatusActive, Schedulable: true},
			{ID: 119, Status: service.StatusActive, Schedulable: true},
		}
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/bulk-update", map[string]any{
			"account_ids": []int64{118, 119},
			"credentials": map[string]any{"model_mapping": map[string]any{"gpt-new": "gpt-new"}},
			"group_ids":   []int64{10},
		})

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		require.NotContains(t, rec.Body.String(), "updated_ids")
		requireCatalogMutation(t, catalog, []int64{118, 119}, []int64{118, 119})
	})

	t.Run("failed write", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.bulkErr = errors.New("bulk failed")
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/bulk-update", map[string]any{
			"account_ids": []int64{117}, "name": "updated",
		})

		requireCatalogMutation(t, catalog, nil, nil)
	})
}

func TestAccountHandlerBulkUpdate_GroupOnlyCatalogUsesSuccessfulBindings(t *testing.T) {
	adminSvc := newModelCatalogMutationAdminService()
	adminSvc.bulkResult = &service.BulkUpdateAccountsResult{
		Success: 1, Failed: 1,
		SuccessIDs: []int64{221}, FailedIDs: []int64{222}, UpdatedIDs: []int64{221},
		Results: []service.BulkUpdateAccountResult{
			{AccountID: 221, Success: true},
			{AccountID: 222, Success: false, Error: "bind failed"},
		},
	}
	adminSvc.accountsByIDs = []*service.Account{{ID: 221, Status: service.StatusActive, Schedulable: true}}
	catalog := &adminModelCatalogStub{}
	router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

	rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/bulk-update", map[string]any{
		"account_ids": []int64{221, 222}, "group_ids": []int64{10},
	})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), "updated_ids")
	requireCatalogMutation(t, catalog, []int64{221}, []int64{221})
}

func TestAccountHandlerBatchUpdateCredentials_ModelCatalogUsesSuccessfulResults(t *testing.T) {
	adminSvc := newModelCatalogMutationAdminService()
	adminSvc.updatedByID[121] = &service.Account{ID: 121, Status: service.StatusActive, Schedulable: true}
	adminSvc.updateErrors[122] = errors.New("credential update failed")
	adminSvc.updatedByID[123] = &service.Account{ID: 123, Status: service.StatusDisabled, Schedulable: true}
	catalog := &adminModelCatalogStub{}
	router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

	rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/batch-update-credentials", map[string]any{
		"account_ids": []int64{121, 122, 123}, "field": "account_uuid", "value": "updated-uuid",
	})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	requireCatalogMutation(t, catalog, []int64{121, 123}, []int64{121})
}

func TestAccountHandlerBatchUpdateCredentials_PrevalidationFailureDoesNotMutateCatalog(t *testing.T) {
	adminSvc := newModelCatalogMutationAdminService()
	adminSvc.getAccountErrors[124] = errors.New("account missing")
	catalog := &adminModelCatalogStub{}
	router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

	performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/batch-update-credentials", map[string]any{
		"account_ids": []int64{124, 125}, "field": "account_uuid", "value": "updated-uuid",
	})

	requireCatalogMutation(t, catalog, nil, nil)
}

func TestAccountHandlerApplyOAuthCredentials_ModelCatalogMutationFollowsPrimaryWrite(t *testing.T) {
	t.Run("successful application uses final account state", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.accountsByID[131] = &service.Account{ID: 131, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth}
		adminSvc.updatedByID[131] = &service.Account{ID: 131, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true}
		adminSvc.clearedByID[131] = adminSvc.updatedByID[131]
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/131/apply-oauth-credentials", map[string]any{
			"type": service.AccountTypeOAuth, "credentials": map[string]any{"refresh_token": "new-refresh"},
		})

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		requireCatalogMutation(t, catalog, []int64{131}, []int64{131})
	})

	t.Run("failed primary write", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.accountsByID[132] = &service.Account{ID: 132, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth}
		adminSvc.updateErrors[132] = errors.New("oauth application failed")
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/132/apply-oauth-credentials", map[string]any{
			"type": service.AccountTypeOAuth, "credentials": map[string]any{"refresh_token": "new-refresh"},
		})

		requireCatalogMutation(t, catalog, nil, nil)
	})
}

func TestAccountHandlerApplyOAuthCredentials_PostClearFailureKeepsReloadedRefreshCurrent(t *testing.T) {
	adminSvc := newModelCatalogMutationAdminService()
	adminSvc.accountsByID[133] = &service.Account{
		ID: 133, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
		Status: service.StatusError, Schedulable: true,
	}
	adminSvc.updatedByID[133] = &service.Account{
		ID: 133, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
		Status: service.StatusError, Schedulable: true,
	}
	adminSvc.clearErrors[133] = &service.AccountMutationError{
		Cause:             errors.New("post-clear failure"),
		MutatedAccountIDs: []int64{133},
	}
	adminSvc.accountsByIDs = []*service.Account{{
		ID: 133, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
	}}
	catalog := &adminModelCatalogStub{}
	router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

	rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/133/apply-oauth-credentials", map[string]any{
		"type": service.AccountTypeOAuth, "credentials": map[string]any{"refresh_token": "new-refresh"},
	})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	requireCatalogMutation(t, catalog, []int64{133}, []int64{133})
}

func TestAccountHandlerSetSchedulable_ModelCatalogMutationFollowsReturnedState(t *testing.T) {
	t.Run("enable active account", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.schedulableByID[141] = &service.Account{ID: 141, Status: service.StatusActive, Schedulable: true}
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/141/schedulable", map[string]any{"schedulable": true})

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		requireCatalogMutation(t, catalog, []int64{141}, []int64{141})
	})

	t.Run("disable active account", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.schedulableByID[142] = &service.Account{ID: 142, Status: service.StatusActive, Schedulable: false}
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/142/schedulable", map[string]any{"schedulable": false})

		requireCatalogMutation(t, catalog, []int64{142}, nil)
	})

	t.Run("failed write", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.schedulableErrors[143] = errors.New("toggle failed")
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

		performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/143/schedulable", map[string]any{"schedulable": true})

		requireCatalogMutation(t, catalog, nil, nil)
	})
}

func TestAccountHandlerBatchCreate_ModelCatalogUsesSuccessfulResults(t *testing.T) {
	adminSvc := newModelCatalogMutationAdminService()
	adminSvc.createdByName["active"] = &service.Account{ID: 151, Name: "active", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true}
	adminSvc.createdByName["disabled"] = &service.Account{ID: 152, Name: "disabled", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusDisabled, Schedulable: true}
	adminSvc.createErrors["failed"] = errors.New("batch create failed")
	catalog := &adminModelCatalogStub{}
	router := setupModelCatalogMutationRouter(adminSvc, nil, catalog)

	rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/batch", map[string]any{
		"accounts": []map[string]any{
			{"name": "active", "platform": service.PlatformAnthropic, "type": service.AccountTypeOAuth, "credentials": map[string]any{"refresh_token": "one"}},
			{"name": "disabled", "platform": service.PlatformAnthropic, "type": service.AccountTypeOAuth, "credentials": map[string]any{"refresh_token": "two"}},
			{"name": "failed", "platform": service.PlatformAnthropic, "type": service.AccountTypeOAuth, "credentials": map[string]any{"refresh_token": "three"}},
		},
	})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	requireCatalogMutation(t, catalog, []int64{151, 152}, []int64{151})
}

func TestAccountHandlerRefresh_ModelCatalogMutationFollowsCredentialWrite(t *testing.T) {
	t.Run("successful refresh", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.accountsByID[161] = &service.Account{
			ID: 161, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"refresh_token": "good"},
		}
		adminSvc.updatedByID[161] = &service.Account{
			ID: 161, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true,
		}
		oauthSvc := service.NewOAuthService(nil, &adminClaudeOAuthClientStub{refreshToken: func(string) (*oauth.TokenResponse, error) {
			return &oauth.TokenResponse{AccessToken: "new-access", TokenType: "Bearer", ExpiresIn: 3600}, nil
		}})
		defer oauthSvc.Stop()
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, oauthSvc, catalog)

		rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/161/refresh", nil)

		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		requireCatalogMutation(t, catalog, []int64{161}, []int64{161})
	})

	t.Run("failed refresh", func(t *testing.T) {
		adminSvc := newModelCatalogMutationAdminService()
		adminSvc.accountsByID[162] = &service.Account{
			ID: 162, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth,
			Status: service.StatusActive, Schedulable: true,
			Credentials: map[string]any{"refresh_token": "bad"},
		}
		oauthSvc := service.NewOAuthService(nil, &adminClaudeOAuthClientStub{refreshToken: func(string) (*oauth.TokenResponse, error) {
			return nil, errors.New("refresh failed")
		}})
		defer oauthSvc.Stop()
		catalog := &adminModelCatalogStub{}
		router := setupModelCatalogMutationRouter(adminSvc, oauthSvc, catalog)

		performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/162/refresh", nil)

		requireCatalogMutation(t, catalog, nil, nil)
	})
}

func TestAccountHandlerBatchRefresh_ModelCatalogUsesSuccessfulRefreshResults(t *testing.T) {
	adminSvc := newModelCatalogMutationAdminService()
	adminSvc.accountsByIDs = []*service.Account{
		{ID: 171, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"refresh_token": "good-active"}},
		{ID: 172, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, Credentials: map[string]any{"refresh_token": "bad"}},
		{ID: 173, Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusDisabled, Schedulable: true, Credentials: map[string]any{"refresh_token": "good-disabled"}},
	}
	adminSvc.updatedByID[171] = &service.Account{ID: 171, Status: service.StatusActive, Schedulable: true}
	adminSvc.updatedByID[173] = &service.Account{ID: 173, Status: service.StatusDisabled, Schedulable: true}
	oauthSvc := service.NewOAuthService(nil, &adminClaudeOAuthClientStub{refreshToken: func(refreshToken string) (*oauth.TokenResponse, error) {
		if refreshToken == "bad" {
			return nil, errors.New("refresh failed")
		}
		return &oauth.TokenResponse{AccessToken: "new-access", TokenType: "Bearer", ExpiresIn: 3600}, nil
	}})
	defer oauthSvc.Stop()
	catalog := &adminModelCatalogStub{}
	router := setupModelCatalogMutationRouter(adminSvc, oauthSvc, catalog)

	rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/batch-refresh", map[string]any{
		"account_ids": []int64{171, 172, 173},
	})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	requireCatalogMutation(t, catalog, []int64{171, 173}, []int64{171})
}

func TestAccountHandlerRefresh_ClearSuccessThenCredentialWriteFailureMutatesCatalog(t *testing.T) {
	adminSvc := newModelCatalogMutationAdminService()
	adminSvc.accountsByID[181] = &service.Account{
		ID: 181, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusError, Schedulable: true, ErrorMessage: "missing_project_id: retry",
		Credentials: map[string]any{"refresh_token": "old-refresh"},
	}
	adminSvc.clearedByID[181] = &service.Account{
		ID: 181, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
	}
	adminSvc.updateErrors[181] = errors.New("credential update failed")
	adminSvc.accountsByIDs = []*service.Account{{
		ID: 181, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
	}}
	catalog := &adminModelCatalogStub{}
	router := setupAntigravityRefreshMutationRouter(adminSvc, catalog)

	performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/181/refresh", nil)

	requireCatalogMutation(t, catalog, []int64{181}, []int64{181})
}

func TestAccountHandlerBatchRefresh_ClearSuccessThenCredentialWriteFailureMutatesCatalog(t *testing.T) {
	adminSvc := newModelCatalogMutationAdminService()
	initial := []*service.Account{{
		ID: 182, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusError, Schedulable: true, ErrorMessage: "missing_project_id: retry",
		Credentials: map[string]any{"refresh_token": "old-refresh"},
	}}
	current := []*service.Account{{
		ID: 182, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
	}}
	adminSvc.accountsByIDsSeq = [][]*service.Account{initial, current}
	adminSvc.clearedByID[182] = &service.Account{
		ID: 182, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
	}
	adminSvc.updateErrors[182] = errors.New("credential update failed")
	catalog := &adminModelCatalogStub{}
	router := setupAntigravityRefreshMutationRouter(adminSvc, catalog)

	rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/batch-refresh", map[string]any{
		"account_ids": []int64{182},
	})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	requireCatalogMutation(t, catalog, []int64{182}, []int64{182})
}

func TestAccountHandlerRefresh_ReceiptFailureUsesOneCurrentCatalogRefresh(t *testing.T) {
	adminSvc := newModelCatalogMutationAdminService()
	adminSvc.accountsByID[191] = &service.Account{
		ID: 191, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusError, Schedulable: true, ErrorMessage: "missing_project_id: retry",
		Credentials: map[string]any{"refresh_token": "old-refresh"},
	}
	adminSvc.clearedByID[191] = &service.Account{
		ID: 191, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "cleared-stale"},
	}
	adminSvc.updateErrors[191] = &service.AccountMutationError{
		Cause: errors.New("post-update failure"), MutatedAccountIDs: []int64{191},
	}
	adminSvc.accountsByIDs = []*service.Account{{
		ID: 191, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "persisted-current"},
	}}
	catalog := &adminModelCatalogStub{}
	router := setupAntigravityRefreshMutationRouter(adminSvc, catalog)

	performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/191/refresh", nil)

	requireCatalogMutation(t, catalog, []int64{191}, []int64{191})
	requireSingleCatalogRefreshCredential(t, catalog, "access_token", "persisted-current")
}

func TestAccountHandlerBatchRefresh_ReceiptFailureUsesOneCurrentCatalogRefresh(t *testing.T) {
	adminSvc := newModelCatalogMutationAdminService()
	initial := []*service.Account{{
		ID: 192, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusError, Schedulable: true, ErrorMessage: "missing_project_id: retry",
		Credentials: map[string]any{"refresh_token": "old-refresh"},
	}}
	current := []*service.Account{{
		ID: 192, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "persisted-current"},
	}}
	adminSvc.accountsByIDsSeq = [][]*service.Account{initial, current}
	adminSvc.clearedByID[192] = &service.Account{
		ID: 192, Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true,
		Credentials: map[string]any{"access_token": "cleared-stale"},
	}
	adminSvc.updateErrors[192] = &service.AccountMutationError{
		Cause: errors.New("post-update failure"), MutatedAccountIDs: []int64{192},
	}
	catalog := &adminModelCatalogStub{}
	router := setupAntigravityRefreshMutationRouter(adminSvc, catalog)

	rec := performCatalogJSONRequest(t, router, http.MethodPost, "/accounts/batch-refresh", map[string]any{
		"account_ids": []int64{192},
	})

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	requireCatalogMutation(t, catalog, []int64{192}, []int64{192})
	requireSingleCatalogRefreshCredential(t, catalog, "access_token", "persisted-current")
}
