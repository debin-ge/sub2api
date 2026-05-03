package handler

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type miniMaxFailoverForwarder struct {
	mu       sync.Mutex
	errors   []error
	accounts []int64
}

func (f *miniMaxFailoverForwarder) ForwardMessages(ctx context.Context, c *gin.Context, account *service.Account, body []byte, requestID string) (*service.ForwardResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.accounts = append(f.accounts, account.ID)
	if len(f.errors) > 0 {
		err := f.errors[0]
		f.errors = f.errors[1:]
		if err != nil {
			return nil, err
		}
	}

	c.JSON(http.StatusOK, gin.H{"id": "msg_2"})
	return &service.ForwardResult{
		RequestID: "upstream-req-2",
		Model:     "claude-sonnet-4-5",
		Usage: service.ClaudeUsage{
			InputTokens:  3,
			OutputTokens: 5,
		},
		Duration: time.Millisecond,
	}, nil
}

type miniMaxFailoverGatewayService struct {
	selections []*service.AccountSelectionResult

	selectedExcluded []map[int64]struct{}
	recorded         *service.RecordUsageInput
	degraded         []miniMaxDegradeCall
}

type miniMaxDegradeCall struct {
	accountID int64
	status    int
}

func (s *miniMaxFailoverGatewayService) GenerateSessionHash(parsed *service.ParsedRequest) string {
	return "session-hash"
}

func (s *miniMaxFailoverGatewayService) SelectAccountWithLoadAwareness(ctx context.Context, groupID *int64, sessionHash string, requestedModel string, excludedIDs map[int64]struct{}, metadataUserID string, sub2apiUserID int64) (*service.AccountSelectionResult, error) {
	copied := make(map[int64]struct{}, len(excludedIDs))
	for id := range excludedIDs {
		copied[id] = struct{}{}
	}
	s.selectedExcluded = append(s.selectedExcluded, copied)
	if len(s.selections) == 0 {
		return nil, service.ErrNoAvailableAccounts
	}
	selection := s.selections[0]
	s.selections = s.selections[1:]
	return selection, nil
}

func (s *miniMaxFailoverGatewayService) RecordUsage(ctx context.Context, input *service.RecordUsageInput) error {
	s.recorded = input
	return nil
}

func (s *miniMaxFailoverGatewayService) HandleMiniMaxUpstreamError(ctx context.Context, account *service.Account, failoverErr *service.UpstreamFailoverError) {
	if account == nil || failoverErr == nil {
		return
	}
	s.degraded = append(s.degraded, miniMaxDegradeCall{
		accountID: account.ID,
		status:    failoverErr.StatusCode,
	})
}

