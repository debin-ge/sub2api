package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type fakeGLMForwarder struct {
	account        *service.Account
	body           []byte
	requestID      string
	messagesCalled int
	chatCalled     int
	errs           []error
	panicMessages  bool
}

func (f *fakeGLMForwarder) ForwardMessages(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	f.messagesCalled++
	f.account = account
	f.body = append([]byte(nil), body...)
	f.requestID = requestID
	if f.panicMessages {
		panic("glm forward panic")
	}
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	c.JSON(http.StatusOK, gin.H{"id": "msg_1", "model": "claude-sonnet-4-5"})
	return &service.ForwardResult{
		RequestID:     "glm-upstream-req-1",
		Model:         "claude-sonnet-4-5",
		UpstreamModel: "GLM-5.1",
		Usage: service.ClaudeUsage{
			InputTokens:  11,
			OutputTokens: 7,
		},
		Duration: time.Millisecond,
	}, nil
}

func (f *fakeGLMForwarder) ForwardChatCompletions(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	f.chatCalled++
	f.account = account
	f.body = append([]byte(nil), body...)
	f.requestID = requestID
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	c.JSON(http.StatusOK, gin.H{"id": "chatcmpl_1", "model": "glm-4.5-air"})
	return &service.ForwardResult{
		RequestID:     "glm-upstream-chat-req-1",
		Model:         "glm-4.5-air",
		UpstreamModel: "GLM-4.5-air",
		Usage: service.ClaudeUsage{
			InputTokens:  13,
			OutputTokens: 5,
		},
		Duration: time.Millisecond,
	}, nil
}

type fakeGLMGatewayService struct {
	selections      []*service.AccountSelectionResult
	selectErr       error
	recorded        *service.RecordUsageInput
	sessionHash     string
	selectedModel   string
	selectedGroup   *int64
	selectedUserID  int64
	excludedHistory []map[int64]struct{}
	degradedAccount *service.Account
	degradedErr     *service.UpstreamFailoverError
}

func (f *fakeGLMGatewayService) GenerateSessionHash(parsed *service.ParsedRequest) string {
	if f.sessionHash != "" {
		return f.sessionHash
	}
	return "glm-session-hash"
}

func (f *fakeGLMGatewayService) SelectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, metadataUserID string, sub2apiUserID int64) (*service.AccountSelectionResult, error) {
	f.selectedGroup = groupID
	f.selectedModel = requestedModel
	f.selectedUserID = sub2apiUserID
	snapshot := make(map[int64]struct{}, len(excludedIDs))
	for id := range excludedIDs {
		snapshot[id] = struct{}{}
	}
	f.excludedHistory = append(f.excludedHistory, snapshot)
	if f.selectErr != nil {
		return nil, f.selectErr
	}
	if len(f.selections) == 0 {
		return nil, nil
	}
	selection := f.selections[0]
	f.selections = f.selections[1:]
	return selection, nil
}

func (f *fakeGLMGatewayService) RecordUsage(ctx context.Context, input *service.RecordUsageInput) error {
	f.recorded = input
	return nil
}

func (f *fakeGLMGatewayService) HandleGLMUpstreamError(ctx context.Context, account *service.Account, failoverErr *service.UpstreamFailoverError) {
	f.degradedAccount = account
	f.degradedErr = failoverErr
}

type fakeGLMBillingChecker struct {
	calls int
	err   error
}

func (f *fakeGLMBillingChecker) CheckBillingEligibility(ctx context.Context, user *service.User, apiKey *service.APIKey, group *service.Group, subscription *service.UserSubscription) error {
	f.calls++
	return f.err
}

type fakeGLMConcurrencyController struct {
	incrementWaitCalls int
	decrementWaitCalls int
	acquireUserCalls   int
	releaseUserCalls   int
	allowWait          bool
}

func (f *fakeGLMConcurrencyController) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	f.incrementWaitCalls++
	return f.allowWait, nil
}

func (f *fakeGLMConcurrencyController) DecrementWaitCount(ctx context.Context, userID int64) {
	f.decrementWaitCalls++
}

func (f *fakeGLMConcurrencyController) IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error) {
	return true, nil
}

func (f *fakeGLMConcurrencyController) DecrementAccountWaitCount(ctx context.Context, accountID int64) {
}

func (f *fakeGLMConcurrencyController) AcquireUserSlotWithWait(c *gin.Context, userID int64, maxConcurrency int, isStream bool, streamStarted *bool) (func(), error) {
	f.acquireUserCalls++
	return func() { f.releaseUserCalls++ }, nil
}

