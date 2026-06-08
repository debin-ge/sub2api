package handler

import (
	"context"
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

type fakeWindsurfForwarder struct {
	account        *service.Account
	body           []byte
	requestID      string
	messagesCalled int
	chatCalled     int
}

func (f *fakeWindsurfForwarder) ForwardMessages(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	f.messagesCalled++
	f.account = account
	f.body = append([]byte(nil), body...)
	f.requestID = requestID
	c.JSON(http.StatusOK, gin.H{"id": "msg_1", "model": "claude-sonnet-4.6"})
	return &service.ForwardResult{
		RequestID:     "windsurf-upstream-req-1",
		Model:         "claude-3-5-sonnet-latest",
		UpstreamModel: "claude-sonnet-4.6",
		Usage: service.ClaudeUsage{
			InputTokens:  11,
			OutputTokens: 7,
		},
		Duration: time.Millisecond,
	}, nil
}

func (f *fakeWindsurfForwarder) ForwardChatCompletions(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	f.chatCalled++
	f.account = account
	f.body = append([]byte(nil), body...)
	f.requestID = requestID
	c.JSON(http.StatusOK, gin.H{"id": "chatcmpl_1", "model": "claude-sonnet-4.6"})
	return &service.ForwardResult{
		RequestID:     "windsurf-upstream-chat-req-1",
		Model:         "claude-3-5-sonnet-latest",
		UpstreamModel: "claude-sonnet-4.6",
		Usage: service.ClaudeUsage{
			InputTokens:  13,
			OutputTokens: 5,
		},
		Duration: time.Millisecond,
	}, nil
}

type fakeWindsurfGatewayService struct {
	selections     []*service.AccountSelectionResult
	recorded       *service.RecordUsageInput
	sessionHash    string
	selectedModel  string
	selectedGroup  *int64
	selectedUserID int64
}

func (f *fakeWindsurfGatewayService) GenerateSessionHash(parsed *service.ParsedRequest) string {
	if f.sessionHash != "" {
		return f.sessionHash
	}
	return "windsurf-session-hash"
}

func (f *fakeWindsurfGatewayService) SelectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, metadataUserID string, sub2apiUserID int64) (*service.AccountSelectionResult, error) {
	f.selectedGroup = groupID
	f.selectedModel = requestedModel
	f.selectedUserID = sub2apiUserID
	if len(f.selections) == 0 {
		return nil, nil
	}
	selection := f.selections[0]
	f.selections = f.selections[1:]
	return selection, nil
}

func (f *fakeWindsurfGatewayService) RecordUsage(ctx context.Context, input *service.RecordUsageInput) error {
	f.recorded = input
	return nil
}

type fakeWindsurfBillingChecker struct {
	calls int
}

func (f *fakeWindsurfBillingChecker) CheckBillingEligibility(ctx context.Context, user *service.User, apiKey *service.APIKey, group *service.Group, subscription *service.UserSubscription, quotaPlatform string) error {
	f.calls++
	return nil
}

type fakeWindsurfConcurrencyController struct {
	incrementWaitCalls int
	decrementWaitCalls int
	acquireUserCalls   int
	releaseUserCalls   int
	allowWait          bool
}

func (f *fakeWindsurfConcurrencyController) IncrementWaitCount(ctx context.Context, userID int64, maxWait int) (bool, error) {
	f.incrementWaitCalls++
	return f.allowWait, nil
}

func (f *fakeWindsurfConcurrencyController) DecrementWaitCount(ctx context.Context, userID int64) {
	f.decrementWaitCalls++
}

func (f *fakeWindsurfConcurrencyController) IncrementAccountWaitCount(ctx context.Context, accountID int64, maxWait int) (bool, error) {
	return true, nil
}

func (f *fakeWindsurfConcurrencyController) DecrementAccountWaitCount(ctx context.Context, accountID int64) {
}

func (f *fakeWindsurfConcurrencyController) AcquireUserSlotWithWait(c *gin.Context, userID int64, maxConcurrency int, isStream bool, streamStarted *bool) (func(), error) {
	f.acquireUserCalls++
	return func() { f.releaseUserCalls++ }, nil
}

func (f *fakeWindsurfConcurrencyController) AcquireAccountSlotWithWaitTimeout(c *gin.Context, accountID int64, maxConcurrency int, timeout time.Duration, isStream bool, streamStarted *bool) (func(), error) {
	return func() {}, nil
}

func newWindsurfHandlerTestContext(t *testing.T, path string, platform string, body string) (*gin.Context, *httptest.ResponseRecorder, *service.APIKey) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "windsurf-test-client")
	req = req.WithContext(context.WithValue(req.Context(), ctxkey.ClientRequestID, "windsurf-client-req-1"))
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

