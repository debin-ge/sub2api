package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterPaymentRoutesIncludesWiseWebhook(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	RegisterPaymentRoutes(
		router.Group("/api/v1"),
		&handler.PaymentHandler{},
		&handler.PaymentWebhookHandler{},
		admin.NewPaymentHandler(nil, nil),
		middleware.JWTAuthMiddleware(func(c *gin.Context) { c.Next() }),
		middleware.AdminAuthMiddleware(func(c *gin.Context) { c.Next() }),
		nil,
	)

	for _, route := range router.Routes() {
		if route.Method == "POST" && route.Path == "/api/v1/payment/webhook/wise" {
			return
		}
	}
	require.Fail(t, "POST /api/v1/payment/webhook/wise route was not registered")
}
