//go:build unit

package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// countTokensUserRPMCache 是 service.UserRPMCache 的内存实现，只记 (user, group) 桶。
type countTokensUserRPMCache struct {
	counts map[string]int
}

func (c *countTokensUserRPMCache) key(userID, groupID int64) string {
	return "ug:" + strconv.FormatInt(userID, 10) + ":" + strconv.FormatInt(groupID, 10)
}

func (c *countTokensUserRPMCache) IncrementUserGroupRPM(_ context.Context, userID, groupID int64) (int, error) {
	if c.counts == nil {
		c.counts = map[string]int{}
	}
	k := c.key(userID, groupID)
	c.counts[k]++
	return c.counts[k], nil
}

func (c *countTokensUserRPMCache) IncrementUserRPM(_ context.Context, _ int64) (int, error) {
	return 1, nil
}

func (c *countTokensUserRPMCache) GetUserGroupRPM(_ context.Context, userID, groupID int64) (int, error) {
	return c.counts[c.key(userID, groupID)], nil
}

func (c *countTokensUserRPMCache) GetUserRPM(_ context.Context, _ int64) (int, error) {
	return 0, nil
}

// SEC-009：count_tokens 零计费、不占并发槽，须由 gateway.token_count_rpm 单独限速并映射为
// 429 + Retry-After；简易模式跳过计费校验但不能跳过这道闸门。
func TestOpenAICompatibleCountTokensEnforcesTokenCountRPM(t *testing.T) {
	gin.SetMode(gin.TestMode)
	groupID := int64(79)
	accounts := []service.Account{{ID: 1, Platform: service.PlatformDeepSeek, Type: service.AccountTypeAPIKey,
		Status: service.StatusActive, Schedulable: true, Concurrency: 2,
		GroupIDs: []int64{groupID}, Credentials: map[string]any{"api_key": "test-key"}}}
	slots := &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}
	concurrency := service.NewConcurrencyService(slots)
	cfg := &config.Config{RunMode: config.RunModeSimple}
	cfg.Gateway.TokenCountRPM = 2
	repo := openAIImagesFailoverAccountRepo{accounts: accounts}
	gateway := service.NewOpenAIGatewayService(repo, nil, nil, nil, nil, nil, &grokMediaSlotBindings{}, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	billing := service.NewBillingCacheService(nil, nil, nil, nil, &countTokensUserRPMCache{}, nil, cfg, nil)
	t.Cleanup(billing.Stop)
	h := NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)

	call := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages/count_tokens",
			strings.NewReader(`{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello"}]}`)).WithContext(context.Background())
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{ID: 20, UserID: 10, GroupID: &groupID,
			Group: &service.Group{ID: groupID, Platform: service.PlatformDeepSeek}, User: &service.User{ID: 10}})
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 10, Concurrency: 1})
		h.CountTokens(c)
		return w
	}

	for i := 0; i < 2; i++ {
		w := call()
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}
	w := call()
	require.Equal(t, http.StatusTooManyRequests, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "rate_limit_exceeded")
	require.Contains(t, w.Body.String(), "token counting requests-per-minute limit exceeded")
	require.NotEmpty(t, w.Header().Get("Retry-After"))
	slots.assertReleased(t)
}
