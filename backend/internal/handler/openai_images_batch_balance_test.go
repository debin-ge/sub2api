//go:build unit

package handler

import (
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

// SEC-011：/v1/images 的 n 受 images_max_n 约束（400），且 n>1 时须在转发前确认
// 余额能覆盖 n 倍单价（402）；n=1 保持既有准入行为。
func TestOpenAIGatewayHandlerImages_BatchCountCapAndBalancePrecheck(t *testing.T) {
	gin.SetMode(gin.TestMode)

	const unit = 0.04
	unitPrice := unit
	groupID := int64(24)
	userID := int64(333)

	newHandler := func(t *testing.T, balance float64) *OpenAIGatewayHandler {
		t.Helper()
		cfg := &config.Config{}
		cfg.Gateway.ImagesMaxN = 10
		cfg.Default.RateMultiplier = 1
		slots := &grokMediaSlotsCache{accounts: map[string]int64{}, users: map[string]int64{}}
		concurrency := service.NewConcurrencyService(slots)
		gateway := service.NewOpenAIGatewayService(
			openAIImagesFailoverAccountRepo{}, nil, nil, nil, nil, nil, &grokMediaSlotBindings{}, cfg, nil,
			concurrency, service.NewBillingService(cfg, nil), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		)
		userRepo := &openAIWSUsageHandlerUserRepoStub{user: service.User{ID: userID, Balance: balance, Concurrency: 1}}
		billing := service.NewBillingCacheService(nil, userRepo, nil, nil, nil, nil, cfg, nil)
		t.Cleanup(billing.Stop)
		return NewOpenAIGatewayHandler(gateway, concurrency, billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
	}

	call := func(t *testing.T, h *OpenAIGatewayHandler, balance float64, body string) *httptest.ResponseRecorder {
		t.Helper()
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID:      222,
			UserID:  userID,
			GroupID: &groupID,
			Group: &service.Group{
				ID:                   groupID,
				Platform:             service.PlatformOpenAI,
				Status:               service.StatusActive,
				AllowImageGeneration: true,
				RateMultiplier:       1,
				ImagePrice1K:         &unitPrice,
				ImagePrice2K:         &unitPrice,
				ImagePrice4K:         &unitPrice,
			},
			User: &service.User{ID: userID, Balance: balance, Concurrency: 1},
		})
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: userID, Concurrency: 1})
		h.Images(c)
		return rec
	}

	t.Run("n above cap is rejected with 400", func(t *testing.T) {
		h := newHandler(t, 100)
		rec := call(t, h, 100, `{"model":"gpt-image-2","prompt":"draw","size":"1024x1024","n":11}`)
		require.Equal(t, http.StatusBadRequest, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "n must be between 1 and 10")
	})

	t.Run("n=10 with balance of 1.5 units is rejected with 402", func(t *testing.T) {
		balance := 1.5 * unit
		h := newHandler(t, balance)
		rec := call(t, h, balance, `{"model":"gpt-image-2","prompt":"draw","size":"1024x1024","n":10}`)
		require.Equal(t, http.StatusPaymentRequired, rec.Code, rec.Body.String())
		require.Contains(t, rec.Body.String(), "insufficient balance")
	})

	t.Run("n=10 with sufficient balance passes the precheck", func(t *testing.T) {
		balance := 20 * unit
		h := newHandler(t, balance)
		rec := call(t, h, balance, `{"model":"gpt-image-2","prompt":"draw","size":"1024x1024","n":10}`)
		require.NotEqual(t, http.StatusPaymentRequired, rec.Code, rec.Body.String())
		require.NotEqual(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})

	t.Run("n=1 with balance of 1.5 units is unaffected", func(t *testing.T) {
		balance := 1.5 * unit
		h := newHandler(t, balance)
		rec := call(t, h, balance, `{"model":"gpt-image-2","prompt":"draw","size":"1024x1024","n":1}`)
		require.NotEqual(t, http.StatusPaymentRequired, rec.Code, rec.Body.String())
		require.NotEqual(t, http.StatusBadRequest, rec.Code, rec.Body.String())
	})
}
