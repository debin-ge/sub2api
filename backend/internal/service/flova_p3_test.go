package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// flovaAccountRepoStub 复用 codingPlanAccountTestRepo 的空实现，只记录本轮关心的调用。
type flovaAccountRepoStub struct {
	*codingPlanAccountTestRepo
	accounts    []Account
	extra       map[string]any
	updated     *Account
	errorID     int64
	errorMsg    string
	pausedID    int64
	pausedUntil time.Time
	pausedWhy   string
	clearedID   int64
}

func newFlovaAccountRepoStub(account *Account) *flovaAccountRepoStub {
	return &flovaAccountRepoStub{codingPlanAccountTestRepo: &codingPlanAccountTestRepo{account: account}, extra: map[string]any{}}
}

func (r *flovaAccountRepoStub) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *flovaAccountRepoStub) Update(_ context.Context, account *Account) error {
	r.updated = account
	return nil
}
func (r *flovaAccountRepoStub) ListByPlatform(context.Context, string) ([]Account, error) {
	return r.accounts, nil
}
func (r *flovaAccountRepoStub) SetError(_ context.Context, id int64, msg string) error {
	r.errorID, r.errorMsg = id, msg
	return nil
}
func (r *flovaAccountRepoStub) SetTempUnschedulable(_ context.Context, id int64, until time.Time, reason string) error {
	r.pausedID, r.pausedUntil, r.pausedWhy = id, until, reason
	return nil
}
func (r *flovaAccountRepoStub) ClearTempUnschedulable(_ context.Context, id int64) error {
	r.clearedID = id
	return nil
}
func (r *flovaAccountRepoStub) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	for k, v := range updates {
		r.extra[k] = v
	}
	return nil
}

func flovaSchedulableAccount() *Account {
	account := flovaVideoTestAccount()
	account.Status = StatusActive
	account.Schedulable = true
	return account
}

func TestFlovaCreditsSyncServicePausesAndClears(t *testing.T) {
	account := flovaSchedulableAccount()
	repo := newFlovaAccountRepoStub(account)
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", `{"code":0,"data":{"credits_balance":"40.000000","subscription_info":{"lookup_key":"starter_month"}}}`)
	svc := NewFlovaCreditsSyncService(repo, newFlovaTestProvider(stub), 0)
	svc.now = func() time.Time { return time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC) }

	user, err := svc.SyncAccount(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, 40.0, user.CreditsBalance)
	require.Equal(t, 40.0, repo.extra[FlovaExtraCredits])
	require.Equal(t, "2026-10-07T12:00:00Z", repo.extra[FlovaExtraCreditsCheckedAt])
	require.Equal(t, account.ID, repo.pausedID, "below the 60-credit reserve pauses the account")
	require.True(t, strings.HasPrefix(repo.pausedWhy, flovaCreditsLowReasonPrefix))
	require.Equal(t, svc.now().Add(flovaCreditsPauseCooldown), repo.pausedUntil)

	// 余额恢复且停调原因是本服务写的 → 清除。
	stub.json("/api/agent/v2/user", flovaUserOK)
	until := time.Now().Add(time.Hour)
	account.TempUnschedulableUntil = &until
	account.TempUnschedulableReason = repo.pausedWhy
	_, err = svc.SyncAccount(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, account.ID, repo.clearedID)

	// 别的子系统写的停调不碰。
	repo.clearedID = 0
	account.TempUnschedulableReason = "manual"
	_, err = svc.SyncAccount(context.Background(), account)
	require.NoError(t, err)
	require.Zero(t, repo.clearedID)

	// 401 → 账号置 error。
	stub.on("/api/agent/v2/user", func(int, *http.Request, string) *http.Response {
		return openAIVideoResponseForTest(http.StatusUnauthorized, `{"code":"401","message":"Invalid or expired API token"}`, nil)
	})
	_, err = svc.SyncAccount(context.Background(), account)
	require.Error(t, err)
	require.Equal(t, account.ID, repo.errorID)
	require.Contains(t, repo.errorMsg, "Flova rejected the API key")
}

func TestFlovaCreditsSyncServiceRunOnceSkipsInactiveAndByID(t *testing.T) {
	active := flovaSchedulableAccount()
	inactive := flovaSchedulableAccount()
	inactive.ID = 32
	inactive.Status = "disabled"
	repo := newFlovaAccountRepoStub(active)
	repo.accounts = []Account{*active, *inactive}
	stub := newFlovaStubServer()
	stub.json("/api/agent/v2/user", flovaUserOK)
	svc := NewFlovaCreditsSyncService(repo, newFlovaTestProvider(stub), 0)
	svc.runOnce()
	require.Equal(t, 1, stub.count("/api/agent/v2/user"), "inactive accounts are skipped")

	user, err := svc.SyncAccountByID(context.Background(), active.ID)
	require.NoError(t, err)
	require.Equal(t, 2009.0, user.CreditsBalance)

	svc.Start() // interval 0 → 不启动，Stop 也不能阻塞
	svc.Stop()
}

func TestVideoTaskServiceDisableAccountOnAuthFailure(t *testing.T) {
	repo := newFlovaAccountRepoStub(flovaSchedulableAccount())
	svc := &VideoTaskService{accounts: repo}
	accountID := int64(31)
	task := &VideoTask{Provider: VideoProviderFlova, AccountID: &accountID}

	svc.disableAccountOnAuthFailure(context.Background(), task, &VideoProviderError{Kind: "rate_limit", Code: "rate_limited"})
	require.Zero(t, repo.errorID, "only authentication failures disable the account")

	svc.disableAccountOnAuthFailure(context.Background(), task, &VideoProviderError{Kind: "authentication", Code: "unauthorized", Message: "Invalid or expired API token"})
	require.Equal(t, accountID, repo.errorID)
	require.Equal(t, "video provider flova rejected credentials: Invalid or expired API token", repo.errorMsg)

	// 任何视频 provider 都适用（D13）。
	repo.errorID = 0
	task.Provider = VideoProviderByteDance
	svc.disableAccountOnAuthFailure(context.Background(), task, &VideoProviderError{Kind: "authentication", Code: "unauthorized"})
	require.Equal(t, accountID, repo.errorID)
	svc.disableAccountOnAuthFailure(context.Background(), task, errors.New("plain"))
}

