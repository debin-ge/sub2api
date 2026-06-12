package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fakeMiniMaxForwarder struct {
	account         *service.Account
	body            []byte
	requestID       string
	messagesCalled  bool
	chatCalled      bool
	responsesCalled bool
	err             error
}

func (f *fakeMiniMaxForwarder) ForwardMessages(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	f.messagesCalled = true
	f.account = account
	f.body = append([]byte(nil), body...)
	f.requestID = requestID
	if f.err != nil {
		return nil, f.err
	}
	c.JSON(http.StatusOK, gin.H{"id": "msg_1", "model": "claude-sonnet-4-5"})
	return &service.ForwardResult{
		RequestID: "upstream-req-1",
		Model:     "claude-sonnet-4-5",
		Usage: service.ClaudeUsage{
			InputTokens:  11,
			OutputTokens: 7,
		},
		Duration: time.Millisecond,
	}, nil
}

func (f *fakeMiniMaxForwarder) ForwardChatCompletions(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	f.chatCalled = true
	f.account = account
	f.body = append([]byte(nil), body...)
	f.requestID = requestID
	if f.err != nil {
		return nil, f.err
	}
	c.JSON(http.StatusOK, gin.H{"id": "chatcmpl_1", "model": "claude-sonnet-4-5"})
	return &service.ForwardResult{
		RequestID: "upstream-chat-req-1",
		Model:     "claude-sonnet-4-5",
		Usage: service.ClaudeUsage{
			InputTokens:  13,
			OutputTokens: 5,
		},
		Duration: time.Millisecond,
	}, nil
}

func (f *fakeMiniMaxForwarder) ForwardResponses(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	f.responsesCalled = true
	f.account = account
	f.body = append([]byte(nil), body...)
	f.requestID = requestID
	if f.err != nil {
		return nil, f.err
	}
	c.JSON(http.StatusOK, gin.H{"id": "resp_1", "object": "response", "model": "claude-sonnet-4-5"})
	effort := "medium"
	return &service.ForwardResult{
		RequestID:       "upstream-resp-req-1",
		Model:           "claude-sonnet-4-5",
		UpstreamModel:   "MiniMax-M2.7",
		ReasoningEffort: &effort,
		Usage: service.ClaudeUsage{
			InputTokens:  9,
			OutputTokens: 4,
		},
		Duration: time.Millisecond,
	}, nil
}

type fakeMiniMaxGatewayService struct {
	selection      *service.AccountSelectionResult
	selectErr      error
	recorded       *service.RecordUsageInput
	sessionHash    string
	selectedModel  string
	selectedGroup  *int64
	selectedUserID int64
}

func (f *fakeMiniMaxGatewayService) GenerateSessionHash(parsed *service.ParsedRequest) string {
	if f.sessionHash != "" {
		return f.sessionHash
	}
	return "session-hash"
}

func (f *fakeMiniMaxGatewayService) SelectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, metadataUserID string, sub2apiUserID int64) (*service.AccountSelectionResult, error) {
	f.selectedGroup = groupID
	f.selectedModel = requestedModel
	f.selectedUserID = sub2apiUserID
	if f.selectErr != nil {
		return nil, f.selectErr
	}
	return f.selection, nil
}

func (f *fakeMiniMaxGatewayService) RecordUsage(ctx context.Context, input *service.RecordUsageInput) error {
	f.recorded = input
	return nil
}

type fakeMiniMaxBillingChecker struct {
	calls int
	err   error
}

func (f *fakeMiniMaxBillingChecker) CheckBillingEligibility(ctx context.Context, user *service.User, apiKey *service.APIKey, group *service.Group, subscription *service.UserSubscription, quotaPlatform string) error {
	f.calls++
	return f.err
}

type fakeMiniMaxConcurrencyController struct {
	incrementWaitCalls        int
	decrementWaitCalls        int
	acquireUserCalls          int
	releaseUserCalls          int
	incrementAccountWaitCalls int
	decrementAccountWaitCalls int
	acquireAccountCalls       int
	releaseAccountCalls       int
	accountWaitMax            int
	allowWait                 bool
	allowAccountWait          bool
	startStreamOnUserAcquire  bool
	acquireUserErr            error
}

