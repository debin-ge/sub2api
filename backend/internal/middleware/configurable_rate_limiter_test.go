package middleware

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

type stubRateLimitSettings struct {
	perIP             int
	windowIP          int
	perEmail          int
	windowEmail       int
	perEmailDomain    int
	windowEmailDomain int
}

func (s *stubRateLimitSettings) GetRegistrationRateLimitPerIP(context.Context) int { return s.perIP }
func (s *stubRateLimitSettings) GetRegistrationRateLimitWindowIP(context.Context) int {
	return s.windowIP
}
func (s *stubRateLimitSettings) GetRegistrationRateLimitPerEmail(context.Context) int {
	return s.perEmail
}
func (s *stubRateLimitSettings) GetRegistrationRateLimitWindowEmail(context.Context) int {
	return s.windowEmail
}
func (s *stubRateLimitSettings) GetRegistrationRateLimitPerEmailDomain(context.Context) int {
	return s.perEmailDomain
}
func (s *stubRateLimitSettings) GetRegistrationRateLimitWindowEmailDomain(context.Context) int {
	return s.windowEmailDomain
}

func defaultStubSettings() *stubRateLimitSettings {
	return &stubRateLimitSettings{
		perIP:             10,
		windowIP:          3600,
		perEmail:          2,
		windowEmail:       3600,
		perEmailDomain:    100,
		windowEmailDomain: 3600,
	}
}

// fakeEmailRateLimitRun 用内存计数模拟 Lua 脚本的 check-then-increment 语义
func fakeEmailRateLimitRun(t *testing.T) map[string]int {
	t.Helper()
	counts := make(map[string]int)
	original := emailRateLimitRun
	emailRateLimitRun = func(_ context.Context, _ *redis.Client, domainKey, emailKey string, _ int64, domainLimit int, _ int64, emailLimit int) (string, error) {
		if counts[domainKey] >= domainLimit {
			return "domain", nil
		}
		if counts[emailKey] >= emailLimit {
			return "email", nil
		}
		counts[domainKey]++
		counts[emailKey]++
		return "", nil
	}
	t.Cleanup(func() { emailRateLimitRun = original })
	return counts
}

func fakeCheckAndIncrRun(t *testing.T) map[string]int {
	t.Helper()
	counts := make(map[string]int)
	original := checkAndIncrRun
	checkAndIncrRun = func(_ context.Context, _ *redis.Client, key string, _ int64, limit int) (bool, error) {
		if counts[key] >= limit {
			return false, nil
		}
		counts[key]++
		return true, nil
	}
	t.Cleanup(func() { checkAndIncrRun = original })
	return counts
}

func newVerifyCodeRouter(crl *ConfigurableRateLimiter, bodyOut *string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/send", crl.SendVerifyCodeRateLimit(), func(c *gin.Context) {
		if bodyOut != nil {
			data, _ := io.ReadAll(c.Request.Body)
			*bodyOut = string(data)
		}
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return router
}

func postJSON(router *gin.Engine, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "203.0.113.10:12345"
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestSendVerifyCodeRateLimitPerEmailAddress(t *testing.T) {
	fakeEmailRateLimitRun(t)
	crl := NewConfigurableRateLimiter(nil, defaultStubSettings())
	router := newVerifyCodeRouter(crl, nil)

	// 同一地址（大小写/空格归一化后）：前2次放行，第3次被地址层拒绝
	w := postJSON(router, "/send", `{"email":" Alice@Gmail.com "}`)
	require.Equal(t, http.StatusOK, w.Code)
	w = postJSON(router, "/send", `{"email":"alice@gmail.com"}`)
	require.Equal(t, http.StatusOK, w.Code)

	w = postJSON(router, "/send", `{"email":"alice@gmail.com"}`)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), `"code":"RATE_LIMIT_EXCEEDED_EMAIL"`)

	// 同域名的其他地址不受影响（不共享计数桶）
	w = postJSON(router, "/send", `{"email":"bob@gmail.com"}`)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestSendVerifyCodeRateLimitDomainThreshold(t *testing.T) {
	fakeEmailRateLimitRun(t)
	settings := defaultStubSettings()
	settings.perEmailDomain = 2
	settings.perEmail = 100
	crl := NewConfigurableRateLimiter(nil, settings)
	router := newVerifyCodeRouter(crl, nil)

	w := postJSON(router, "/send", `{"email":"a@gmail.com"}`)
	require.Equal(t, http.StatusOK, w.Code)
	w = postJSON(router, "/send", `{"email":"b@gmail.com"}`)
	require.Equal(t, http.StatusOK, w.Code)

	// 域名计数已达上限，第三个地址被域名层拒绝
	w = postJSON(router, "/send", `{"email":"c@gmail.com"}`)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), `"code":"RATE_LIMIT_EXCEEDED_EMAIL_DOMAIN"`)

	// 其他域名不受影响
	w = postJSON(router, "/send", `{"email":"a@qq.com"}`)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestSendVerifyCodeRateLimitFailClose(t *testing.T) {
	rdb := redis.NewClient(&redis.Options{
		Addr:         "127.0.0.1:1",
		DialTimeout:  50 * time.Millisecond,
		ReadTimeout:  50 * time.Millisecond,
		WriteTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() {
		_ = rdb.Close()
	})

	crl := NewConfigurableRateLimiter(rdb, defaultStubSettings())
	router := newVerifyCodeRouter(crl, nil)

	w := postJSON(router, "/send", `{"email":"alice@gmail.com"}`)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestSendVerifyCodeRateLimitNilSettingsPassThrough(t *testing.T) {
	crl := NewConfigurableRateLimiter(nil, nil)
	router := newVerifyCodeRouter(crl, nil)

	w := postJSON(router, "/send", `{"email":"alice@gmail.com"}`)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestSendVerifyCodeRateLimitNoEmailPassThrough(t *testing.T) {
	fakeEmailRateLimitRun(t)
	crl := NewConfigurableRateLimiter(nil, defaultStubSettings())
	router := newVerifyCodeRouter(crl, nil)

	w := postJSON(router, "/send", `{}`)
	require.Equal(t, http.StatusOK, w.Code)
	w = postJSON(router, "/send", `{"email":"not-an-email"}`)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestSendVerifyCodeRateLimitPreservesBody(t *testing.T) {
	fakeEmailRateLimitRun(t)
	crl := NewConfigurableRateLimiter(nil, defaultStubSettings())
	var handlerBody string
	router := newVerifyCodeRouter(crl, &handlerBody)

	body := `{"email":"alice@gmail.com","turnstile_token":"abc"}`
	w := postJSON(router, "/send", body)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, body, handlerBody)
}

func TestRegistrationRateLimitLayers(t *testing.T) {
	fakeCheckAndIncrRun(t)
	fakeEmailRateLimitRun(t)
	settings := defaultStubSettings()
	settings.perIP = 2
	settings.perEmail = 100
	crl := NewConfigurableRateLimiter(nil, settings)

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.POST("/register", crl.RegistrationRateLimit(), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	w := postJSON(router, "/register", `{"email":"a@gmail.com"}`)
	require.Equal(t, http.StatusOK, w.Code)
	w = postJSON(router, "/register", `{"email":"b@gmail.com"}`)
	require.Equal(t, http.StatusOK, w.Code)

	// 同一 IP 第3次注册被 IP 层拒绝
	w = postJSON(router, "/register", `{"email":"c@gmail.com"}`)
	require.Equal(t, http.StatusTooManyRequests, w.Code)
	require.Contains(t, w.Body.String(), `"code":"RATE_LIMIT_EXCEEDED_IP"`)
}
