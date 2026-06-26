package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayHandlerBalanceReturnsAuthenticatedUserBalance(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &GatewayHandler{}
	router := gin.New()
	router.GET("/v1/balance", func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
			ID: 9,
			User: &service.User{
				ID:      42,
				Balance: 12.34,
			},
		})
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 42})
		h.Balance(c)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/balance", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Balance float64 `json:"balance"`
		UserID  int64   `json:"user_id"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, int64(42), body.UserID)
	require.InDelta(t, 12.34, body.Balance, 0.000001)
}

func TestGatewayHandlerBalanceRejectsMissingAPIKeyContext(t *testing.T) {
	gin.SetMode(gin.TestMode)

	h := &GatewayHandler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/balance", nil)

	h.Balance(c)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}
