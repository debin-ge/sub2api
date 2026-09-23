//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Claude Code bursts /v1/messages/count_tokens; each call used to take an
// account slot that was never released and lingered until the slot TTL,
// inflating account concurrency and starving real requests.
func TestOpenAICompatibleCountTokensSkipsSlots(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(79)
	accounts := []service.Account{{ID: 1, Platform: service.PlatformDeepSeek, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 2,
		GroupIDs: []int64{groupID}, Credentials: map[string]any{"api_key": "test-key"}}}
	slots := &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}
	concurrency := service.NewConcurrencyService(slots)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	repo := openAIImagesFailoverAccountRepo{accounts: accounts}
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, &grokMediaSlotBindings{}, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	h := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)

	for _, full := range []bool{false, true} {
		slots.full = full
		// 远超账号并发上限（2），旧实现第 3 个请求起就会被泄漏的槽挤满。
		for range 10 {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens",
				strings.NewReader(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"}]}`)).WithContext(context.Background())
			c.Request.Header.Set("Content-Type", "application/json")
			c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 20, UserID: 10, GroupID: &groupID,
				Group: &service.Group{ID: groupID, Platform: service.PlatformDeepSeek}, User: &service.User{ID: 10}})
			c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 10, Concurrency: 1})
			h.CountTokens(c)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), "input_tokens")
			slots.assertReleased(t)
		}
	}
	require.Zero(t, slots.acquired, "count_tokens must not take account slots")
	require.Zero(t, slots.userAcquired, "count_tokens must not take user slots")
}
