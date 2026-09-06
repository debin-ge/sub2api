package routes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type compositeReplayAPIKeyRepo struct{ keyBillingRouteAPIKeyRepo }

func (*compositeReplayAPIKeyRepo) UpdateLastUsed(context.Context, int64, time.Time) error { return nil }

type compositeReplayTaskRepo struct {
	service.VideoTaskRepository
	task    *service.VideoTask
	creates int
}

func (repo *compositeReplayTaskRepo) GetVideoTaskByIdempotency(_ context.Context, userID int64, endpoint, key string) (*service.VideoTask, error) {
	if repo.task.UserID == userID && endpoint == service.CompositeRouteEndpointVideos && key == "original-intent" {
		return repo.task, nil
	}
	return nil, service.ErrVideoTaskNotFound
}

func (repo *compositeReplayTaskRepo) GetVideoTaskForOwner(_ context.Context, userID int64, publicID string) (*service.VideoTask, error) {
	if repo.task.UserID == userID && repo.task.PublicID == publicID {
		return repo.task, nil
	}
	return nil, service.ErrVideoTaskNotFound
}

func (repo *compositeReplayTaskRepo) CreateHeldVideoTask(context.Context, service.VideoCreateTaskParams) (*service.VideoTask, bool, error) {
	repo.creates++
	return nil, false, service.ErrBillingServiceUnavailable
}

func TestCompositeVideoHTTPReplayPrecedesRouteChangesAndExhaustedBudget(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, prefix := range []string{"", "/v1"} {
		t.Run(prefix, func(t *testing.T) {
			group := &service.Group{ID: 42, Platform: service.PlatformComposite, Status: service.StatusActive, Hydrated: true}
			user := &service.User{ID: 7, Role: service.RoleUser, Status: service.StatusActive, Balance: 0}
			key := &service.APIKey{ID: 9, UserID: user.ID, Key: "composite-replay-test-key", User: user, Group: group, GroupID: &group.ID,
				Status: service.StatusAPIKeyQuotaExhausted, Quota: 4, QuotaUsed: 4}
			cfg := &config.Config{RunMode: config.RunModeStandard, Gateway: config.GatewayConfig{Video: config.GatewayVideoConfig{Enabled: true, CreationEnabled: false, DisclosurePolicy: config.VideoDisclosureNone}}}
			hash, err := service.HashVideoRequest(map[string]any{
				"operation": "generate", "model": "video-alias", "prompt": "test", "seconds": 8, "size": "1280x720",
				"quality": "", "audio_enabled": nil, "service_tier": "", "request_mode": "", "inference_mode": "",
				"input_reference": nil, "inputs": []any{}, "characters": nil, "source_video": "", "provider_options": nil, "callback_url": "",
			})
			require.NoError(t, err)
			taskRepo := &compositeReplayTaskRepo{task: &service.VideoTask{ID: 1, PublicID: service.NewVideoTaskID(), Source: "managed", UserID: user.ID,
				Operation: service.VideoOperationGenerate, Provider: service.VideoProviderOpenAI, RequestHash: hash,
				RequestedModel: "video-alias", PublicModel: service.OpenAIVideoModelSora2, UpstreamModel: service.OpenAIVideoModelSora2,
				GenerationState: service.VideoGenerationQueued, BillingState: service.VideoBillingHeld,
				RequestAttributes: map[string]any{"client_request_contract_version": 2}, CreatedAt: time.Now()}}
			resolver := service.NewCompositeRouteResolver(compositeRouteRepoStub{routes: []service.CompositeModelRoute{
				{GroupID: group.ID, PublicModel: "video-alias", UpstreamModel: "grok-imagine-video", TargetPlatform: service.PlatformGrok, MatchType: service.CompositeRouteMatchExact, Endpoint: service.CompositeRouteEndpointVideos, Enabled: true},
			}})
			taskService := service.NewVideoTaskService(taskRepo, nil, nil, nil, nil, nil, nil, resolver, service.NewVideoProviderRegistry(), nil, nil, nil, nil, cfg)
			videoHandler := handler.NewVideoHandler(taskService, nil, cfg, nil, nil)
			keyRepo := &compositeReplayAPIKeyRepo{keyBillingRouteAPIKeyRepo{apiKey: key}}
			keyService := service.NewAPIKeyService(keyRepo, nil, nil, nil, nil, nil, cfg)
			router := gin.New()
			router.Use(gin.HandlerFunc(middleware.NewAPIKeyAuthMiddleware(keyService, nil, cfg)), compositeTargetPlatformMiddleware(resolver, videoHandler))
			router.POST(prefix+"/videos", func(ctx *gin.Context) {
				if getGroupPlatform(ctx) == service.PlatformGrok {
					(&handler.OpenAIGatewayHandler{}).GrokVideoGeneration(ctx)
				} else {
					videoHandler.Create(ctx)
				}
			})
			for _, test := range []struct {
				key, prompt string
				status      int
			}{
				{"original-intent", "test", http.StatusOK},
				{"original-intent", "changed", http.StatusConflict},
				{"new-intent", "test", http.StatusTooManyRequests},
			} {
				request := httptest.NewRequest(http.MethodPost, prefix+"/videos", strings.NewReader(`{"model":"video-alias","prompt":"`+test.prompt+`","seconds":8,"size":"1280x720"}`))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("x-api-key", key.Key)
				request.Header.Set("Idempotency-Key", test.key)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, request)
				require.Equal(t, test.status, recorder.Code, recorder.Body.String())
				if test.status == http.StatusOK {
					require.Contains(t, recorder.Body.String(), taskRepo.task.PublicID)
				}
			}
			require.Zero(t, taskRepo.creates)
		})
	}
}
