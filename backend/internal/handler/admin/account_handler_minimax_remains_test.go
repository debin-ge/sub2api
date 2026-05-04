package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type minimaxRemainsAdminService struct {
	*stubAdminService
	account     *service.Account
	updateInput *service.UpdateAccountInput
}

func (s *minimaxRemainsAdminService) GetAccount(ctx context.Context, id int64) (*service.Account, error) {
	copied := *s.account
	if s.account.Credentials != nil {
		copied.Credentials = map[string]any{}
		for key, value := range s.account.Credentials {
			copied.Credentials[key] = value
		}
	}
	if s.account.Extra != nil {
		copied.Extra = map[string]any{}
		for key, value := range s.account.Extra {
			copied.Extra[key] = value
		}
	}
	return &copied, nil
}

func (s *minimaxRemainsAdminService) UpdateAccount(ctx context.Context, id int64, input *service.UpdateAccountInput) (*service.Account, error) {
	s.updateInput = input
	updated := *s.account
	updated.Extra = input.Extra
	s.account = &updated
	return &updated, nil
}

func TestAccountHandlerSyncMiniMaxRemainsUpdatesExtra(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var gotAuthorization string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuthorization = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"text_5h_limit":4500,"text_5h_remaining":3200}}`))
	}))
	t.Cleanup(srv.Close)

	adminSvc := &minimaxRemainsAdminService{
		stubAdminService: newStubAdminService(),
		account: &service.Account{
			ID:       88,
			Platform: service.PlatformMiniMax,
			Type:     service.AccountTypeAPIKey,
			Status:   service.StatusActive,
			Credentials: map[string]any{
				"api_key": "sk-cp-test",
			},
			Extra: map[string]any{
				"text_5h_limit": 1000,
				"future_field":  "keep",
			},
		},
	}
	client := service.NewMiniMaxTokenPlanClient(srv.URL, srv.Client())
	h := NewAccountHandler(adminSvc, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, client)
	router := gin.New()
	router.POST("/api/v1/admin/accounts/:id/minimax/remains-sync", h.SyncMiniMaxRemains)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/88/minimax/remains-sync", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "Bearer sk-cp-test", gotAuthorization)
	require.NotNil(t, adminSvc.updateInput)
	require.Equal(t, 1000, adminSvc.updateInput.Extra["text_5h_limit"])
	require.Equal(t, "keep", adminSvc.updateInput.Extra["future_field"])
	require.Equal(t, int64(4500), adminSvc.updateInput.Extra["minimax_text_5h_limit"])
	require.Equal(t, int64(3200), adminSvc.updateInput.Extra["minimax_text_5h_remaining"])
	require.NotEmpty(t, adminSvc.updateInput.Extra["minimax_remains_synced_at"])
	require.NotNil(t, adminSvc.updateInput.Extra["minimax_remains_raw"])
}
