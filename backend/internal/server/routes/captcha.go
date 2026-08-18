package routes

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/Wei-Shaw/sub2api/internal/middleware"
)

const (
	defaultCaptchaChallengeRatePerMin = 20
	defaultCaptchaVerifyRatePerMin    = 60
)

// RegisterCaptchaRoutes 注册自建行为验证码的公开路由。
//
// 挑战生成涉及图像合成，是 CPU 密集操作，且这两个接口都免鉴权，
// 因此必须限流；限流器自身故障时 fail-close，与注册接口保持一致。
//
// 这里单独接收 cfg 而不是并入 RegisterAuthRoutes，是因为后者的签名里没有
// *config.Config，而限流阈值需要从配置读取。
func RegisterCaptchaRoutes(
	v1 *gin.RouterGroup,
	h *handler.Handlers,
	redisClient *redis.Client,
	cfg *config.Config,
) {
	if h == nil || h.Captcha == nil {
		return
	}

	rateLimiter := middleware.NewRateLimiter(redisClient)
	challengeRate := defaultCaptchaChallengeRatePerMin
	verifyRate := defaultCaptchaVerifyRatePerMin
	if cfg != nil {
		if cfg.GoCaptcha.ChallengeRatePerMin > 0 {
			challengeRate = cfg.GoCaptcha.ChallengeRatePerMin
		}
		if cfg.GoCaptcha.VerifyRatePerMin > 0 {
			verifyRate = cfg.GoCaptcha.VerifyRatePerMin
		}
	}

	captcha := v1.Group("/captcha")
	{
		captcha.POST("/challenge",
			rateLimiter.LimitWithOptions("captcha-challenge", challengeRate, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Captcha.CreateChallenge,
		)
		captcha.POST("/verify",
			rateLimiter.LimitWithOptions("captcha-verify", verifyRate, time.Minute, middleware.RateLimitOptions{
				FailureMode: middleware.RateLimitFailClose,
			}),
			h.Captcha.Verify,
		)
	}
}
