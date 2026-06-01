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

type fakeKimiForwarder struct {
	account         *service.Account
	body            []byte
	requestID       string
	messagesCalled  int
	chatCalled      int
	responsesCalled int
	errs            []error
	panicMessages   bool
}

func (f *fakeKimiForwarder) ForwardMessages(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	f.messagesCalled++
	f.account = account
	f.body = append([]byte(nil), body...)
	f.requestID = requestID
	if f.panicMessages {
		panic("kimi forward panic")
	}
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return nil, err
		}
	}
	c.JSON(http.StatusOK, gin.H{"id": "msg_1", "model": "kimi-for-coding"})
	return &service.ForwardResult{
		RequestID:     "kimi-upstream-req-1",
		Model:         "kimi-for-coding",
		UpstreamModel: "kimi-for-coding",
		Usage: service.ClaudeUsage{
			InputTokens:  11,
			OutputTokens: 7,
		},
		Duration: time.Millisecond,
	}, nil
}

func (f *fakeKimiForwarder) ForwardChatCompletions(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
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
	c.JSON(http.StatusOK, gin.H{"id": "chatcmpl_1", "model": "kimi-for-coding"})
	return &service.ForwardResult{
		RequestID:     "kimi-upstream-chat-req-1",
		Model:         "kimi-for-coding",
		UpstreamModel: "kimi-for-coding",
		Usage: service.ClaudeUsage{
			InputTokens:  13,
			OutputTokens: 5,
		},
		Duration: time.Millisecond,
	}, nil
}

func (f *fakeKimiForwarder) ForwardResponses(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	f.responsesCalled++
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
	c.JSON(http.StatusOK, gin.H{"id": "resp_1", "object": "response", "model": "kimi-for-coding"})
	effort := "medium"
	return &service.ForwardResult{
		RequestID:       "kimi-upstream-resp-req-1",
		Model:           "kimi-for-coding",
		UpstreamModel:   "kimi-for-coding",
		ReasoningEffort: &effort,
		Usage: service.ClaudeUsage{
			InputTokens:  9,
			OutputTokens: 4,
		},
		Duration: time.Millisecond,
	}, nil
}

type fakeKimiGatewayService struct {
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

func (f *fakeKimiGatewayService) GenerateSessionHash(parsed *service.ParsedRequest) string {
	if f.sessionHash != "" {
		return f.sessionHash
	}
	return "kimi-session-hash"
}

func (f *fakeKimiGatewayService) SelectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, metadataUserID string, sub2apiUserID int64) (*service.AccountSelectionResult, error) {
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

func (f *fakeKimiGatewayService) RecordUsage(ctx context.Context, input *service.RecordUsageInput) error {
	f.recorded = input
	return nil
}

func (f *fakeKimiGatewayService) HandleKimiUpstreamError(ctx context.Context, account *service.Account, failoverErr *service.UpstreamFailoverError) {
	f.degradedAccount = account
	f.degradedErr = failoverErr
}

type fakeKimiBillingChecker struct {
	calls int
	err   error
}

func (f *fakeKimiBillingChecker) CheckBillingEligibility(ctx context.Context, user *service.User, apiKey *service.APIKey, group *service.Group, subscription *service.UserSubscription) error {
	f.calls++
	return f.err
}

type fakeKimiConcurrencyController struct {
	incrementWaitCalls int
	decrementWaitCalls int
	acquireUserCalls   int
	releaseUserCalls   int
	allowWait          bool
}

func (f *fakeKimiConcurrencyController) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	f.incrementWaitCalls++
	return f.allowWait, nil
}

func (f *fakeKimiConcurrencyController) DecrementWaitCount(ctx context.Context, userID int64) {
	f.decrementWaitCalls++
}

func (f *fakeKimiConcurrencyController) IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error) {
	return true, nil
}

func (f *fakeKimiConcurrencyController) DecrementAccountWaitCount(ctx context.Context, accountID int64) {
}

func (f *fakeKimiConcurrencyController) AcquireUserSlotWithWait(c *gin.Context, userID int64, maxConcurrency int, isStream bool, streamStarted *bool) (func(), error) {
	f.acquireUserCalls++
	return func() { f.releaseUserCalls++ }, nil
}

