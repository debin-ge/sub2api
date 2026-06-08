package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type codingPlanAccountTestRepo struct {
	account     *Account
	setErrorID  int64
	setErrorMsg string
}

func (r *codingPlanAccountTestRepo) Create(context.Context, *Account) error { return nil }
func (r *codingPlanAccountTestRepo) GetByID(context.Context, int64) (*Account, error) {
	if r.account == nil {
		return nil, fmt.Errorf("account not found")
	}
	return r.account, nil
}
func (r *codingPlanAccountTestRepo) GetByIDs(context.Context, []int64) ([]*Account, error) {
	return []*Account{r.account}, nil
}
func (r *codingPlanAccountTestRepo) ExistsByID(context.Context, int64) (bool, error) {
	return true, nil
}
func (r *codingPlanAccountTestRepo) GetByCRSAccountID(context.Context, string) (*Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) FindByExtraField(context.Context, string, any) ([]Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) ListCRSAccountIDs(context.Context) (map[string]int64, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) Update(context.Context, *Account) error { return nil }
func (r *codingPlanAccountTestRepo) Delete(context.Context, int64) error    { return nil }
func (r *codingPlanAccountTestRepo) List(context.Context, pagination.PaginationParams) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *codingPlanAccountTestRepo) ListWithFilters(context.Context, pagination.PaginationParams, string, string, string, string, int64, string) ([]Account, *pagination.PaginationResult, error) {
	return nil, nil, nil
}
func (r *codingPlanAccountTestRepo) ListByGroup(context.Context, int64) ([]Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) ListActive(context.Context) ([]Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) ListByPlatform(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) UpdateLastUsed(context.Context, int64) error { return nil }
func (r *codingPlanAccountTestRepo) BatchUpdateLastUsed(context.Context, map[int64]time.Time) error {
	return nil
}
func (r *codingPlanAccountTestRepo) SetError(_ context.Context, id int64, msg string) error {
	r.setErrorID = id
	r.setErrorMsg = msg
	return nil
}
func (r *codingPlanAccountTestRepo) ClearError(context.Context, int64) error { return nil }
func (r *codingPlanAccountTestRepo) SetSchedulable(context.Context, int64, bool) error {
	return nil
}
func (r *codingPlanAccountTestRepo) AutoPauseExpiredAccounts(context.Context, time.Time) (int64, error) {
	return 0, nil
}
func (r *codingPlanAccountTestRepo) BindGroups(context.Context, int64, []int64) error { return nil }
func (r *codingPlanAccountTestRepo) ListSchedulable(context.Context) ([]Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) ListSchedulableByGroupID(context.Context, int64) ([]Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) ListSchedulableByPlatform(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) ListSchedulableByGroupIDAndPlatform(context.Context, int64, string) ([]Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) ListSchedulableByPlatforms(context.Context, []string) ([]Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) ListSchedulableByGroupIDAndPlatforms(context.Context, int64, []string) ([]Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) ListSchedulableUngroupedByPlatform(context.Context, string) ([]Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) ListSchedulableUngroupedByPlatforms(context.Context, []string) ([]Account, error) {
	return nil, nil
}
func (r *codingPlanAccountTestRepo) SetRateLimited(context.Context, int64, time.Time) error {
	return nil
}
func (r *codingPlanAccountTestRepo) SetModelRateLimit(context.Context, int64, string, time.Time, ...string) error {
	return nil
}
func (r *codingPlanAccountTestRepo) SetOverloaded(context.Context, int64, time.Time) error {
	return nil
}
func (r *codingPlanAccountTestRepo) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	return nil
}
func (r *codingPlanAccountTestRepo) ClearTempUnschedulable(context.Context, int64) error {
	return nil
}
func (r *codingPlanAccountTestRepo) ClearRateLimit(context.Context, int64) error { return nil }
func (r *codingPlanAccountTestRepo) ClearAntigravityQuotaScopes(context.Context, int64) error {
	return nil
}
func (r *codingPlanAccountTestRepo) ClearModelRateLimits(context.Context, int64) error {
	return nil
}
func (r *codingPlanAccountTestRepo) UpdateSessionWindow(context.Context, int64, *time.Time, *time.Time, string) error {
	return nil
}
func (r *codingPlanAccountTestRepo) UpdateSessionWindowEnd(context.Context, int64, time.Time) error {
	return nil
}
func (r *codingPlanAccountTestRepo) UpdateExtra(context.Context, int64, map[string]any) error {
	return nil
}
func (r *codingPlanAccountTestRepo) BulkUpdate(context.Context, []int64, AccountBulkUpdate) (int64, error) {
	return 0, nil
}
func (r *codingPlanAccountTestRepo) RevertProxyFallback(context.Context, int64) error {
	return nil
}
func (r *codingPlanAccountTestRepo) IncrementQuotaUsed(context.Context, int64, float64) error {
	return nil
}
func (r *codingPlanAccountTestRepo) ResetQuotaUsed(context.Context, int64) error { return nil }

type codingPlanHTTPUpstreamRecorder struct {
	req  *http.Request
	body []byte
	resp *http.Response
}

func (u *codingPlanHTTPUpstreamRecorder) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return nil, fmt.Errorf("unexpected Do call")
}

func (u *codingPlanHTTPUpstreamRecorder) DoWithTLS(req *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	u.req = req
	if req.Body != nil {
		body, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		u.body = body
	}
	if u.resp != nil {
		return u.resp, nil
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(`data: {"type":"message_stop"}` + "\n\n")),
	}, nil
}

func newCodingPlanTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/1/test", nil)
	req.Header.Set("User-Agent", "admin-test-client")
	c.Request = req
	return c, rec
}

func TestAccountTestServiceCodingPlanUsesBearerAuthAndOfficialEndpoint(t *testing.T) {
	tests := []struct {
		name      string
		account   *Account
		wantURL   string
		wantModel string
	}{
		{
			name: "minimax",
			account: &Account{
				ID:          11,
				Platform:    PlatformMiniMax,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-minimax"},
			},
			wantURL:   "https://api.minimax.io/anthropic/v1/messages",
			wantModel: "MiniMax-M2.7",
		},
		{
			name: "glm",
			account: &Account{
				ID:          12,
				Platform:    PlatformGLM,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-glm"},
			},
			wantURL:   "https://open.bigmodel.cn/api/anthropic/v1/messages",
			wantModel: "GLM-5.1",
		},
		{
			name: "kimi",
			account: &Account{
				ID:          13,
				Platform:    PlatformKimi,
				Type:        AccountTypeAPIKey,
				Concurrency: 1,
				Credentials: map[string]any{"api_key": "sk-kimi"},
			},
			wantURL:   "https://api.kimi.com/coding/v1/messages",
			wantModel: "kimi-for-coding",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c, rec := newCodingPlanTestContext()
			upstream := &codingPlanHTTPUpstreamRecorder{}
			svc := &AccountTestService{
				accountRepo:  &codingPlanAccountTestRepo{account: tc.account},
				httpUpstream: upstream,
			}

			err := svc.TestAccountConnection(c, tc.account.ID, "", "", AccountTestModeDefault)

			require.NoError(t, err)
			require.NotNil(t, upstream.req)
			require.Equal(t, tc.wantURL, upstream.req.URL.String())
			require.Equal(t, "Bearer "+tc.account.GetCredential("api_key"), upstream.req.Header.Get("Authorization"))
			require.Empty(t, upstream.req.Header.Get("x-api-key"))
			require.Equal(t, tc.wantModel, gjson.GetBytes(upstream.body, "model").String())
			require.Contains(t, rec.Body.String(), `"type":"test_complete"`)
		})
	}
}