func windsurfTestAccount(id int64) *service.Account {
	return &service.Account{
		ID:       id,
		Platform: service.PlatformWindsurf,
		Type:     service.AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key": "sk-windsurf",
			"model_mapping": map[string]any{
				"claude-3-5-sonnet-latest": "claude-sonnet-4.6",
			},
		},
		Concurrency: 1,
	}
}

func TestWindsurfGatewayHandlerMessagesAllowsMappedModelAndRecordsUsage(t *testing.T) {
	account := windsurfTestAccount(101)
	forwarder := &fakeWindsurfForwarder{}
	concurrency := &fakeWindsurfConcurrencyController{allowWait: true}
	billing := &fakeWindsurfBillingChecker{}
	gateway := &fakeWindsurfGatewayService{
		selections: []*service.AccountSelectionResult{{Account: account, Acquired: true}},
	}
	h := &WindsurfGatewayHandler{
		windsurfService:     forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   concurrency,
		billingCacheService: billing,
	}
	c, rec, apiKey := newWindsurfHandlerTestContext(t, "/v1/messages", service.PlatformWindsurf, `{"model":"claude-3-5-sonnet-latest","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, account, forwarder.account)
	require.JSONEq(t, `{"model":"claude-3-5-sonnet-latest","messages":[{"role":"user","content":"hello"}]}`, string(forwarder.body))
	require.Equal(t, "windsurf-client-req-1", forwarder.requestID)
	require.Equal(t, "claude-3-5-sonnet-latest", gateway.selectedModel)
	require.NotNil(t, gateway.recorded)
	require.Equal(t, apiKey, gateway.recorded.APIKey)
	require.Equal(t, account, gateway.recorded.Account)
	require.Equal(t, "/v1/messages", gateway.recorded.InboundEndpoint)
	require.Equal(t, 1, concurrency.incrementWaitCalls)
	require.Equal(t, 1, concurrency.acquireUserCalls)
	require.Equal(t, 1, billing.calls)
}

func TestWindsurfGatewayHandlerChatCompletionsSuccessForwardsAndRecordsUsage(t *testing.T) {
	account := windsurfTestAccount(101)
	forwarder := &fakeWindsurfForwarder{}
	billing := &fakeWindsurfBillingChecker{}
	gateway := &fakeWindsurfGatewayService{
		selections: []*service.AccountSelectionResult{{Account: account, Acquired: true}},
	}
	h := &WindsurfGatewayHandler{
		windsurfService:       forwarder,
		gatewayService:        gateway,
		concurrencyHelper:     &fakeWindsurfConcurrencyController{allowWait: true},
		chatConcurrencyHelper: &fakeWindsurfConcurrencyController{allowWait: true},
		billingCacheService:   billing,
	}
	c, rec, apiKey := newWindsurfHandlerTestContext(t, "/v1/chat/completions", service.PlatformWindsurf, `{"model":"claude-sonnet-4.6","messages":[{"role":"user","content":"hello"}]}`)

	h.ChatCompletions(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, forwarder.chatCalled)
	require.Equal(t, 0, forwarder.messagesCalled)
	require.Equal(t, account, forwarder.account)
	require.Equal(t, "claude-sonnet-4.6", gateway.selectedModel)
	require.NotNil(t, gateway.recorded)
	require.Equal(t, apiKey, gateway.recorded.APIKey)
	require.Equal(t, account, gateway.recorded.Account)
	require.Equal(t, "/v1/chat/completions", gateway.recorded.InboundEndpoint)
	require.Equal(t, 1, billing.calls)
}

func TestWindsurfGatewayHandlerMessagesRejectsInvalidPlatform(t *testing.T) {
	h := &WindsurfGatewayHandler{}
	c, rec, _ := newWindsurfHandlerTestContext(t, "/v1/messages", service.PlatformOpenAI, `{"model":"claude-sonnet-4.6"}`)

	h.Messages(c)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "invalid_request_error")
	require.Contains(t, rec.Body.String(), "Windsurf")
}

func TestWindsurfGatewayHandlerUnsupportedReturnsNotFound(t *testing.T) {
	h := &WindsurfGatewayHandler{}
	c, rec, _ := newWindsurfHandlerTestContext(t, "/v1/responses", service.PlatformWindsurf, "")

	h.Unsupported(c)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "not_found_error")
	require.Contains(t, rec.Body.String(), "Windsurf gateway supports /v1/messages and /v1/chat/completions only")
}
