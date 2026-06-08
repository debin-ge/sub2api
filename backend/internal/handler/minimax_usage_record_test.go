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

type miniMaxUsageLogRepoStub struct {
	service.UsageLogRepository

	lastLog *service.UsageLog
}

func (s *miniMaxUsageLogRepoStub) Create(ctx context.Context, log *service.UsageLog) (bool, error) {
	s.lastLog = log
	return true, nil
}

type miniMaxUsageUserRepoStub struct {
	service.UserRepository
}

func (s *miniMaxUsageUserRepoStub) DeductBalance(ctx context.Context, id int64, amount float64) error {
	return nil
}

type miniMaxUsageSubRepoStub struct {
	service.UserSubscriptionRepository
}

func (s *miniMaxUsageSubRepoStub) IncrementUsage(ctx context.Context, id int64, costUSD float64) error {
	return nil
}

type miniMaxUsageGatewayService struct {
	selection *service.AccountSelectionResult
	recorder  *service.GatewayService
}

func (s *miniMaxUsageGatewayService) GenerateSessionHash(parsed *service.ParsedRequest) string {
	return "minimax-usage-session"
}

func (s *miniMaxUsageGatewayService) SelectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, metadataUserID string, sub2apiUserID int64) (*service.AccountSelectionResult, error) {
	return s.selection, nil
}

func (s *miniMaxUsageGatewayService) RecordUsage(ctx context.Context, input *service.RecordUsageInput) error {
	return s.recorder.RecordUsage(ctx, input)
}

type miniMaxUsageForwarder struct {
	result *service.ForwardResult
}

func (f *miniMaxUsageForwarder) ForwardMessages(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	c.JSON(http.StatusOK, gin.H{"id": "msg_1"})
	return f.result, nil
}

func (f *miniMaxUsageForwarder) ForwardChatCompletions(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	return f.ForwardMessages(ctx, c, account, body, requestID)
}

func TestMiniMaxUsageRecordMetadata(t *testing.T) {
	usageRepo := &miniMaxUsageLogRepoStub{}
	cfg := &config.Config{}
	cfg.Default.RateMultiplier = 1
	recorder := service.NewGatewayService(
		nil,
		nil,
		usageRepo,
		nil,
		&miniMaxUsageUserRepoStub{},
		&miniMaxUsageSubRepoStub{},
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
		nil,
	)
	groupID := int64(42)
	subscriptionID := int64(88)
	minimaxGroup := &service.Group{
		ID:               groupID,
		Platform:         service.PlatformMiniMax,
		RateMultiplier:   0,
		SubscriptionType: service.SubscriptionTypeSubscription,
	}
	minimaxAccount := &service.Account{
		ID:          101,
		Platform:    service.PlatformMiniMax,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-minimax"},
	}
	h := &MiniMaxGatewayHandler{
		minimaxService: &miniMaxUsageForwarder{
			result: &service.ForwardResult{
				RequestID:     "minimax-usage-record",
				Model:         "claude-sonnet-4-5",
				UpstreamModel: "MiniMax-M2.7",
				Usage: service.ClaudeUsage{
					InputTokens:  11,
					OutputTokens: 7,
				},
				Duration: time.Second,
			},
		},
		gatewayService: &miniMaxUsageGatewayService{
			selection: &service.AccountSelectionResult{
				Account:  minimaxAccount,
				Acquired: true,
			},
			recorder: recorder,
		},
		concurrencyHelper:   &fakeMiniMaxConcurrencyController{allowWait: true},
		billingCacheService: &fakeMiniMaxBillingChecker{},
	}
	c, rec, apiKey := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)
	apiKey.GroupID = &groupID
	apiKey.Group = minimaxGroup
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
	require.Equal(t, service.PlatformMiniMax, minimaxAccount.Platform)
	require.Equal(t, minimaxAccount.ID, usageRepo.lastLog.AccountID)
	require.Equal(t, apiKey.ID, usageRepo.lastLog.APIKeyID)
	require.NotNil(t, usageRepo.lastLog.GroupID)
	require.Equal(t, minimaxGroup.ID, *usageRepo.lastLog.GroupID)
	require.NotNil(t, usageRepo.lastLog.SubscriptionID)
	require.Equal(t, subscriptionID, *usageRepo.lastLog.SubscriptionID)
	require.NotNil(t, usageRepo.lastLog.InboundEndpoint)
	require.Equal(t, "/v1/messages", *usageRepo.lastLog.InboundEndpoint)
	require.NotNil(t, usageRepo.lastLog.UpstreamEndpoint)
	require.Equal(t, "/v1/messages", *usageRepo.lastLog.UpstreamEndpoint)
	require.Equal(t, "claude-sonnet-4-5", usageRepo.lastLog.RequestedModel)
	require.Equal(t, "MiniMax-M2.7", usageRepo.lastLog.Model)
}