func (f *fakeMiniMaxConcurrencyController) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	f.incrementWaitCalls++
	return f.allowWait, nil
}

func (f *fakeMiniMaxConcurrencyController) DecrementWaitCount(ctx context.Context, userID int64) {
	f.decrementWaitCalls++
}

func (f *fakeMiniMaxConcurrencyController) AcquireUserSlotWithWait(c *gin.Context, userID int64, maxConcurrency int, isStream bool, streamStarted *bool) (func(), error) {
	f.acquireUserCalls++
	if f.acquireUserErr != nil {
		return nil, f.acquireUserErr
	}
	if f.startStreamOnUserAcquire {
		*streamStarted = true
		c.Writer.WriteHeader(http.StatusOK)
		_, _ = c.Writer.Write([]byte(": ping\n\n"))
	}
	return func() { f.releaseUserCalls++ }, nil
}

func (f *fakeMiniMaxConcurrencyController) IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error) {
	f.incrementAccountWaitCalls++
	f.accountWaitMax = maxWait
	return f.allowAccountWait, nil
}

func (f *fakeMiniMaxConcurrencyController) DecrementAccountWaitCount(ctx context.Context, accountID int64) {
	f.decrementAccountWaitCalls++
}

func (f *fakeMiniMaxConcurrencyController) AcquireAccountSlotWithWaitTimeout(c *gin.Context, accountID int64, maxConcurrency int, timeout time.Duration, isStream bool, streamStarted *bool) (func(), error) {
	f.acquireAccountCalls++
	return func() { f.releaseAccountCalls++ }, nil
}

func newMiniMaxHandlerTestContext(t *testing.T, platform string, body string) (*gin.Context, *httptest.ResponseRecorder, *service.APIKey) {
	return newMiniMaxHandlerTestContextForPath(t, "/v1/messages", platform, body)
}

func newMiniMaxHandlerTestContextForPath(t *testing.T, path string, platform string, body string) (*gin.Context, *httptest.ResponseRecorder, *service.APIKey) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "minimax-test-client")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.ClientRequestID, "client-req-1"))
	c.Request = req

	groupID := int64(42)
	apiKey := &service.APIKey{
		ID:      7,
		UserID:  99,
		GroupID: &groupID,
		Group: &service.Group{
			ID:       groupID,
			Platform: platform,
		},
		User: &service.User{
			ID:          99,
			Concurrency: 2,
			Balance:     10,
			Status:      service.StatusActive,
		},
		Status: service.StatusActive,
	}
	c.Set(string(middleware.ContextKeyAPIKey), apiKey)
	c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 99, Concurrency: 2})
	return c, rec, apiKey
}

