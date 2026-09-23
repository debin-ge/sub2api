//go:build unit

package handler

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Gemini :countTokens only selects and forwards, so it must neither take nor
// wait for user/account slots; generateContent keeps the normal slot flow.
func TestGeminiCountTokensSkipsSlots(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(9200)
	group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformGemini, Status: service.StatusActive}
	account := &service.Account{
		ID: 9201, Platform: service.PlatformGemini, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 1,
		Credentials:   map[string]any{"api_key": "test-key"},
		AccountGroups: []service.AccountGroup{{AccountID: 9201, GroupID: groupID}},
	}
	cfg := &config.Config{RunMode: config.RunModeSimple}
	slots := &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}
	concurrency := service.NewConcurrencyService(slots)
	schedulerSnapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: []*service.Account{account}}, nil, nil, nil, nil)
	gatewayService := service.NewGatewayService(
		nil, &fakeGroupRepo{group: group}, nil, nil, nil, nil, nil, nil, cfg,
		schedulerSnapshot, concurrency, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
	)
	var upstreamActions []string
	upstream := &grokMediaSlotUpstream{call: func(req *http.Request, _ int64) (*http.Response, error) {
		upstreamActions = append(upstreamActions, req.URL.Path)
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{"totalTokens":3}`))}, nil
	}}
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	h := &GatewayHandler{
		gatewayService:           gatewayService,
		geminiCompatService:      service.NewGeminiMessagesCompatService(nil, nil, nil, schedulerSnapshot, nil, nil, upstream, nil, gatewayService, cfg),
		billingCacheService:      billing,
		concurrencyHelper:        NewConcurrencyHelper(concurrency, SSEPingFormatNone, 0),
		maxAccountSwitchesGemini: 1,
		cfg:                      cfg,
	}
	apiKey := &service.APIKey{ID: 9202, UserID: 9203, GroupID: &groupID, Group: group, Status: service.StatusActive,
		User: &service.User{ID: 9203, Concurrency: 1, Balance: 100}}

	call := func(action string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		ctx := context.WithValue(context.Background(), ctxkey.Group, group)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-3.1-pro:"+action,
			strings.NewReader(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}]}`)).WithContext(ctx)
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "modelAction", Value: "/gemini-3.1-pro:" + action}}
		c.Set(string(middleware.ContextKeyAPIKey), apiKey)
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: apiKey.UserID, Concurrency: 1})
		h.GeminiV1BetaModels(c)
		return w
	}

	// 槽位全满时 countTokens 仍应直接转发，而不是排队或 429。
	slots.full = true
	for range 5 {
		w := call("countTokens")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		slots.assertReleased(t)
	}
	require.Zero(t, slots.acquired, "countTokens must not take account slots")
	require.Zero(t, slots.userAcquired, "countTokens must not take user slots")
	require.Len(t, upstreamActions, 5)
	for _, path := range upstreamActions {
		require.True(t, strings.HasSuffix(path, ":countTokens"), path)
	}

	slots.full = false
	w := call("generateContent")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	slots.assertReleased(t)
	require.Equal(t, 1, slots.acquired, "generateContent keeps taking an account slot")
	require.Equal(t, 1, slots.userAcquired, "generateContent keeps taking a user slot")
}