func (f *fakeGLMConcurrencyController) AcquireAccountSlotWithWaitTimeout(c *gin.Context, accountID int64, maxConcurrency int, timeout time.Duration, isStream bool, streamStarted *bool) (func(), error) {
	return func() {}, nil
}

func newGLMHandlerTestContext(t *testing.T, platform string, body string) (*gin.Context, *httptest.ResponseRecorder, *service.APIKey) {
	return newGLMHandlerTestContextForPath(t, "/v1/messages", platform, body)
}

func newGLMHandlerTestContextForPath(t *testing.T, path string, platform string, body string) (*gin.Context, *httptest.ResponseRecorder, *service.APIKey) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "glm-test-client")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.ClientRequestID, "glm-client-req-1"))
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

func glmTestAccount(id int64) *service.Account {
	return &service.Account{
		ID:          id,
		Platform:    service.PlatformGLM,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-glm"},
		Concurrency: 1,
	}
}

func TestGLMGatewayHandlerMessagesSuccessForwardsAndRecordsUsage(t *testing.T) {
	account := glmTestAccount(101)
	forwarder := &fakeGLMForwarder{}
	concurrency := &fakeGLMConcurrencyController{allowWait: true}
	billing := &fakeGLMBillingChecker{}
	gateway := &fakeGLMGatewayService{
		selections: []*service.AccountSelectionResult{{Account: account, Acquired: true}},
	}
	h := &GLMGatewayHandler{
		glmService:          forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   concurrency,
		billingCacheService: billing,
	}
	c, rec, apiKey := newGLMHandlerTestContext(t, service.PlatformGLM, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, account, forwarder.account)
	require.JSONEq(t, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`, string(forwarder.body))
	require.Equal(t, "glm-client-req-1", forwarder.requestID)
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

func TestGLMGatewayHandlerChatCompletionsSuccessForwardsAndRecordsUsage(t *testing.T) {
	account := glmTestAccount(101)
	forwarder := &fakeGLMForwarder{}
	concurrency := &fakeGLMConcurrencyController{allowWait: true}
	billing := &fakeGLMBillingChecker{}
	gateway := &fakeGLMGatewayService{
		selections: []*service.AccountSelectionResult{{Account: account, Acquired: true}},
	}
	h := &GLMGatewayHandler{
		glmService:          forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   concurrency,
		billingCacheService: billing,
	}
	c, rec, apiKey := newGLMHandlerTestContextForPath(t, "/v1/chat/completions", service.PlatformGLM, `{"model":"glm-4.5-air","messages":[{"role":"user","content":"hello"}]}`)

	h.ChatCompletions(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, forwarder.chatCalled)
	require.Equal(t, 0, forwarder.messagesCalled)
	require.Equal(t, account, forwarder.account)
	require.JSONEq(t, `{"model":"glm-4.5-air","messages":[{"role":"user","content":"hello"}]}`, string(forwarder.body))
	require.Equal(t, "glm-client-req-1", forwarder.requestID)
	require.Equal(t, "glm-4.5-air", gateway.selectedModel)
	require.Equal(t, int64(99), gateway.selectedUserID)
	require.NotNil(t, gateway.recorded)
	require.Equal(t, apiKey, gateway.recorded.APIKey)
	require.Equal(t, account, gateway.recorded.Account)
	require.Equal(t, "/v1/chat/completions", gateway.recorded.InboundEndpoint)
	require.NotEmpty(t, gateway.recorded.RequestPayloadHash)
	require.Equal(t, 1, billing.calls)
}

func TestGLMGatewayHandlerMessagesRejectsInvalidPlatform(t *testing.T) {
	h := &GLMGatewayHandler{}
	c, rec, _ := newGLMHandlerTestContext(t, service.PlatformOpenAI, `{"model":"claude-sonnet-4-5"}`)

	h.Messages(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid_request_error")
	require.Contains(t, rec.Body.String(), "GLM")
}

func TestGLMGatewayHandlerMessagesRejectsEmptyBody(t *testing.T) {
	h := &GLMGatewayHandler{}
	c, rec, _ := newGLMHandlerTestContext(t, service.PlatformGLM, "")

	h.Messages(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Request body is empty")
}

func TestGLMGatewayHandlerMessagesRequiresModel(t *testing.T) {
	h := &GLMGatewayHandler{}
	c, rec, _ := newGLMHandlerTestContext(t, service.PlatformGLM, `{"messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "model is required")
}

func TestGLMGatewayHandlerMessagesSelectionFailureReturnsServiceUnavailable(t *testing.T) {
	h := &GLMGatewayHandler{
		glmService:          &fakeGLMForwarder{},
		gatewayService:      &fakeGLMGatewayService{selectErr: errors.New("no glm account")},
		concurrencyHelper:   &fakeGLMConcurrencyController{allowWait: true},
		billingCacheService: &fakeGLMBillingChecker{},
	}
	c, rec, _ := newGLMHandlerTestContext(t, service.PlatformGLM, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "Service temporarily unavailable")
}

func TestGLMGatewayHandlerMessagesRejectsNonGLMCodingPlanAccount(t *testing.T) {
	released := 0
	h := &GLMGatewayHandler{
		glmService:          &fakeGLMForwarder{},
		gatewayService:      &fakeGLMGatewayService{selections: []*service.AccountSelectionResult{{Account: &service.Account{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}, Acquired: true, ReleaseFunc: func() { released++ }}}},
		concurrencyHelper:   &fakeGLMConcurrencyController{allowWait: true},
		billingCacheService: &fakeGLMBillingChecker{},
	}
	c, rec, _ := newGLMHandlerTestContext(t, service.PlatformGLM, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "No available GLM coding-plan accounts")
	require.Equal(t, 1, released)
}

func TestGLMGatewayHandlerMessagesReleasesAcquiredAccountOnForwardPanic(t *testing.T) {
	released := 0
	account := glmTestAccount(101)
	h := &GLMGatewayHandler{
		glmService: &fakeGLMForwarder{panicMessages: true},
		gatewayService: &fakeGLMGatewayService{selections: []*service.AccountSelectionResult{{
			Account:     account,
			Acquired:    true,
			ReleaseFunc: func() { released++ },
		}}},
		concurrencyHelper:   &fakeGLMConcurrencyController{allowWait: true},
		billingCacheService: &fakeGLMBillingChecker{},
	}
	c, _, _ := newGLMHandlerTestContext(t, service.PlatformGLM, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	require.Panics(t, func() {
		h.Messages(c)
	})
	require.Equal(t, 1, released)
}

func TestGLMGatewayHandlerMessagesStopsWhenBillingEligibilityFails(t *testing.T) {
	forwarder := &fakeGLMForwarder{}
	h := &GLMGatewayHandler{
		glmService:          forwarder,
		gatewayService:      &fakeGLMGatewayService{},
		concurrencyHelper:   &fakeGLMConcurrencyController{allowWait: true},
		billingCacheService: &fakeGLMBillingChecker{err: service.ErrUserRPMExceeded},
	}
	c, rec, _ := newGLMHandlerTestContext(t, service.PlatformGLM, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Nil(t, forwarder.account)
}

func TestGLMGatewayHandlerUnsupportedReturnsNotFound(t *testing.T) {
	h := &GLMGatewayHandler{}
	c, rec, _ := newGLMHandlerTestContext(t, service.PlatformGLM, "")

	h.Unsupported(c)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "not_found_error")
	require.Contains(t, rec.Body.String(), "GLM gateway supports /v1/messages and /v1/chat/completions only")
}

func TestGLMGatewayHandlerRetryableUpstreamErrorFailsOverToNextAccount(t *testing.T) {
	first := glmTestAccount(101)
	second := glmTestAccount(102)
	forwarder := &fakeGLMForwarder{
		errs: []error{&service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}, nil},
	}
	gateway := &fakeGLMGatewayService{
		selections: []*service.AccountSelectionResult{
			{Account: first, Acquired: true},
			{Account: second, Acquired: true},
		},
	}
	h := &GLMGatewayHandler{
		glmService:          forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   &fakeGLMConcurrencyController{allowWait: true},
		billingCacheService: &fakeGLMBillingChecker{},
		maxAccountSwitches:  3,
	}
	c, rec, _ := newGLMHandlerTestContext(t, service.PlatformGLM, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 2, forwarder.messagesCalled)
	require.Equal(t, second, forwarder.account)
	require.Len(t, gateway.excludedHistory, 2)
	require.Contains(t, gateway.excludedHistory[1], first.ID)
	require.NotNil(t, gateway.recorded)
	require.Equal(t, second, gateway.recorded.Account)
	require.Equal(t, first, gateway.degradedAccount)
	require.NotNil(t, gateway.degradedErr)
}

func TestGLMGatewayHandlerChatCompletionsUsesOpenAICompatiblePingFormat(t *testing.T) {
	h := NewGLMGatewayHandler(nil, nil, nil, nil, nil, service.NewConcurrencyService(nil), &config.Config{})

	helper, ok := h.chatConcurrencyHelper.(*ConcurrencyHelper)
	require.True(t, ok)
	require.NotEqual(t, SSEPingFormatClaude, helper.pingFormat)
	require.Equal(t, SSEPingFormatComment, helper.pingFormat)
}