func TestMiniMaxGatewayHandlerMessagesSuccessForwardsAndRecordsUsage(t *testing.T) {
	account := &service.Account{
		ID:          101,
		Platform:    service.PlatformMiniMax,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-minimax"},
		Concurrency: 1,
	}
	forwarder := &fakeMiniMaxForwarder{}
	concurrency := &fakeMiniMaxConcurrencyController{allowWait: true}
	billing := &fakeMiniMaxBillingChecker{}
	gateway := &fakeMiniMaxGatewayService{
		selection: &service.AccountSelectionResult{
			Account:  account,
			Acquired: true,
		},
	}
	h := &MiniMaxGatewayHandler{
		minimaxService:      forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   concurrency,
		billingCacheService: billing,
	}
	c, rec, apiKey := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, account, forwarder.account)
	require.JSONEq(t, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`, string(forwarder.body))
	require.Equal(t, "client-req-1", forwarder.requestID)
	require.Equal(t, "claude-sonnet-4-5", gateway.selectedModel)
	require.Equal(t, int64(99), gateway.selectedUserID)
	require.NotNil(t, gateway.recorded)
	require.Equal(t, apiKey, gateway.recorded.APIKey)
	require.Equal(t, account, gateway.recorded.Account)
	require.Equal(t, "/v1/messages", gateway.recorded.InboundEndpoint)
	require.NotEmpty(t, gateway.recorded.RequestPayloadHash)
	require.Equal(t, 1, concurrency.incrementWaitCalls)
	require.Equal(t, 1, concurrency.acquireUserCalls)
	require.Equal(t, 1, concurrency.decrementWaitCalls)
	require.Equal(t, 1, concurrency.releaseUserCalls)
	require.Equal(t, 1, billing.calls)
}

func TestMiniMaxGatewayHandlerChatCompletionsSuccessForwardsAndRecordsUsage(t *testing.T) {
	account := &service.Account{
		ID:          101,
		Platform:    service.PlatformMiniMax,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-minimax"},
		Concurrency: 1,
	}
	forwarder := &fakeMiniMaxForwarder{}
	concurrency := &fakeMiniMaxConcurrencyController{allowWait: true}
	billing := &fakeMiniMaxBillingChecker{}
	gateway := &fakeMiniMaxGatewayService{
		selection: &service.AccountSelectionResult{
			Account:  account,
			Acquired: true,
		},
	}
	h := &MiniMaxGatewayHandler{
		minimaxService:      forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   concurrency,
		billingCacheService: billing,
	}
	c, rec, apiKey := newMiniMaxHandlerTestContextForPath(t, "/v1/chat/completions", service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.ChatCompletions(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, forwarder.chatCalled)
	require.False(t, forwarder.messagesCalled)
	require.Equal(t, account, forwarder.account)
	require.JSONEq(t, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`, string(forwarder.body))
	require.Equal(t, "client-req-1", forwarder.requestID)
	require.Equal(t, "claude-sonnet-4-5", gateway.selectedModel)
	require.Equal(t, int64(99), gateway.selectedUserID)
	require.NotNil(t, gateway.recorded)
	require.Equal(t, apiKey, gateway.recorded.APIKey)
	require.Equal(t, account, gateway.recorded.Account)
	require.Equal(t, "/v1/chat/completions", gateway.recorded.InboundEndpoint)
	require.NotEmpty(t, gateway.recorded.RequestPayloadHash)
	require.Equal(t, 1, concurrency.incrementWaitCalls)
	require.Equal(t, 1, concurrency.acquireUserCalls)
	require.Equal(t, 1, concurrency.decrementWaitCalls)
	require.Equal(t, 1, concurrency.releaseUserCalls)
	require.Equal(t, 1, billing.calls)
}

