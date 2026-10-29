//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type grokNativeHTTP struct {
	service.HTTPUpstream
	created      atomic.Int64
	observed     atomic.Int64
	contents     atomic.Int64
	status       int
	taskID       string
	beforeCreate func()
}

func (h *grokNativeHTTP) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	code, body := 200, `{"request_id":"`+h.taskID+`"}`
	if req.Method == http.MethodPost {
		h.created.Add(1)
		if h.beforeCreate != nil {
			h.beforeCreate()
		}
		if h.status != 0 {
			code = h.status
		}
	} else {
		h.observed.Add(1)
		body = `{"status":"done","model":"grok-imagine-video-1.5","video":{"url":"https://vidgen.x.ai/test.mp4","duration":8,"respect_moderation":true},"usage":{"cost_in_usd_ticks":6900000000,"output_tokens":17,"input_tokens_details":{"image_tokens":12}}}`
	}
	return &http.Response{StatusCode: code, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
}

type grokNativeGroupRepo struct {
	service.GroupRepository
	group *service.Group
}

func (r grokNativeGroupRepo) GetByID(context.Context, int64) (*service.Group, error) {
	return r.group, nil
}
func nativeGrokFixture(t *testing.T) (*service.VideoTaskService, *service.VideoTaskWorker, *videoTaskRepository, *grokNativeHTTP, *service.User, service.VideoSubmitRequest, *config.Config) {
	t.Helper()
	ctx := context.Background()
	repo, resources, callbacks, user, key, account := newVideoRepositoryFixture(t, 10)
	group := attachVideoIntentGroup(t, key, service.PlatformGrok)
	group.AllowImageGeneration = true
	unit := service.VideoBillingUnitSecond
	price := 0.1
	group.ModelPricing = []service.ChannelModelPricing{{Platform: service.PlatformGrok, Models: []string{"grok-imagine-video-1.5"}, BillingMode: service.BillingModeVideo, Intervals: []service.PricingInterval{{ID: 1, BillingUnit: &unit, PerRequestPrice: &price}}}}
	_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET platform='grok',status='active',schedulable=true,concurrency=4,credentials=jsonb_build_object('api_key','test') WHERE id=$1`, account.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, account.ID, group.ID)
	require.NoError(t, err)
	cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{Enabled: true, CreationEnabled: true, GrokManagedEnabled: true, PollIntervalSeconds: 1, LeaseSeconds: 60, WorkerConcurrency: 4, ContentProxy: config.GatewayVideoContentProxyConfig{Enabled: true}}}}
	cfg.Totp.EncryptionKey = strings.Repeat("02", 32)
	encryptor, err := NewAESEncryptor(cfg)
	require.NoError(t, err)
	upstream := &grokNativeHTTP{taskID: uuid.NewString()}
	provider := service.NewGrokVideoProvider(upstream, nil, cfg)
	registry := service.NewVideoProviderRegistry(&grokNativeContentProvider{GrokVideoProvider: provider, http: upstream})
	accounts := NewAccountRepository(testEntClient(t), integrationDB, nil)
	quotas := NewUserPlatformQuotaServiceAdapter(NewUserPlatformQuotaRepository(testEntClient(t)))
	cache := service.NewBillingCacheService(NewBillingCache(integrationRedis), nil, nil, nil, nil, nil, cfg, quotas)
	t.Cleanup(cache.Stop)
	svc := service.NewVideoTaskService(repo, resources, nil, accounts, grokNativeGroupRepo{group: group}, nil, nil, nil, registry, service.NewVideoPricingResolver(nil, nil), repo.billing, encryptor, cache, cfg)
	effects := service.NewUsageBillingPostEffectsService(cache, nil, nil, accounts, quotas, nil, nil, cfg)
	worker := service.NewVideoTaskWorker(repo, nil, accounts, registry, svc, repo.billing, callbacks, effects, cfg)
	key.User = user
	key.Group = group
	request := service.VideoSubmitRequest{APIKey: key, Operation: service.VideoOperationGenerate, Model: "grok-imagine-video-1.5", Prompt: "test", Seconds: 8, Resolution: "480p", IdempotencyKey: "grok-native-" + uuid.NewString(), Grok: &service.GrokVideoOptions{ClientRequestHash: "test-hash"}}
	return svc, worker, repo, upstream, user, request, cfg
}
func TestGrokManagedFundsAndDeliveryIntegration(t *testing.T) {
	ctx := context.Background()
	svc, worker, repo, upstream, user, request, _ := nativeGrokFixture(t)
	upstream.beforeCreate = func() { assertVideoBudgetTotals(t, user.ID, 1, 9.2, 0.8) }
	result, err := svc.Submit(ctx, request)
	require.NoError(t, err)
	require.Equal(t, service.VideoBillingHeld, result.Task.BillingState)
	assertVideoBudgetTotals(t, user.ID, 1, 9.2, 0.8)
	replay, err := svc.Submit(ctx, request)
	require.NoError(t, err)
	require.Equal(t, result.Task.PublicID, replay.Task.PublicID)
	require.Equal(t, int64(1), upstream.created.Load())
	changedPrice := 100.0
	request.APIKey.Group.ModelPricing[0].Intervals[0].PerRequestPrice = &changedPrice
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET next_action_at=NOW()-INTERVAL '1 second' WHERE id=$1`, result.Task.ID)
	require.NoError(t, err)
	require.NoError(t, worker.ProcessBatch(ctx, 1))
	task, err := repo.GetVideoTaskForOwner(ctx, user.ID, result.Task.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.VideoGenerationCompleted, task.GenerationState)
	require.Equal(t, service.VideoBillingCapturePending, task.BillingState)
	status, err := svc.GetStatusForOwner(ctx, user.ID, task.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.VideoGenerationInProgress, service.ProjectVideoStatus(status))
	_, err = svc.OpenContentForTask(ctx, task, service.ProviderContentRequest{Method: "GET", Variant: "video"})
	require.ErrorIs(t, err, service.ErrVideoSettlementPending)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- worker.ProcessBatch(ctx, 1) }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	task, err = repo.GetVideoTaskForOwner(ctx, user.ID, task.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.VideoBillingCaptured, task.BillingState)
	require.Equal(t, service.VideoGenerationCompleted, service.ProjectVideoStatus(task))
	assertVideoBudgetTotals(t, user.ID, 1, 9.2, 0)
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE user_id=$1`, user.ID).Scan(&count))
	require.Equal(t, 1, count)
	require.Equal(t, int64(1), upstream.created.Load())
}
func TestGrokManagedRejectionRefundIntegration(t *testing.T) {
	svc, _, _, upstream, user, request, _ := nativeGrokFixture(t)
	upstream.status = 400
	_, err := svc.Submit(context.Background(), request)
	require.Error(t, err)
	assertVideoBudgetTotals(t, user.ID, 1, 10, 0)
	require.Equal(t, int64(1), upstream.created.Load())
}
func TestGrokManagedUnknownSubmissionDoesNotResubmitIntegration(t *testing.T) {
	ctx := context.Background()
	svc, worker, repo, upstream, user, request, _ := nativeGrokFixture(t)
	upstream.status = 502
	result, err := svc.Submit(ctx, request)
	require.NoError(t, err)
	require.Equal(t, service.VideoGenerationSubmitting, result.Task.GenerationState)
	assertVideoBudgetTotals(t, user.ID, 1, 9.2, 0.8)
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET next_action_at=NOW()-INTERVAL '1 second',submission_unknown_at=NOW()-INTERVAL '31 minutes' WHERE id=$1`, result.Task.ID)
	require.NoError(t, err)
	require.NoError(t, worker.ProcessBatch(ctx, 1))
	task, err := repo.GetVideoTaskForOwner(ctx, user.ID, result.Task.PublicID)
	require.NoError(t, err)
	require.Equal(t, service.VideoGenerationFailed, task.GenerationState)
	require.Equal(t, service.VideoBillingReleased, task.BillingState)
	assertVideoBudgetTotals(t, user.ID, 1, 10, 0)
	require.Equal(t, int64(1), upstream.created.Load())
	require.Zero(t, upstream.observed.Load())
}

