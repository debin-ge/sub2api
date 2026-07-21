package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// ConfigurableRateLimiter 可配置的速率限制器
type ConfigurableRateLimiter struct {
	redis           *redis.Client
	settingService  *service.SettingService
	baseRateLimiter *RateLimiter
}

// NewConfigurableRateLimiter 创建可配置的速率限制器
func NewConfigurableRateLimiter(redis *redis.Client, settingService *service.SettingService) *ConfigurableRateLimiter {
	return &ConfigurableRateLimiter{
		redis:           redis,
		settingService:  settingService,
		baseRateLimiter: NewRateLimiter(redis),
	}
}

// RegistrationRateLimit 注册接口的多层速率限制
// 1. 基于IP的速率限制
// 2. 基于邮箱域名的速率限制
func (crl *ConfigurableRateLimiter) RegistrationRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// 获取配置
		ipLimit := crl.settingService.GetRegistrationRateLimitPerIP(ctx)
		ipWindow := crl.settingService.GetRegistrationRateLimitWindowIP(ctx)
		emailLimit := crl.settingService.GetRegistrationRateLimitPerEmail(ctx)
		emailWindow := crl.settingService.GetRegistrationRateLimitWindowEmail(ctx)

		// 1. IP 速率限制
		clientIP := getClientIP(c)
		ipKey := fmt.Sprintf("rate:registration:ip:%s", clientIP)

		if !crl.checkRateLimit(ctx, ipKey, ipLimit, time.Duration(ipWindow)*time.Second) {
			c.JSON(429, gin.H{
				"error": "注册请求过于频繁，请稍后再试",
				"code":  "RATE_LIMIT_EXCEEDED_IP",
			})
			c.Abort()
			return
		}

		// 2. 邮箱域名速率限制（从请求体中提取，不消费请求体）
		email := peekEmailFromBody(c)
		if email != "" {
			emailDomain := extractEmailDomain(email)
			if emailDomain != "" {
				emailKey := fmt.Sprintf("rate:registration:email:%s", emailDomain)

				if !crl.checkRateLimit(ctx, emailKey, emailLimit, time.Duration(emailWindow)*time.Second) {
					c.JSON(429, gin.H{
						"error": "该邮箱域名注册请求过于频繁，请稍后再试",
						"code":  "RATE_LIMIT_EXCEEDED_EMAIL_DOMAIN",
					})
					c.Abort()
					return
				}
			}
		}

		c.Next()
	}
}

// SendVerifyCodeRateLimit 发送验证码接口的速率限制
func (crl *ConfigurableRateLimiter) SendVerifyCodeRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// 获取配置
		emailLimit := crl.settingService.GetRegistrationRateLimitPerEmail(ctx)
		emailWindow := crl.settingService.GetRegistrationRateLimitWindowEmail(ctx)

		// 邮箱域名速率限制（从请求体中提取，不消费请求体）
		email := peekEmailFromBody(c)
		if email != "" {
			emailDomain := extractEmailDomain(email)
			if emailDomain != "" {
				emailKey := fmt.Sprintf("rate:verify-code:email:%s", emailDomain)

				if !crl.checkRateLimit(ctx, emailKey, emailLimit, time.Duration(emailWindow)*time.Second) {
					c.JSON(429, gin.H{
						"error": "该邮箱域名验证码请求过于频繁，请稍后再试",
						"code":  "RATE_LIMIT_EXCEEDED_EMAIL_DOMAIN",
					})
					c.Abort()
					return
				}
			}
		}

		c.Next()
	}
}

// peekEmailFromBody 从请求体中读取 email 字段，同时恢复请求体供后续处理器使用。
// 返回空字符串表示解析失败或无 email 字段（不影响后续流程）。
func peekEmailFromBody(c *gin.Context) string {
	if c.Request.Body == nil {
		return ""
	}
	bodyBytes, err := io.ReadAll(c.Request.Body)
	if err != nil {
		return ""
	}
	// 恢复请求体，供后续处理器读取
	c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	if len(bodyBytes) == 0 {
		return ""
	}

	var req struct {
		Email string `json:"email"`
	}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		return ""
	}
	return strings.TrimSpace(req.Email)
}

// checkRateLimit 检查速率限制
func (crl *ConfigurableRateLimiter) checkRateLimit(ctx context.Context, key string, limit int, window time.Duration) bool {
	// 使用 Redis INCR + EXPIRE 实现滑动窗口
	pipe := crl.redis.Pipeline()
	incr := pipe.Incr(ctx, key)
	pipe.Expire(ctx, key, window)
	_, err := pipe.Exec(ctx)

	if err != nil {
		// Redis 故障时采用 fail-close 策略（拒绝请求）
		return false
	}

	count := incr.Val()
	return count <= int64(limit)
}

// getClientIP 获取客户端真实IP
func getClientIP(c *gin.Context) string {
	// 优先从 X-Forwarded-For 获取
	xff := c.GetHeader("X-Forwarded-For")
	if xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			ip := strings.TrimSpace(ips[0])
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}

	// 其次从 X-Real-IP 获取
	xri := c.GetHeader("X-Real-IP")
	if xri != "" {
		ip := strings.TrimSpace(xri)
		if net.ParseIP(ip) != nil {
			return ip
		}
	}

	// 最后使用 RemoteAddr
	ip, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return c.Request.RemoteAddr
	}
	return ip
}

// extractEmailDomain 提取邮箱域名
func extractEmailDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) == 2 {
		return strings.ToLower(strings.TrimSpace(parts[1]))
	}
	return ""
}
