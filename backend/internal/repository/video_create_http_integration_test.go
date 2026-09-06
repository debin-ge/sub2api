//go:build integration

package repository

import (
	"context"

	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func videoIntentHTTPRouter(t *testing.T, tasks service.VideoTaskRepository, user *service.User, key *service.APIKey, producer gin.HandlerFunc, auth ...gin.HandlerFunc) *gin.Engine {
	t.Helper()
	settings := &config.Config{Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{Enabled: true, CreationEnabled: true, DisclosurePolicy: config.VideoDisclosureNone}}}
	settings.Totp.EncryptionKey = strings.Repeat("02", 32)
	encryptor, err := NewAESEncryptor(settings)
	require.NoError(t, err)
	taskService := service.NewVideoTaskService(tasks, &videoResourceRepository{db: integrationDB}, nil, nil, nil, nil, nil, nil, service.NewVideoProviderRegistry(), nil, nil, encryptor, nil, settings)
	videoHandler := handler.NewVideoHandler(taskService, nil, settings, nil, nil)
	group := attachVideoIntentGroup(t, key, service.PlatformComposite)
	key.User = user
	key.Group = group
	key.GroupID = &group.ID
	router := gin.New()
	router.Use(gin.Recovery())
	if len(auth) > 0 {
		router.Use(auth...)
	} else {
		router.Use(func(c *gin.Context) {
			c.Set(string(middleware.ContextKeyAPIKey), key)
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: user.ID})
			c.Next()
		})
	}
	router.Use(videoHandler.CreateIntentMiddleware)
	router.POST("/v1/videos", producer)
	router.POST("/videos", producer)
	router.POST("/v1/videos/generations", producer)
	router.POST("/videos/generations", producer)
	return router
}

func performVideoIntentHTTP(router *gin.Engine, path, key, body string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	return recorder
}

func TestVideoCreateHTTPSerializesInitialRoutingAndNativeReplay(t *testing.T) {
	ctx := context.Background()
	repo, _, _, user, key, account := newVideoRepositoryFixture(t, 100)
	var selected, nativeCalls, alternateCalls atomic.Int64
	entered, proceed := make(chan struct{}), make(chan struct{})
	body := `{"model":"video-alias","prompt":"test","seconds":8,"size":"1280x720"}`
	nativeHash, err := service.HashVideoRequest(map[string]any{"operation": "generate", "model": "video-alias", "prompt": "test", "seconds": 8, "size": "1280x720",
		"quality": "", "audio_enabled": nil, "service_tier": "", "request_mode": "", "inference_mode": "", "input_reference": nil, "inputs": []any{},
		"characters": nil, "source_video": "", "provider_options": nil, "callback_url": ""})
	require.NoError(t, err)
	router := videoIntentHTTPRouter(t, repo, user, key, func(c *gin.Context) {
		if selected.Load() == 0 {
			close(entered)
			<-proceed
			params := videoCreateParams(user, key, account, service.NewVideoTaskID(), "initial-route", nativeHash, 4)
			params.Owner.GroupID = key.GroupID
			params.RequestedModel = "video-alias"
			params.RequestAttributes["client_request_contract_version"] = 2
			task, created, err := repo.CreateHeldVideoTask(c.Request.Context(), params)
			if err != nil {
				c.JSON(500, gin.H{"error": "native hold failed"})
				return
			}
			if created {
				nativeCalls.Add(1)
			}
			_, err = repo.TransitionVideoTask(service.WithVideoTaskWriteGuard(c.Request.Context(), task.ID, task.Version), task.PublicID,
				service.VideoTaskTransition{GenerationState: service.VideoGenerationSubmitting})
			require.NoError(t, err)
			_, err = repo.SaveVideoProviderAccepted(videoRepositoryWriteContext(t, repo, ctx, task.PublicID), task.PublicID,
				service.VideoProviderAcceptance{ProviderTaskID: "native-result", GenerationState: service.VideoGenerationQueued})
			require.NoError(t, err)
			c.JSON(202, gin.H{"id": task.PublicID})
		} else {
			alternateCalls.Add(1)
			c.JSON(202, gin.H{"id": "alternate-result"})
		}
	})
	require.NotNil(t, key.GroupID)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO account_groups(account_id,group_id) VALUES($1,$2)`, account.ID, *key.GroupID)
	require.NoError(t, err)
	first := make(chan *httptest.ResponseRecorder, 1)
	go func() { first <- performVideoIntentHTTP(router, "/v1/videos", "initial-route", body) }()
	<-entered
	selected.Store(1)
	duplicate := performVideoIntentHTTP(router, "/videos", "initial-route", body)
	require.Equal(t, http.StatusConflict, duplicate.Code)
	require.Equal(t, "3", duplicate.Header().Get("Retry-After"))
	require.Zero(t, alternateCalls.Load())
	close(proceed)
	firstResponse := <-first
	require.Equal(t, 202, firstResponse.Code, firstResponse.Body.String())
	_, err = integrationDB.ExecContext(ctx, `UPDATE users SET balance=0 WHERE id=$1`, user.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE api_keys SET status='quota_exhausted' WHERE id=$1`, key.ID)
	require.NoError(t, err)
	user.Balance = 0
	key.Status = service.StatusAPIKeyQuotaExhausted
	replay := performVideoIntentHTTP(router, "/videos", "initial-route", body)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.Equal(t, "true", replay.Header().Get("X-Video-Idempotency-Replayed"))
	require.Equal(t, int64(1), nativeCalls.Load())
	require.Zero(t, alternateCalls.Load())
	assertVideoBudgetTotals(t, user.ID, 1, 0, 4)
}
