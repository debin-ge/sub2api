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
	account   *service.Account
	body      []byte
	requestID string
	err       error
}

func (f *fakeMiniMaxForwarder) ForwardMessages(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
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

func newMiniMaxHandlerTestContext(t *testing.T, platform string, body string) (*gin.Context, *httptest.ResponseRecorder, *service.APIKey) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(body))
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
	gateway := &fakeMiniMaxGatewayService{
		selection: &service.AccountSelectionResult{
			Account:  account,
			Acquired: true,
		},
	}
	h := &MiniMaxGatewayHandler{
		minimaxService: forwarder,
		gatewayService: gateway,
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
		minimaxService: &fakeMiniMaxForwarder{},
		gatewayService: &fakeMiniMaxGatewayService{selectErr: errors.New("no minimax account")},
	}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Contains(t, rec.Body.String(), "Service temporarily unavailable")
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
		minimaxService: forwarder,
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

func TestMiniMaxGatewayHandlerUnsupportedReturnsNotFound(t *testing.T) {
	h := &MiniMaxGatewayHandler{}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, "")

	h.Unsupported(c)

	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Contains(t, rec.Body.String(), "not_found_error")
}
