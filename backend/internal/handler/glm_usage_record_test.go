package handler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type glmUsageLogRepoStub struct {
	service.UsageLogRepository

	lastLog *service.UsageLog
}

func (s *glmUsageLogRepoStub) Create(ctx context.Context, log *service.UsageLog) (bool, error) {
	s.lastLog = log
	return true, nil
}

type glmUsageUserRepoStub struct {
	service.UserRepository
}

func (s *glmUsageUserRepoStub) DeductBalance(ctx context.Context, id int64, amount float64) error {
	return nil
}

type glmUsageSubRepoStub struct {
	service.UserSubscriptionRepository
}

func (s *glmUsageSubRepoStub) IncrementUsage(ctx context.Context, id int64, costUSD float64) error {
	return nil
}

type glmUsageGatewayService struct {
	selection *service.AccountSelectionResult
	recorder  *service.GatewayService
}

func (s *glmUsageGatewayService) GenerateSessionHash(parsed *service.ParsedRequest) string {
	return "glm-usage-session"
}

func (s *glmUsageGatewayService) SelectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, metadataUserID string, sub2apiUserID int64) (*service.AccountSelectionResult, error) {
	return s.selection, nil
}

func (s *glmUsageGatewayService) RecordUsage(ctx context.Context, input *service.RecordUsageInput) error {
	return s.recorder.RecordUsage(ctx, input)
}

type glmUsageForwarder struct {
	result *service.ForwardResult
}

func (f *glmUsageForwarder) ForwardMessages(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	c.JSON(http.StatusOK, gin.H{"id": "msg_1"})
	return f.result, nil
}

func (f *glmUsageForwarder) ForwardChatCompletions(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	return f.ForwardMessages(ctx, c, account, body, requestID)
}

func TestGLMUsageRecordMetadata(t *testing.T) {
	usageRepo := &glmUsageLogRepoStub{}
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	recorder := service.NewGatewayService(
		nil,
		nil,
		usageRepo,
		nil,
		&glmUsageUserRepoStub{},
		&glmUsageSubRepoStub{},
		nil,
		nil,
		cfg,
		nil,
		nil,
		service.NewBillingService(cfg, nil),
		nil,
		&service.BillingCacheService{},
		nil,
		nil,
		&service.DeferredService{},
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	groupID := int64(42)
	subscriptionID := int64(88)
	glmGroup := &service.Group{
		ID:               groupID,
		Platform:         service.PlatformGLM,
		RateMultiplier:   0,
		SubscriptionType: service.SubscriptionTypeSubscription,
	}
	glmAccount := &service.Account{
		ID:          101,
		Platform:    service.PlatformGLM,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-glm"},
	}
	h := &GLMGatewayHandler{
		glmService: &glmUsageForwarder{
			result: &service.ForwardResult{
				RequestID:     "glm-usage-record",
				Model:         "claude-sonnet-4-5",
				UpstreamModel: "GLM-5.1",
				Usage: service.ClaudeUsage{
					InputTokens:  11,
					OutputTokens: 7,
				},
				Duration: time.Second,
			},
		},
		gatewayService: &glmUsageGatewayService{
			selection: &service.AccountSelectionResult{
				Account:  glmAccount,
				Acquired: true,
			},
			recorder: recorder,
		},
		concurrencyHelper:   &fakeGLMConcurrencyController{allowWait: true},
		billingCacheService: &fakeGLMBillingChecker{},
	}
	c, rec, apiKey := newGLMHandlerTestContext(t, service.PlatformGLM, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)
	apiKey.GroupID = &groupID
	apiKey.Group = glmGroup
	apiKey.User = &service.User{ID: 99, Status: service.StatusActive}
	c.Set(string(middleware.ContextKeySubscription), &service.UserSubscription{
		ID:      subscriptionID,
		UserID:  apiKey.User.ID,
		GroupID: groupID,
		Status:  service.SubscriptionStatusActive,
	})

	h.Messages(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, service.PlatformGLM, glmAccount.Platform)
	require.Equal(t, glmAccount.ID, usageRepo.lastLog.AccountID)
	require.Equal(t, apiKey.ID, usageRepo.lastLog.APIKeyID)
	require.NotNil(t, usageRepo.lastLog.GroupID)
	require.Equal(t, glmGroup.ID, *usageRepo.lastLog.GroupID)
	require.NotNil(t, usageRepo.lastLog.SubscriptionID)
	require.Equal(t, subscriptionID, *usageRepo.lastLog.SubscriptionID)
	require.NotNil(t, usageRepo.lastLog.InboundEndpoint)
	require.Equal(t, "/v1/messages", *usageRepo.lastLog.InboundEndpoint)
	require.NotNil(t, usageRepo.lastLog.UpstreamEndpoint)
	require.Equal(t, "/v1/messages", *usageRepo.lastLog.UpstreamEndpoint)
	require.Equal(t, "claude-sonnet-4-5", usageRepo.lastLog.RequestedModel)
	require.Equal(t, "claude-sonnet-4-5", usageRepo.lastLog.Model)
	require.NotNil(t, usageRepo.lastLog.UpstreamModel)
	require.Equal(t, "GLM-5.1", *usageRepo.lastLog.UpstreamModel)
}