func TestMiniMaxGatewayHandlerResponsesSuccessForwardsAndRecordsUsage(t *testing.T) {
	account := &service.Account{
		ID:          101,
		Platform:    service.PlatformMiniMax,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-minimax"},
		Concurrency: 1,
	}
	forwarder := &fakeMiniMaxForwarder{}
	concurrency := &fakeMiniMaxConcurrencyController{allowWait: true}
	billing := &fakeMiniMaxBillingChecker{}
	gateway := &fakeMiniMaxGatewayService{
		selection: &service.AccountSelectionResult{
			Account:  account,
			Acquired: true,
		},
	}
	h := &MiniMaxGatewayHandler{
		minimaxService:      forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   concurrency,
		billingCacheService: billing,
	}
	c, rec, apiKey := newMiniMaxHandlerTestContextForPath(t, "/v1/responses", service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","input":"hello","reasoning":{"effort":"medium"}}`)

	h.Responses(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, forwarder.responsesCalled)
	require.False(t, forwarder.messagesCalled)
	require.False(t, forwarder.chatCalled)
	require.Equal(t, account, forwarder.account)
	require.JSONEq(t, `{"model":"claude-sonnet-4-5","input":"hello","reasoning":{"effort":"medium"}}`, string(forwarder.body))
	require.Equal(t, "client-req-1", forwarder.requestID)
	require.Equal(t, "claude-sonnet-4-5", gateway.selectedModel)
	require.Equal(t, int64(99), gateway.selectedUserID)
	require.NotNil(t, gateway.recorded)
	require.Equal(t, apiKey, gateway.recorded.APIKey)
	require.Equal(t, account, gateway.recorded.Account)
	require.Equal(t, "/v1/responses", gateway.recorded.InboundEndpoint)
	require.Equal(t, "/v1/messages", gateway.recorded.UpstreamEndpoint)
	require.NotNil(t, gateway.recorded.Result.ReasoningEffort)
	require.Equal(t, "medium", *gateway.recorded.Result.ReasoningEffort)
	require.NotEmpty(t, gateway.recorded.RequestPayloadHash)
	require.Equal(t, 1, billing.calls)
}

func TestMiniMaxGatewayHandlerResponsesRejectsPreviousResponseIDBeforeForwarding(t *testing.T) {
	forwarder := &fakeMiniMaxForwarder{}
	concurrency := &fakeMiniMaxConcurrencyController{allowWait: true}
	billing := &fakeMiniMaxBillingChecker{}
	h := &MiniMaxGatewayHandler{
		minimaxService:      forwarder,
		gatewayService:      &fakeMiniMaxGatewayService{},
		concurrencyHelper:   concurrency,
		billingCacheService: billing,
	}
	c, rec, _ := newMiniMaxHandlerTestContextForPath(t, "/v1/responses", service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","previous_response_id":"resp_1","input":"hello"}`)

	h.Responses(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid_request_error")
	require.Contains(t, rec.Body.String(), "previous_response_id")
	require.False(t, forwarder.responsesCalled)
	require.Equal(t, 0, concurrency.incrementWaitCalls)
	require.Equal(t, 0, billing.calls)
}

func TestMiniMaxGatewayHandlerMessagesRejectsInvalidPlatform(t *testing.T) {
	h := &MiniMaxGatewayHandler{}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformOpenAI, `{"model":"claude-sonnet-4-5"}`)

	h.Messages(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid_request_error")
	require.Contains(t, rec.Body.String(), "MiniMax")
}

func TestMiniMaxGatewayHandlerMessagesRejectsEmptyBody(t *testing.T) {
	h := &MiniMaxGatewayHandler{}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, "")

	h.Messages(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Request body is empty")
}

func TestMiniMaxGatewayHandlerMessagesRequiresModel(t *testing.T) {
	h := &MiniMaxGatewayHandler{}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "model is required")
}

func TestMiniMaxGatewayHandlerMessagesSelectionFailureReturnsServiceUnavailable(t *testing.T) {
	h := &MiniMaxGatewayHandler{
		minimaxService:      &fakeMiniMaxForwarder{},
		gatewayService:      &fakeMiniMaxGatewayService{selectErr: errors.New("no minimax account")},
		concurrencyHelper:   &fakeMiniMaxConcurrencyController{allowWait: true},
		billingCacheService: &fakeMiniMaxBillingChecker{},
	}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "Service temporarily unavailable")
}

func TestMiniMaxGatewayHandlerMessagesDoesNotWriteJSONAfterStreamStarts(t *testing.T) {
	concurrency := &fakeMiniMaxConcurrencyController{
		allowWait:                true,
		startStreamOnUserAcquire: true,
	}
	h := &MiniMaxGatewayHandler{
		minimaxService:      &fakeMiniMaxForwarder{},
		gatewayService:      &fakeMiniMaxGatewayService{selectErr: errors.New("no minimax account")},
		concurrencyHelper:   concurrency,
		billingCacheService: &fakeMiniMaxBillingChecker{},
	}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","stream":true,"messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, ": ping\n\n", rec.Body.String())
	require.Equal(t, 1, concurrency.releaseUserCalls)
}

func TestMiniMaxGatewayHandlerMessagesDoesNotForwardWithoutAccountSlotOrWaitPlan(t *testing.T) {
	account := &service.Account{
		ID:          101,
		Platform:    service.PlatformMiniMax,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-minimax"},
	}
	forwarder := &fakeMiniMaxForwarder{}
	h := &MiniMaxGatewayHandler{
		minimaxService:      forwarder,
		concurrencyHelper:   &fakeMiniMaxConcurrencyController{allowWait: true},
		billingCacheService: &fakeMiniMaxBillingChecker{},
		gatewayService: &fakeMiniMaxGatewayService{
			selection: &service.AccountSelectionResult{
				Account:  account,
				Acquired: false,
			},
		},
	}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Nil(t, forwarder.account)
}

