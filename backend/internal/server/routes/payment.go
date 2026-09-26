package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// RegisterPaymentRoutes registers all payment-related routes:
// user-facing endpoints, webhook endpoints, and admin endpoints.
func RegisterPaymentRoutes(
	v1 *gin.RouterGroup,
	paymentHandler *handler.PaymentHandler,
	webhookHandler *handler.PaymentWebhookHandler,
	adminPaymentHandler *admin.PaymentHandler,
	jwtAuth middleware.JWTAuthMiddleware,
	adminAuth middleware.AdminAuthMiddleware,
	auditLog middleware.AuditLogMiddleware,
	settingService *service.SettingService,
	panelRateLimiter *middleware.PanelRateLimiter,
) {
	// --- User-facing payment endpoints (authenticated) ---
	authenticated := v1.Group("/payment")
	authenticated.Use(gin.HandlerFunc(jwtAuth))
	authenticated.Use(middleware.BackendModeUserGuard(settingService))
	// 面板全局按用户限流
	authenticated.Use(panelRateLimiter.Global())
	{
		authenticated.GET("/config", paymentHandler.GetPaymentConfig)
		authenticated.GET("/checkout-info", paymentHandler.GetCheckoutInfo)
		authenticated.GET("/plans", paymentHandler.GetPlans)
		authenticated.GET("/limits", paymentHandler.GetLimits)

		orders := authenticated.Group("/orders")
		{
			orders.POST("", paymentHandler.CreateOrder)
			orders.POST("/verify", paymentHandler.VerifyOrder)
			orders.GET("/my", paymentHandler.GetMyOrders)
			orders.GET("/:id", paymentHandler.GetOrder)
			orders.POST("/:id/cancel", paymentHandler.CancelOrder)
			orders.POST("/:id/refund-request", paymentHandler.RequestRefund)
			orders.GET("/refund-eligible-providers", paymentHandler.GetRefundEligibleProviders)
		}
	}

	// --- Public payment endpoints (no auth) ---
	// Signed resume-token recovery is the preferred public lookup path.
	// The legacy anonymous out_trade_no verify endpoint remains available as a
	// persisted-state compatibility path for staggered upgrades.
	//
	// SEC-018：这两个端点匿名可达且每次都查库（verify 还能按 out_trade_no 枚举订单），
	// 必须带按客户端 IP 的兜底限流。PanelRateLimiter.PublicIP 走系统设置里的公开接口
	// RPM（Redis 异常 fail-open，反代内网地址跳过）。
	// TODO(SEC-018): /orders/verify 还应叠加一层固定阈值、Redis 故障 fail-close 的
	// internal/middleware.RateLimiter.LimitWithOptions("payment-public-verify", 30, time.Minute,
	// RateLimitOptions{FailureMode: RateLimitFailClose})，与 routes/auth.go 的注册/登录入口
	// 一致；这需要 RegisterPaymentRoutes 新增 *internal/middleware.RateLimiter（或 *redis.Client）
	// 参数并同步修改 server/router.go 调用点。
	// 请求体同样只包含 out_trade_no / resume_token，沿用匿名接口 64KiB 上限（SEC-012）。
	public := v1.Group("/payment/public")
	public.Use(publicBodyLimit(publicJSONBodyLimit)...)
	public.Use(panelRateLimiter.PublicIP())
	{
		public.POST("/orders/verify", paymentHandler.VerifyOrderPublic)
		public.POST("/orders/resolve", paymentHandler.ResolveOrderPublicByResumeToken)
	}

	// --- Webhook endpoints (no auth) ---
	webhook := v1.Group("/payment/webhook")
	{
		// EasyPay sends GET callbacks with query params
		webhook.GET("/easypay", webhookHandler.EasyPayNotify)
		webhook.POST("/easypay", webhookHandler.EasyPayNotify)
		webhook.POST("/alipay", webhookHandler.AlipayNotify)
		webhook.POST("/wxpay", webhookHandler.WxpayNotify)
		webhook.POST("/stripe", webhookHandler.StripeWebhook)
		webhook.POST("/airwallex", webhookHandler.AirwallexWebhook)
		webhook.POST("/wise", webhookHandler.WiseWebhook)
	}

	// --- Admin payment endpoints (admin auth) ---
	adminGroup := v1.Group("/admin/payment")
	adminGroup.Use(gin.HandlerFunc(adminAuth))
	adminGroup.Use(gin.HandlerFunc(auditLog))
	adminGroup.Use(middleware.AdminComplianceGuard(settingService))
	{
		// Dashboard
		adminGroup.GET("/dashboard", adminPaymentHandler.GetDashboard)

		// Config
		adminGroup.GET("/config", adminPaymentHandler.GetConfig)
		adminGroup.PUT("/config", adminPaymentHandler.UpdateConfig)

		// Orders
		adminOrders := adminGroup.Group("/orders")
		{
			adminOrders.GET("", adminPaymentHandler.ListOrders)
			adminOrders.GET("/:id", adminPaymentHandler.GetOrderDetail)
			adminOrders.POST("/:id/cancel", adminPaymentHandler.CancelOrder)
			adminOrders.POST("/:id/retry", adminPaymentHandler.RetryFulfillment)
			adminOrders.POST("/:id/refund", adminPaymentHandler.ProcessRefund)
			adminOrders.POST("/:id/manual-refund/confirm", adminPaymentHandler.ConfirmManualRefund)
			adminOrders.POST("/:id/refund/query", adminPaymentHandler.QueryAndFinalizeRefund)
		}

		// Subscription Plans
		plans := adminGroup.Group("/plans")
		{
			plans.GET("", adminPaymentHandler.ListPlans)
			plans.POST("", adminPaymentHandler.CreatePlan)
			plans.PUT("/:id", adminPaymentHandler.UpdatePlan)
			plans.DELETE("/:id", adminPaymentHandler.DeletePlan)
		}

		// Provider Instances
		providers := adminGroup.Group("/providers")
		{
			providers.GET("", adminPaymentHandler.ListProviders)
			providers.POST("", adminPaymentHandler.CreateProvider)
			providers.PUT("/:id", adminPaymentHandler.UpdateProvider)
			providers.DELETE("/:id", adminPaymentHandler.DeleteProvider)
		}
	}
}