func TestMiniMaxGatewayHandlerMessagesFailoverRetriesNextAccountAndRecordsUsage(t *testing.T) {
	first := &service.Account{ID: 101, Platform: service.PlatformMiniMax, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-1"}}
	second := &service.Account{ID: 202, Platform: service.PlatformMiniMax, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-2"}}
	gateway := &miniMaxFailoverGatewayService{
		selections: []*service.AccountSelectionResult{
			{Account: first, Acquired: true, ReleaseFunc: func() {}},
			{Account: second, Acquired: true, ReleaseFunc: func() {}},
		},
	}
	forwarder := &miniMaxFailoverForwarder{
		errors: []error{&service.UpstreamFailoverError{StatusCode: http.StatusTooManyRequests, ResponseBody: []byte(`{"error":{"message":"rate limited"}}`)}},
	}
	h := &MiniMaxGatewayHandler{
		minimaxService:        forwarder,
		gatewayService:        gateway,
		concurrencyHelper:     &fakeMiniMaxConcurrencyController{allowWait: true},
		billingCacheService:   &fakeMiniMaxBillingChecker{},
		maxAccountSwitches:    3,
		usageRecordWorkerPool: nil,
	}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, []int64{101, 202}, forwarder.accounts)
	require.Len(t, gateway.selectedExcluded, 2)
	_, excluded := gateway.selectedExcluded[1][101]
	require.True(t, excluded, "first account should be excluded on retry")
	require.Equal(t, []miniMaxDegradeCall{{accountID: 101, status: http.StatusTooManyRequests}}, gateway.degraded)
	require.NotNil(t, gateway.recorded)
	require.Equal(t, second, gateway.recorded.Account)
}

func TestMiniMaxGatewayHandlerMessagesFailoverStopsAfterMaxSwitches(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		errorType string
	}{
		{name: "overload", status: 529, errorType: "overloaded_error"},
		{name: "server_error", status: http.StatusInternalServerError, errorType: "server_error"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			first := &service.Account{ID: 101, Platform: service.PlatformMiniMax, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-1"}}
			gateway := &miniMaxFailoverGatewayService{
				selections: []*service.AccountSelectionResult{
					{Account: first, Acquired: true, ReleaseFunc: func() {}},
				},
			}
			forwarder := &miniMaxFailoverForwarder{
				errors: []error{&service.UpstreamFailoverError{StatusCode: tc.status, ResponseBody: []byte(`{"error":{"message":"retry later"}}`)}},
			}
			h := &MiniMaxGatewayHandler{
				minimaxService:      forwarder,
				gatewayService:      gateway,
				concurrencyHelper:   &fakeMiniMaxConcurrencyController{allowWait: true},
				billingCacheService: &fakeMiniMaxBillingChecker{},
				maxAccountSwitches:  0,
			}
			c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

			h.Messages(c)

			require.Equal(t, http.StatusBadGateway, rec.Code)
			require.Contains(t, rec.Body.String(), tc.errorType)
			require.Equal(t, []int64{101}, forwarder.accounts)
			require.Equal(t, []miniMaxDegradeCall{{accountID: 101, status: tc.status}}, gateway.degraded)
			require.Nil(t, gateway.recorded)
		})
	}
}

func TestMiniMaxGatewayHandlerMessagesAuthErrorDoesNotFailover(t *testing.T) {
	first := &service.Account{ID: 101, Platform: service.PlatformMiniMax, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-1"}}
	gateway := &miniMaxFailoverGatewayService{
		selections: []*service.AccountSelectionResult{
			{Account: first, Acquired: true, ReleaseFunc: func() {}},
		},
	}
	forwarder := &miniMaxFailoverForwarder{
		errors: []error{&service.UpstreamFailoverError{StatusCode: http.StatusUnauthorized, ResponseBody: []byte(`{"error":{"message":"bad key"}}`)}},
	}
	h := &MiniMaxGatewayHandler{
		minimaxService:      forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   &fakeMiniMaxConcurrencyController{allowWait: true},
		billingCacheService: &fakeMiniMaxBillingChecker{},
		maxAccountSwitches:  3,
	}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.Contains(t, rec.Body.String(), "upstream_auth_error")
	require.Equal(t, []int64{101}, forwarder.accounts)
	require.Empty(t, gateway.degraded)
	require.Nil(t, gateway.recorded)
}

func TestMiniMaxGatewayHandlerMessagesNonFailoverErrorKeepsExistingMapping(t *testing.T) {
	first := &service.Account{ID: 101, Platform: service.PlatformMiniMax, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-1"}}
	gateway := &miniMaxFailoverGatewayService{
		selections: []*service.AccountSelectionResult{
			{Account: first, Acquired: true, ReleaseFunc: func() {}},
		},
	}
	forwarder := &miniMaxFailoverForwarder{
		errors: []error{errors.New("minimax quota exhausted")},
	}
	h := &MiniMaxGatewayHandler{
		minimaxService:      forwarder,
		gatewayService:      gateway,
		concurrencyHelper:   &fakeMiniMaxConcurrencyController{allowWait: true},
		billingCacheService: &fakeMiniMaxBillingChecker{},
		maxAccountSwitches:  3,
	}
	c, rec, _ := newMiniMaxHandlerTestContext(t, service.PlatformMiniMax, `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hello"}]}`)

	h.Messages(c)

	require.Equal(t, http.StatusTooManyRequests, rec.Code)
	require.Contains(t, rec.Body.String(), "MiniMax quota exhausted")
	require.Equal(t, []int64{101}, forwarder.accounts)
	require.Empty(t, gateway.degraded)
}