func TestVideoTaskWorkerTrackFlovaObservation(t *testing.T) {
	account := flovaSchedulableAccount()
	account.Extra = map[string]any{FlovaExtraEmptyStreak: 2.0}
	repo := newFlovaAccountRepoStub(account)
	worker := &VideoTaskWorker{accounts: repo}
	task := &VideoTask{Provider: VideoProviderFlova}

	worker.trackFlovaObservation(context.Background(), account, task, &ProviderVideoTask{Status: VideoGenerationFailed, ErrorCode: "membership_required"})
	require.Zero(t, repo.pausedID, "non-empty failures do not count")
	require.Empty(t, repo.extra)

	worker.trackFlovaObservation(context.Background(), account, task, &ProviderVideoTask{Status: VideoGenerationFailed, ErrorCode: flovaErrorNoVideoArtifact})
	require.Equal(t, account.ID, repo.pausedID, "third empty completion pauses the account")
	require.Equal(t, flovaEmptyStreakPauseReason, repo.pausedWhy)
	require.Equal(t, 0, repo.extra[FlovaExtraEmptyStreak], "streak resets after the pause")

	account.Extra[FlovaExtraEmptyStreak] = 1.0
	repo.pausedID = 0
	worker.trackFlovaObservation(context.Background(), account, task, &ProviderVideoTask{Status: VideoGenerationFailed, ErrorCode: flovaErrorNoVideoArtifact})
	require.Zero(t, repo.pausedID)
	require.Equal(t, 2, repo.extra[FlovaExtraEmptyStreak])

	worker.trackFlovaObservation(context.Background(), account, task, &ProviderVideoTask{Status: VideoGenerationCompleted})
	require.Equal(t, 0, repo.extra[FlovaExtraEmptyStreak])

	repo.extra = map[string]any{}
	worker.trackFlovaObservation(context.Background(), account, &VideoTask{Provider: VideoProviderByteDance}, &ProviderVideoTask{Status: VideoGenerationFailed, ErrorCode: flovaErrorNoVideoArtifact})
	require.Empty(t, repo.extra, "other providers are ignored")
}

func flovaTestConnectionContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	return c, recorder
}

func TestFlovaAccountTestConnection(t *testing.T) {
	t.Run("does not create a skill when none is configured", func(t *testing.T) {
		account := flovaSchedulableAccount()
		delete(account.Credentials, "skill_id")
		repo := newFlovaAccountRepoStub(account)
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/user", flovaUserOK)
		svc := &AccountTestService{accountRepo: repo, httpUpstream: stub.upstream(), cfg: &config.Config{}}
		c, recorder := flovaTestConnectionContext()
		require.NoError(t, svc.testFlovaAccountConnection(c, account))
		body := recorder.Body.String()
		require.Contains(t, body, "Credentials accepted. credits_balance=2009")
		require.Contains(t, body, "Subscription: starter_month")
		require.Contains(t, body, "No skill_id configured (optional)")
		require.Contains(t, body, `"type":"test_complete"`)
		require.Nil(t, repo.updated, "credentials are never rewritten")
		require.Equal(t, 2009.0, repo.extra[FlovaExtraCredits])
		require.Equal(t, []string{"/api/agent/v2/user"}, stub.calls)
	})

	t.Run("verifies an existing skill and warns about free tier", func(t *testing.T) {
		account := flovaSchedulableAccount()
		repo := newFlovaAccountRepoStub(account)
		stub := newFlovaStubServer()
		stub.json("/api/agent/v2/user", `{"code":0,"data":{"credits_balance":"9.000000","subscription_info":{}}}`)
		stub.json("/api/agent/v2/skill/get", `{"code":0,"data":{"skill_id":"sk_test"}}`)
		svc := &AccountTestService{accountRepo: repo, httpUpstream: stub.upstream(), cfg: &config.Config{}}
		c, recorder := flovaTestConnectionContext()
		require.NoError(t, svc.testFlovaAccountConnection(c, account))
		body := recorder.Body.String()
		require.Contains(t, body, "free-tier")
		require.Contains(t, body, "Configured skill verified: sk_test")
		require.Equal(t, "sk_test", stub.lastBody("/api/agent/v2/skill/get")["skill_id"])
		require.Nil(t, repo.updated)
	})

	t.Run("401 marks the account as error", func(t *testing.T) {
		account := flovaSchedulableAccount()
		repo := newFlovaAccountRepoStub(account)
		stub := newFlovaStubServer()
		stub.on("/api/agent/v2/user", func(int, *http.Request, string) *http.Response {
			return openAIVideoResponseForTest(http.StatusUnauthorized, `{"code":"401","message":"Invalid or expired API token"}`, nil)
		})
		svc := &AccountTestService{accountRepo: repo, httpUpstream: stub.upstream(), cfg: &config.Config{}}
		c, recorder := flovaTestConnectionContext()
		require.Error(t, svc.testFlovaAccountConnection(c, account))
		require.Equal(t, account.ID, repo.errorID)
		require.Contains(t, recorder.Body.String(), `"type":"error"`)
	})
}