func (f *fakeKimiConcurrencyController) AcquireAccountSlotWithWaitTimeout(c *gin.Context, accountID int64, maxConcurrency int, timeout time.Duration, isStream bool, streamStarted *bool) (func(), error) {
	return func() {}, nil
}

func newKimiHandlerTestContext(t *testing.T, platform string, body string) (*gin.Context, *httptest.ResponseRecorder, *service.APIKey) {
	return newKimiHandlerTestContextForPath(t, "/v1/messages", platform, body)
}

func newKimiHandlerTestContextForPath(t *testing.T, path string, platform string, body string) (*gin.Context, *httptest.ResponseRecorder, *service.APIKey) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "kimi-test-client")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.ClientRequestID, "kimi-client-req-1"))
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

func kimiTestAccount(id int64) *service.Account {
	return &service.Account{
		ID:          id,
		Platform:    service.PlatformKimi,
		Type:        service.AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-kimi"},
		Concurrency: 1,
	}
}

func TestKimiGatewayHandlerMessagesSuccessForwardsAndRecordsUsage(t *testing.T) {
	account := kimiTestAccount(101)
	forwarder := &fakeKimiForwarder{}
	concurrency := &fakeKimiConcurrencyController{allowWait: true}
	billing := &fakeKimiBillingChecker{}
	gateway := &fakeKimiGatewayService{
		selections: []*service.AccountSelectionResult{{Account: account, Acquired: true}},
	}
	h := &KimiGatewayHandler{
		kimiService:         forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   concurrency,
		billingCacheService: billing,
	}
	c, rec, apiKey := newKimiHandlerTestContext(t, service.PlatformKimi, `{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, account, forwarder.account)
	require.JSONEq(t, `{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`, string(forwarder.body))
	require.Equal(t, "kimi-client-req-1", forwarder.requestID)
	require.Equal(t, "kimi-for-coding", gateway.selectedModel)
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

func TestKimiGatewayHandlerChatCompletionsSuccessForwardsAndRecordsUsage(t *testing.T) {
	account := kimiTestAccount(101)
	forwarder := &fakeKimiForwarder{}
	concurrency := &fakeKimiConcurrencyController{allowWait: true}
	billing := &fakeKimiBillingChecker{}
	gateway := &fakeKimiGatewayService{
		selections: []*service.AccountSelectionResult{{Account: account, Acquired: true}},
	}
	h := &KimiGatewayHandler{
		kimiService:         forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   concurrency,
		billingCacheService: billing,
	}
	c, rec, apiKey := newKimiHandlerTestContextForPath(t, "/v1/chat/completions", service.PlatformKimi, `{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`)

	h.ChatCompletions(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, forwarder.chatCalled)
	require.Equal(t, 0, forwarder.messagesCalled)
	require.Equal(t, account, forwarder.account)
	require.JSONEq(t, `{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`, string(forwarder.body))
	require.Equal(t, "kimi-client-req-1", forwarder.requestID)
	require.Equal(t, "kimi-for-coding", gateway.selectedModel)
	require.Equal(t, int64(99), gateway.selectedUserID)
	require.NotNil(t, gateway.recorded)
	require.Equal(t, apiKey, gateway.recorded.APIKey)
	require.Equal(t, account, gateway.recorded.Account)
	require.Equal(t, "/v1/chat/completions", gateway.recorded.InboundEndpoint)
	require.NotEmpty(t, gateway.recorded.RequestPayloadHash)
	require.Equal(t, 1, billing.calls)
}

func TestKimiGatewayHandlerResponsesSuccessForwardsAndRecordsUsage(t *testing.T) {
	account := kimiTestAccount(101)
	forwarder := &fakeKimiForwarder{}
	concurrency := &fakeKimiConcurrencyController{allowWait: true}
	billing := &fakeKimiBillingChecker{}
	gateway := &fakeKimiGatewayService{
		selections: []*service.AccountSelectionResult{{Account: account, Acquired: true}},
	}
	h := &KimiGatewayHandler{
		kimiService:         forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   concurrency,
		billingCacheService: billing,
	}
	c, rec, apiKey := newKimiHandlerTestContextForPath(t, "/v1/responses", service.PlatformKimi, `{"model":"kimi-for-coding","input":"hello","reasoning":{"effort":"medium"}}`)

	h.Responses(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, forwarder.responsesCalled)
	require.Equal(t, 0, forwarder.messagesCalled)
	require.Equal(t, 0, forwarder.chatCalled)
	require.Equal(t, account, forwarder.account)
	require.JSONEq(t, `{"model":"kimi-for-coding","input":"hello","reasoning":{"effort":"medium"}}`, string(forwarder.body))
	require.Equal(t, "kimi-client-req-1", forwarder.requestID)
	require.Equal(t, "kimi-for-coding", gateway.selectedModel)
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

func TestKimiGatewayHandlerResponsesRejectsNonKimiModel(t *testing.T) {
	forwarder := &fakeKimiForwarder{}
	h := &KimiGatewayHandler{
		kimiService:         forwarder,
		gatewayService:      &fakeKimiGatewayService{},
		concurrencyHelper:   &fakeKimiConcurrencyController{allowWait: true},
		billingCacheService: &fakeKimiBillingChecker{},
	}
	c, rec, _ := newKimiHandlerTestContextForPath(t, "/v1/responses", service.PlatformKimi, `{"model":"claude-sonnet-4-5","input":"hello"}`)

	h.Responses(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Kimi gateway only supports model kimi-for-coding")
	require.Equal(t, 0, forwarder.responsesCalled)
}

func TestKimiGatewayHandlerResponsesRejectsPreviousResponseIDBeforeForwarding(t *testing.T) {
	forwarder := &fakeKimiForwarder{}
	concurrency := &fakeKimiConcurrencyController{allowWait: true}
	billing := &fakeKimiBillingChecker{}
	h := &KimiGatewayHandler{
		kimiService:         forwarder,
		gatewayService:      &fakeKimiGatewayService{},
		concurrencyHelper:   concurrency,
		billingCacheService: billing,
	}
	c, rec, _ := newKimiHandlerTestContextForPath(t, "/v1/responses", service.PlatformKimi, `{"model":"kimi-for-coding","previous_response_id":"resp_1","input":"hello"}`)

	h.Responses(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid_request_error")
	require.Contains(t, rec.Body.String(), "previous_response_id")
	require.Equal(t, 0, forwarder.responsesCalled)
	require.Equal(t, 0, concurrency.incrementWaitCalls)
	require.Equal(t, 0, billing.calls)
}

func TestKimiGatewayHandlerMessagesRejectsInvalidPlatform(t *testing.T) {
	h := &KimiGatewayHandler{}
	c, rec, _ := newKimiHandlerTestContext(t, service.PlatformOpenAI, `{"model":"kimi-for-coding"}`)

	h.Messages(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid_request_error")
	require.Contains(t, rec.Body.String(), "Kimi")
}

func TestKimiGatewayHandlerMessagesRejectsEmptyBody(t *testing.T) {
	h := &KimiGatewayHandler{}
	c, rec, _ := newKimiHandlerTestContext(t, service.PlatformKimi, "")

	h.Messages(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Request body is empty")
}

func TestKimiGatewayHandlerMessagesRequiresModel(t *testing.T) {
	h := &KimiGatewayHandler{}
	c, rec, _ := newKimiHandlerTestContext(t, service.PlatformKimi, `{"messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "model is required")
}

func TestKimiGatewayHandlerMessagesRejectsNonKimiModel(t *testing.T) {
	forwarder := &fakeKimiForwarder{}
	h := &KimiGatewayHandler{
		kimiService:         forwarder,
		gatewayService:      &fakeKimiGatewayService{},
		concurrencyHelper:   &fakeKimiConcurrencyController{allowWait: true},
		billingCacheService: &fakeKimiBillingChecker{},
	}
	c, rec, _ := newKimiHandlerTestContext(t, service.PlatformKimi, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Kimi gateway only supports model kimi-for-coding")
	require.Equal(t, 0, forwarder.messagesCalled)
}

func TestKimiGatewayHandlerChatCompletionsRejectsNonExactKimiModel(t *testing.T) {
	forwarder := &fakeKimiForwarder{}
	h := &KimiGatewayHandler{
		kimiService:         forwarder,
		gatewayService:      &fakeKimiGatewayService{},
		concurrencyHelper:   &fakeKimiConcurrencyController{allowWait: true},
		billingCacheService: &fakeKimiBillingChecker{},
	}
	c, rec, _ := newKimiHandlerTestContextForPath(t, "/v1/chat/completions", service.PlatformKimi, `{"model":" kimi-for-coding ","messages":[{"role":"user","content":"hello"}]}`)

	h.ChatCompletions(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Kimi gateway only supports model kimi-for-coding")
	require.Equal(t, 0, forwarder.chatCalled)
}

func TestKimiGatewayHandlerMessagesSelectionFailureReturnsServiceUnavailable(t *testing.T) {
	h := &KimiGatewayHandler{
		kimiService:         &fakeKimiForwarder{},
		gatewayService:      &fakeKimiGatewayService{selectErr: errors.New("no kimi account")},
		concurrencyHelper:   &fakeKimiConcurrencyController{allowWait: true},
		billingCacheService: &fakeKimiBillingChecker{},
	}
	c, rec, _ := newKimiHandlerTestContext(t, service.PlatformKimi, `{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "Service temporarily unavailable")
}

func TestKimiGatewayHandlerMessagesRejectsNonKimiCodingPlanAccount(t *testing.T) {
	released := 0
	h := &KimiGatewayHandler{
		kimiService:         &fakeKimiForwarder{},
		gatewayService:      &fakeKimiGatewayService{selections: []*service.AccountSelectionResult{{Account: &service.Account{ID: 101, Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey}, Acquired: true, ReleaseFunc: func() { released++ }}}},
		concurrencyHelper:   &fakeKimiConcurrencyController{allowWait: true},
		billingCacheService: &fakeKimiBillingChecker{},
	}
	c, rec, _ := newKimiHandlerTestContext(t, service.PlatformKimi, `{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "No available Kimi coding-plan accounts")
	require.Equal(t, 1, released)
}

func TestKimiGatewayHandlerMessagesReleasesAcquiredAccountOnForwardPanic(t *testing.T) {
	released := 0
	account := kimiTestAccount(101)
	h := &KimiGatewayHandler{
		kimiService: &fakeKimiForwarder{panicMessages: true},
		gatewayService: &fakeKimiGatewayService{selections: []*service.AccountSelectionResult{{
			Account:     account,
			Acquired:    true,
			ReleaseFunc: func() { released++ },
		}}},
		concurrencyHelper:   &fakeKimiConcurrencyController{allowWait: true},
		billingCacheService: &fakeKimiBillingChecker{},
	}
	c, _, _ := newKimiHandlerTestContext(t, service.PlatformKimi, `{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`)

	require.Panics(t, func() {
		h.Messages(c)
	})
	require.Equal(t, 1, released)
}

func TestKimiGatewayHandlerMessagesStopsWhenBillingEligibilityFails(t *testing.T) {
	forwarder := &fakeKimiForwarder{}
	h := &KimiGatewayHandler{
		kimiService:         forwarder,
		gatewayService:      &fakeKimiGatewayService{},
		concurrencyHelper:   &fakeKimiConcurrencyController{allowWait: true},
		billingCacheService: &fakeKimiBillingChecker{err: service.ErrUserRPMExceeded},
	}
	c, rec, _ := newKimiHandlerTestContext(t, service.PlatformKimi, `{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Nil(t, forwarder.account)
}

func TestKimiGatewayHandlerUnsupportedReturnsNotFound(t *testing.T) {
	h := &KimiGatewayHandler{}
	c, rec, _ := newKimiHandlerTestContext(t, service.PlatformKimi, "")

	h.Unsupported(c)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "not_found_error")
	require.Contains(t, rec.Body.String(), "Kimi gateway supports /v1/messages, /v1/chat/completions, and /v1/responses only")
}

func TestKimiGatewayHandlerRetryableUpstreamErrorFailsOverToNextAccount(t *testing.T) {
	first := kimiTestAccount(101)
	second := kimiTestAccount(102)
	forwarder := &fakeKimiForwarder{
		errs: []error{&service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests}, nil},
	}
	gateway := &fakeKimiGatewayService{
		selections: []*service.AccountSelectionResult{
			{Account: first, Acquired: true},
			{Account: second, Acquired: true},
		},
	}
	h := &KimiGatewayHandler{
		kimiService:         forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   &fakeKimiConcurrencyController{allowWait: true},
		billingCacheService: &fakeKimiBillingChecker{},
		maxAccountSwitches:  3,
	}
	c, rec, _ := newKimiHandlerTestContext(t, service.PlatformKimi, `{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`)

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

func TestKimiGatewayHandlerNonRetryableUpstreamErrorDoesNotRecordOrFailover(t *testing.T) {
	first := kimiTestAccount(101)
	second := kimiTestAccount(102)
	forwarder := &fakeKimiForwarder{
		errs: []error{&service.UpstreamFailoverError{
			StatusCode:   http.StatusBadRequest,
			ResponseBody: []byte(`{"error":{"type":"invalid_request_error","message":"max_tokens is required"}}`),
		}},
	}
	gateway := &fakeKimiGatewayService{
		selections: []*service.AccountSelectionResult{
			{Account: first, Acquired: true},
			{Account: second, Acquired: true},
		},
	}
	h := &KimiGatewayHandler{
		kimiService:         forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   &fakeKimiConcurrencyController{allowWait: true},
		billingCacheService: &fakeKimiBillingChecker{},
		maxAccountSwitches:  3,
	}
	c, rec, _ := newKimiHandlerTestContext(t, service.PlatformKimi, `{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "Kimi upstream rejected request: max_tokens is required")
	require.Equal(t, 1, forwarder.messagesCalled)
	require.Equal(t, first, forwarder.account)
	require.Len(t, gateway.excludedHistory, 1)
	require.Nil(t, gateway.recorded)
	require.Nil(t, gateway.degradedAccount)
	require.Nil(t, gateway.degradedErr)
}

func TestKimiGatewayHandlerAuthUpstreamErrorMarksAccountWithoutFailover(t *testing.T) {
	first := kimiTestAccount(101)
	second := kimiTestAccount(102)
	failoverErr := &service.UpstreamFailoverError{StatusCode: http.StatusUnauthorized}
	forwarder := &fakeKimiForwarder{
		errs: []error{failoverErr},
	}
	gateway := &fakeKimiGatewayService{
		selections: []*service.AccountSelectionResult{
			{Account: first, Acquired: true},
			{Account: second, Acquired: true},
		},
	}
	h := &KimiGatewayHandler{
		kimiService:         forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   &fakeKimiConcurrencyController{allowWait: true},
		billingCacheService: &fakeKimiBillingChecker{},
		maxAccountSwitches:  3,
	}
	c, rec, _ := newKimiHandlerTestContext(t, service.PlatformKimi, `{"model":"kimi-for-coding","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Equal(t, 1, forwarder.messagesCalled)
	require.Equal(t, first, forwarder.account)
	require.Len(t, gateway.excludedHistory, 1)
	require.Nil(t, gateway.recorded)
	require.Equal(t, first, gateway.degradedAccount)
	require.Equal(t, failoverErr, gateway.degradedErr)
}

func TestKimiGatewayHandlerChatCompletionsUsesOpenAICompatiblePingFormat(t *testing.T) {
	h := NewKimiGatewayHandler(nil, nil, nil, nil, nil, service.NewConcurrencyService(nil), &config.Config{})

	helper, ok := h.chatConcurrencyHelper.(*ConcurrencyHelper)
	require.True(t, ok)
	require.NotEqual(t, SSEPingFormatClaude, helper.pingFormat)
	require.Equal(t, SSEPingFormatComment, helper.pingFormat)
}