func TestMiniMaxGatewayHandlerMessagesStopsWhenAccountWaitQueueFull(t *testing.T) {
	account := &service.Account{
		ID:          101,
		Platform:    service.PlatformMiniMax,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-minimax"},
	}
	forwarder := &fakeMiniMaxForwarder{}
	concurrency := &fakeMiniMaxConcurrencyController{
		allowWait:        true,
		allowAccountWait: false,
	}
	h := &MiniMaxGatewayHandler{
		minimaxService:      forwarder,
		concurrencyHelper:   concurrency,
		billingCacheService: &fakeMiniMaxBillingChecker{},
		gatewayService: &fakeMiniMaxGatewayService{
			selection: &service.AccountSelectionResult{
				Account:  account,
				Acquired: false,
				WaitPlan: &service.AccountWaitPlan{
					MaxConcurrency: 1,
					MaxWaiting:     3,
					Timeout:        time.Second,
				},
			},
		},
	}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Nil(t, forwarder.account)
	require.Equal(t, 1, concurrency.incrementAccountWaitCalls)
	require.Equal(t, 3, concurrency.accountWaitMax)
	require.Equal(t, 0, concurrency.acquireAccountCalls)
}

func TestMiniMaxGatewayHandlerMessagesReleasesAccountWaitCounterAfterSlotAcquire(t *testing.T) {
	account := &service.Account{
		ID:          101,
		Platform:    service.PlatformMiniMax,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-minimax"},
	}
	forwarder := &fakeMiniMaxForwarder{}
	concurrency := &fakeMiniMaxConcurrencyController{
		allowWait:        true,
		allowAccountWait: true,
	}
	h := &MiniMaxGatewayHandler{
		minimaxService:      forwarder,
		concurrencyHelper:   concurrency,
		billingCacheService: &fakeMiniMaxBillingChecker{},
		gatewayService: &fakeMiniMaxGatewayService{
			selection: &service.AccountSelectionResult{
				Account:  account,
				Acquired: false,
				WaitPlan: &service.AccountWaitPlan{
					MaxConcurrency: 1,
					MaxWaiting:     4,
					Timeout:        time.Second,
				},
			},
		},
	}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, account, forwarder.account)
	require.Equal(t, 1, concurrency.incrementAccountWaitCalls)
	require.Equal(t, 1, concurrency.decrementAccountWaitCalls)
	require.Equal(t, 1, concurrency.acquireAccountCalls)
	require.Equal(t, 1, concurrency.releaseAccountCalls)
}

func TestMiniMaxGatewayHandlerMessagesStopsWhenUserWaitQueueFull(t *testing.T) {
	forwarder := &fakeMiniMaxForwarder{}
	h := &MiniMaxGatewayHandler{
		minimaxService:      forwarder,
		gatewayService:      &fakeMiniMaxGatewayService{},
		concurrencyHelper:   &fakeMiniMaxConcurrencyController{allowWait: false},
		billingCacheService: &fakeMiniMaxBillingChecker{},
	}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Nil(t, forwarder.account)
}

func TestMiniMaxGatewayHandlerMessagesStopsWhenBillingEligibilityFails(t *testing.T) {
	forwarder := &fakeMiniMaxForwarder{}
	concurrency := &fakeMiniMaxConcurrencyController{allowWait: true}
	billing := &fakeMiniMaxBillingChecker{err: service.ErrUserRPMExceeded}
	h := &MiniMaxGatewayHandler{
		minimaxService:      forwarder,
		gatewayService:      &fakeMiniMaxGatewayService{},
		concurrencyHelper:   concurrency,
		billingCacheService: billing,
	}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Equal(t, 1, billing.calls)
	require.Nil(t, forwarder.account)
}

func TestMiniMaxGatewayHandlerUnsupportedReturnsNotFound(t *testing.T) {
	h := &MiniMaxGatewayHandler{}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, "")

	h.Unsupported(c)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "not_found_error")
}
