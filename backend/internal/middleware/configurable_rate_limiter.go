package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

// registrationRateLimitSettings 注册/验证码限流所需的设置读取接口
// （*service.SettingService 天然满足；抽象为接口便于单元测试）
type registrationRateLimitSettings interface {
	GetRegistrationRateLimitPerIP(ctx context.Context) int
	GetRegistrationRateLimitWindowIP(ctx context.Context) int
	GetRegistrationRateLimitPerEmail(ctx context.Context) int
	GetRegistrationRateLimitWindowEmail(ctx context.Context) int
	GetRegistrationRateLimitPerEmailDomain(ctx context.Context) int
	GetRegistrationRateLimitWindowEmailDomain(ctx context.Context) int
}

// ConfigurableRateLimiter 可配置的速率限制器
type ConfigurableRateLimiter struct {
	redis          *redis.Client
	settingService registrationRateLimitSettings
}

// NewConfigurableRateLimiter 创建可配置的速率限制器
func NewConfigurableRateLimiter(redis *redis.Client, settingService registrationRateLimitSettings) *ConfigurableRateLimiter {
	return &ConfigurableRateLimiter{
		redis:          redis,
		settingService: settingService,
	}
}

// checkAndIncrScript 单 key 限流：先检查是否已达上限，未达才计数（被拒绝的请求不计数），
// 仅在 key 首次创建或 TTL 丢失时设置过期时间（避免窗口被不断顺延）。
// 返回 {allowed(0/1)}
var checkAndIncrScript = redis.NewScript(`
local current = tonumber(redis.call('GET', KEYS[1]) or '0')
if current >= tonumber(ARGV[2]) then
  if redis.call('PTTL', KEYS[1]) == -1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
  return 0
end
current = redis.call('INCR', KEYS[1])
if current == 1 or redis.call('PTTL', KEYS[1]) == -1 then
  redis.call('PEXPIRE', KEYS[1], ARGV[1])
end
return 1
`)

// emailRateLimitScript 邮箱两层限流（域名 + 地址）原子检查：
// 任一层已达上限则拒绝且两层都不计数；两层都未达上限才同时计数。
// KEYS[1]=域名 key，KEYS[2]=地址 key
// ARGV[1]=域名窗口(ms) ARGV[2]=域名上限 ARGV[3]=地址窗口(ms) ARGV[4]=地址上限
// 返回 {allowed(0/1), blockedLayer("domain"/"email"/"")}
var emailRateLimitScript = redis.NewScript(`
local dcur = tonumber(redis.call('GET', KEYS[1]) or '0')
if dcur >= tonumber(ARGV[2]) then
  if redis.call('PTTL', KEYS[1]) == -1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
  return {0, 'domain'}
end
local ecur = tonumber(redis.call('GET', KEYS[2]) or '0')
if ecur >= tonumber(ARGV[4]) then
  if redis.call('PTTL', KEYS[2]) == -1 then redis.call('PEXPIRE', KEYS[2], ARGV[3]) end
  return {0, 'email'}
end
local d = redis.call('INCR', KEYS[1])
if d == 1 or redis.call('PTTL', KEYS[1]) == -1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
local e = redis.call('INCR', KEYS[2])
if e == 1 or redis.call('PTTL', KEYS[2]) == -1 then redis.call('PEXPIRE', KEYS[2], ARGV[3]) end
return {1, ''}
`)

// checkAndIncrRun 允许测试覆写单 key 限流脚本执行逻辑
var checkAndIncrRun = func(ctx context.Context, client *redis.Client, key string, windowMillis int64, limit int) (bool, error) {
	allowed, err := checkAndIncrScript.Run(ctx, client, []string{key}, windowMillis, limit).Int64()
	if err != nil {
		return false, err
	}
	return allowed == 1, nil
}

// emailRateLimitRun 允许测试覆写邮箱两层限流脚本执行逻辑
// 返回 blockedLayer：""（放行）/ "domain" / "email"
var emailRateLimitRun = func(ctx context.Context, client *redis.Client, domainKey, emailKey string, domainWindowMillis int64, domainLimit int, emailWindowMillis int64, emailLimit int) (string, error) {
	values, err := emailRateLimitScript.Run(ctx, client,
		[]string{domainKey, emailKey},
		domainWindowMillis, domainLimit, emailWindowMillis, emailLimit,
	).Slice()
	if err != nil {
		return "", err
	}
	if len(values) < 2 {
		return "", fmt.Errorf("email rate limit script returned %d values", len(values))
	}
	allowed, err := parseInt64(values[0])
	if err != nil {
		return "", err
	}
	if allowed == 1 {
		return "", nil
	}
	layer, ok := values[1].(string)
	if !ok {
		return "", fmt.Errorf("unexpected blocked layer type %T", values[1])
	}
	return layer, nil
}