// The provider transport is fake; the HTTP handler, SQL holds, worker and
// settlement outbox below are real. No supplier or public CDN is contacted.
type grokNativeContentProvider struct {
	*service.GrokVideoProvider
	http *grokNativeHTTP
}

func (p *grokNativeContentProvider) OpenContent(context.Context, *service.Account, service.ProviderContentRequest) (*service.ProviderContent, error) {
	p.http.contents.Add(1)
	return &service.ProviderContent{StatusCode: 200, Header: http.Header{"Content-Type": []string{"video/mp4"}}, Body: io.NopCloser(strings.NewReader("fixture-video"))}, nil
}
func TestGrokManagedHTTPReceiptAndDeliveryIntegration(t *testing.T) {
	ctx := context.Background()
	svc, worker, repo, upstream, user, request, cfg := nativeGrokFixture(t)
	cfg.JWT.Secret = strings.Repeat("test-secret-", 4)
	gateway := handler.NewOpenAIGatewayHandler(nil, nil, nil, nil, nil, nil, nil, nil, cfg)
	videos := handler.NewVideoHandler(svc, nil, cfg, gateway, nil)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(func(c *gin.Context) {
		key := *request.APIKey
		if c.GetHeader("X-Test-Other-Key") != "" {
			key.ID++
		}
		c.Set(string(middleware.ContextKeyAPIKey), &key)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: user.ID})
		c.Next()
	})
	router.POST("/v1/videos/generations", gateway.GrokVideoGeneration)
	router.GET("/v1/videos/:request_id", gateway.GrokVideoStatus)
	router.GET("/v1/videos/:request_id/content", gateway.GrokVideoContent)
	router.GET("/v1/videos/:request_id/content/signed", videos.SignedContent)
	create := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/videos/generations", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Idempotency-Key", "http-native-key")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	get := func(path string, other bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if other {
			req.Header.Set("X-Test-Other-Key", "true")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}
	body := `{"model":"grok-imagine-video-1.5","prompt":"test","duration":8,"resolution":"480p"}`
	created := create(body)
	require.Equal(t, 200, created.Code, created.Body.String())
	var acceptance map[string]string
	require.NoError(t, json.Unmarshal(created.Body.Bytes(), &acceptance))
	publicID := acceptance["request_id"]
	id, err := uuid.Parse(publicID)
	require.NoError(t, err)
	require.NotEqual(t, upstream.taskID, publicID)
	localID := "video_" + strings.ReplaceAll(id.String(), "-", "")
	task, err := repo.GetVideoTaskForOwner(ctx, user.ID, localID)
	require.NoError(t, err)
	require.Equal(t, service.VideoBillingHeld, task.BillingState)
	pending := get("/v1/videos/"+publicID, false)
	require.Equal(t, 200, pending.Code)
	require.Contains(t, pending.Body.String(), `"pending"`)
	require.NotContains(t, pending.Body.String(), `"url"`)
	require.Equal(t, 404, get("/v1/videos/"+publicID, true).Code)
	cfg.Gateway.Video.CreationEnabled = false
	replay := create(body)
	require.Equal(t, 200, replay.Code, replay.Body.String())
	require.JSONEq(t, created.Body.String(), replay.Body.String())
	require.Equal(t, int64(1), upstream.created.Load())
	_, err = integrationDB.ExecContext(ctx, `UPDATE video_tasks SET next_action_at=NOW()-INTERVAL '1 second' WHERE id=$1`, task.ID)
	require.NoError(t, err)
	require.NoError(t, worker.ProcessBatch(ctx, 1))
	require.NotContains(t, get("/v1/videos/"+publicID, false).Body.String(), `"url"`)
	require.Equal(t, 503, get("/v1/videos/"+publicID+"/content", false).Code)
	require.Zero(t, upstream.contents.Load())
	require.NoError(t, worker.ProcessBatch(ctx, 1))
	done := get("/v1/videos/"+publicID, false)
	require.Equal(t, 200, done.Code, done.Body.String())
	var result struct {
		Status string `json:"status"`
		Video  struct {
			URL string `json:"url"`
		} `json:"video"`
	}
	require.NoError(t, json.Unmarshal(done.Body.Bytes(), &result))
	require.Equal(t, "done", result.Status)
	require.Contains(t, done.Body.String(), `"respect_moderation":true`)
	require.Contains(t, done.Body.String(), `"cost_in_usd_ticks":6900000000`)
	require.Contains(t, done.Body.String(), `"image_tokens":12`)
	require.NotContains(t, done.Body.String(), `"video_tokens"`)
	require.Equal(t, 404, get("/v1/videos/"+publicID+"/content", true).Code)
	require.Contains(t, result.Video.URL, "sig=")
	for range 2 {
		download := get(result.Video.URL, false)
		require.Equal(t, 200, download.Code, download.Body.String())
		require.Equal(t, "fixture-video", download.Body.String())
	}
	assertVideoBudgetTotals(t, user.ID, 1, 9.2, 0)
	var usage int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM usage_logs WHERE user_id=$1`, user.ID).Scan(&usage))
	require.Equal(t, 1, usage)
}
func TestGrokManagedInsufficientBalanceDoesNotSubmitIntegration(t *testing.T) {
	svc, _, _, upstream, user, request, _ := nativeGrokFixture(t)
	_, err := integrationDB.Exec(`UPDATE users SET balance=0 WHERE id=$1`, user.ID)
	require.NoError(t, err)
	_, err = svc.Submit(context.Background(), request)
	require.Error(t, err)
	require.Zero(t, upstream.created.Load())
	assertVideoBudgetTotals(t, user.ID, 0, 0, 0)
}

// Authorization is applied in SQL before LIMIT, including cursor validation.
func TestGrokManagedListScopeBeforePagination(t *testing.T) {
	ctx := context.Background()
	svc, _, repo, upstream, user, request, _ := nativeGrokFixture(t)
	first, err := svc.Submit(ctx, request)
	require.NoError(t, err)
	secondKey := mustCreateApiKey(t, testEntClient(t), &service.APIKey{UserID: user.ID, GroupID: request.APIKey.GroupID, Key: "test-grok-list-" + uuid.NewString()})
	secondKey.User, secondKey.Group = user, request.APIKey.Group
	secondRequest := request
	secondRequest.APIKey = secondKey
	secondRequest.IdempotencyKey += "-second"
	upstream.taskID = uuid.NewString()
	second, err := svc.Submit(ctx, secondRequest)
	require.NoError(t, err)
	// The newest task belongs to another key of the same user and must be
	// filtered before LIMIT, rather than consuming this key's first page.
	filter := service.VideoTaskFilter{Limit: 1, GrokAPIKeyID: &request.APIKey.ID, GrokGroupID: request.APIKey.GroupID, Provider: service.VideoProviderGrok}
	page, err := repo.ListVideoTasksForOwner(ctx, user.ID, filter)
	require.NoError(t, err)
	require.Len(t, page.Data, 1)
	require.Equal(t, first.Task.PublicID, page.Data[0].PublicID)
	filter.After = second.Task.PublicID
	_, err = repo.ListVideoTasksForOwner(ctx, user.ID, filter)
	require.ErrorIs(t, err, service.ErrVideoTaskNotFound)
	filter.After = ""
	otherKey := request.APIKey.ID + 99999
	filter.GrokAPIKeyID = &otherKey
	page, err = repo.ListVideoTasksForOwner(ctx, user.ID, filter)
	require.NoError(t, err)
	require.Empty(t, page.Data)
	filter.GrokAPIKeyID = &request.APIKey.ID
	otherGroup := *request.APIKey.GroupID + 99999
	filter.GrokGroupID = &otherGroup
	page, err = repo.ListVideoTasksForOwner(ctx, user.ID, filter)
	require.NoError(t, err)
	require.Empty(t, page.Data)
	filter.GrokGroupID = request.APIKey.GroupID
	page, err = repo.ListVideoTasksForOwner(ctx, user.ID+99999, filter)
	require.NoError(t, err)
	require.Empty(t, page.Data)
}