// RegistrationRateLimit 注册接口的多层速率限制
// 1. 基于IP的速率限制
// 2. 基于邮箱的两层速率限制（域名高阈值反批量 + 单地址低阈值）
func (crl *ConfigurableRateLimiter) RegistrationRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if crl.settingService == nil {
			c.Next()
			return
		}
		ctx := c.Request.Context()

		// 1. IP 速率限制
		ipLimit := crl.settingService.GetRegistrationRateLimitPerIP(ctx)
		ipWindow := crl.settingService.GetRegistrationRateLimitWindowIP(ctx)
		clientIP := getClientIP(c)
		ipKey := fmt.Sprintf("rate:registration:ip:%s", clientIP)

		allowed, err := checkAndIncrRun(ctx, crl.redis, ipKey, windowTTLMillis(time.Duration(ipWindow)*time.Second), ipLimit)
		if err != nil {
			// Redis 故障时采用 fail-close 策略（拒绝请求）
			log.Printf("[RateLimit] redis error: key=%s mode=fail-close err=%v", ipKey, err)
			allowed = false
		}
		if !allowed {
			c.JSON(429, gin.H{
				"error": "注册请求过于频繁，请稍后再试",
				"code":  "RATE_LIMIT_EXCEEDED_IP",
			})
			c.Abort()
			return
		}

		// 2. 邮箱域名 + 邮箱地址两层速率限制
		if !crl.checkEmailRateLimit(c, "registration") {
			return
		}

		c.Next()
	}
}

// SendVerifyCodeRateLimit 发送验证码接口的速率限制（邮箱域名 + 邮箱地址两层）
func (crl *ConfigurableRateLimiter) SendVerifyCodeRateLimit() gin.HandlerFunc {
	return func(c *gin.Context) {
		if crl.settingService == nil {
			c.Next()
			return
		}
		if !crl.checkEmailRateLimit(c, "verify-code") {
			return
		}
		c.Next()
	}
}

// checkEmailRateLimit 对请求体中的邮箱执行两层限流（域名高阈值 + 地址低阈值）。
// 返回 false 表示已写入 429 响应并 Abort。
func (crl *ConfigurableRateLimiter) checkEmailRateLimit(c *gin.Context, scope string) bool {
	email := normalizeEmailAddress(peekEmailFromBody(c))
	if email == "" {
		return true
	}
	emailDomain := extractEmailDomain(email)
	if emailDomain == "" {
		return true
	}

	ctx := c.Request.Context()
	emailLimit := crl.settingService.GetRegistrationRateLimitPerEmail(ctx)
	emailWindow := crl.settingService.GetRegistrationRateLimitWindowEmail(ctx)
	domainLimit := crl.settingService.GetRegistrationRateLimitPerEmailDomain(ctx)
	domainWindow := crl.settingService.GetRegistrationRateLimitWindowEmailDomain(ctx)

	domainKey := fmt.Sprintf("rate:%s:email-domain:%s", scope, emailDomain)
	emailKey := fmt.Sprintf("rate:%s:email:%s", scope, email)

	blockedLayer, err := emailRateLimitRun(ctx, crl.redis,
		domainKey, emailKey,
		windowTTLMillis(time.Duration(domainWindow)*time.Second), domainLimit,
		windowTTLMillis(time.Duration(emailWindow)*time.Second), emailLimit,
	)
	if err != nil {
		// Redis 故障时采用 fail-close 策略（拒绝请求）
		log.Printf("[RateLimit] redis error: key=%s mode=fail-close err=%v", emailKey, err)
		blockedLayer = "email"
	}

	switch blockedLayer {
	case "domain":
		c.JSON(429, gin.H{
			"error": "该邮箱域名请求过于频繁，请稍后再试",
			"code":  "RATE_LIMIT_EXCEEDED_EMAIL_DOMAIN",
		})
		c.Abort()
		return false
	case "email":
		c.JSON(429, gin.H{
			"error": "该邮箱请求过于频繁，请稍后再试",
			"code":  "RATE_LIMIT_EXCEEDED_EMAIL",
		})
		c.Abort()
		return false
	}
	return true
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

// normalizeEmailAddress 归一化邮箱地址（小写+去空格），与用户表唯一性检查口径一致
func normalizeEmailAddress(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// extractEmailDomain 提取邮箱域名
func extractEmailDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) == 2 {
		return strings.ToLower(strings.TrimSpace(parts[1]))
	}
	return ""
}
