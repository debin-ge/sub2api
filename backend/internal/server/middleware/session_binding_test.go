//go:build unit

package middleware

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSessionBindingContextFollowsForwardedIPSwitch(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name           string
		trustForwarded bool
		trustedProxies []string
		wantIP         string
	}{
		{name: "enabled switch takes over raw headers", trustForwarded: true, wantIP: "1.2.3.4"},
		{name: "disabled switch ignores untrusted headers", trustForwarded: false, wantIP: "127.0.0.1"},
		{name: "disabled switch uses configured Gin proxy", trustForwarded: false, trustedProxies: []string{"127.0.0.1"}, wantIP: "1.2.3.4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.SetTrustForwardedIPForAPIKeyACL(tc.trustForwarded)

			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(tc.trustedProxies))
			r.Use(SessionBindingContext(cfg))
			r.GET("/t", func(c *gin.Context) {
				binding := service.SessionBindingFromContext(c.Request.Context())
				require.NotNil(t, binding)
				require.Equal(t, tc.wantIP, binding.IP)
				require.Equal(t, "test-agent", binding.UserAgent)
				require.Equal(t, tc.wantIP, SecurityClientIP(c))
				c.Status(200)
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = "127.0.0.1:54321"
			req.Header.Set("X-Real-IP", "1.2.3.4")
			req.Header.Set("User-Agent", "test-agent")
			r.ServeHTTP(w, req)

			require.Equal(t, 200, w.Code)
		})
	}
}

func TestSessionBindingContextSnapshotsForwardedModeAndHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.SetForwardedClientIPSettings(true, []string{"X-Initial-IP"})

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.Use(SessionBindingContext(cfg))
	r.GET("/t", func(c *gin.Context) {
		binding := service.SessionBindingFromContext(c.Request.Context())
		require.NotNil(t, binding)
		require.Equal(t, "1.2.3.4", binding.IP)

		cfg.SetForwardedClientIPSettings(false, []string{"X-Changed-IP"})
		require.Equal(t, "1.2.3.4", ip.GetSecurityClientIP(c, false))
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	// 反代与应用同 Docker 网络：直连对端为内网地址，默认可信
	req.RemoteAddr = "172.18.0.2:12345"
	req.Header.Set("X-Initial-IP", "1.2.3.4")
	req.Header.Set("X-Changed-IP", "4.4.4.4")
	req.Header.Set("X-Real-IP", "8.8.8.8")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	runtimeSettings := cfg.ForwardedClientIPSettings()
	require.False(t, runtimeSettings.TrustForwardedIP)
	require.Equal(t, []string{"X-Changed-IP"}, runtimeSettings.Headers)
}

// SEC-002：兼容模式下只有可信直连对端发来的转发头才会被采纳。
// 未配置 server.trusted_proxies 时默认信任内网/回环对端；配置后仅信任列表。
func TestSessionBindingContextAppliesTrustedProxyPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name           string
		trustedProxies []string
		configured     bool
		remoteAddr     string
		wantIP         string
	}{
		{name: "unconfigured: public peer cannot forge headers", remoteAddr: "9.9.9.9:12345", wantIP: "9.9.9.9"},
		{name: "unconfigured: private peer headers are honored", remoteAddr: "172.18.0.2:12345", wantIP: "1.2.3.4"},
		{name: "configured: peer outside the list is untrusted", trustedProxies: []string{"10.0.0.0/8"}, configured: true, remoteAddr: "172.18.0.2:12345", wantIP: "172.18.0.2"},
		{name: "configured: peer inside the list is trusted", trustedProxies: []string{"10.0.0.0/8"}, configured: true, remoteAddr: "10.0.0.5:12345", wantIP: "1.2.3.4"},
		{name: "configured: explicit empty list trusts nobody", trustedProxies: []string{}, configured: true, remoteAddr: "127.0.0.1:12345", wantIP: "127.0.0.1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{Server: config.ServerConfig{
				TrustedProxies:           tc.trustedProxies,
				TrustedProxiesConfigured: tc.configured,
			}}
			cfg.SetTrustForwardedIPForAPIKeyACL(true)

			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.Use(SessionBindingContext(cfg))
			r.GET("/t", func(c *gin.Context) {
				binding := service.SessionBindingFromContext(c.Request.Context())
				require.NotNil(t, binding)
				require.Equal(t, tc.wantIP, binding.IP)
				require.Equal(t, tc.wantIP, SecurityClientIP(c))
				require.Equal(t, tc.wantIP, ip.GetClientIP(c), "logs and security paths must agree")
				c.Status(200)
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = tc.remoteAddr
			req.Header.Set("X-Forwarded-For", "1.2.3.4")
			req.Header.Set("X-Real-IP", "1.2.3.4")
			req.Header.Set("CF-Connecting-IP", "1.2.3.4")
			r.ServeHTTP(w, req)

			require.Equal(t, 200, w.Code)
		})
	}
}

func TestSessionBindingContextBoundsPersistedUserAgent(t *testing.T) {
	cfg := &config.Config{}
	r := gin.New()
	r.Use(SessionBindingContext(cfg))
	r.GET("/t", func(c *gin.Context) {
		binding := service.SessionBindingFromContext(c.Request.Context())
		require.Len(t, binding.UserAgent, maxPersistentUserAgentBytes)
		require.Equal(t, binding.UserAgent, c.Request.UserAgent())
		c.Status(200)
	})
	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.Header.Set("User-Agent", strings.Repeat("u", 2048))
	r.ServeHTTP(w, req)
	require.Equal(t, 200, w.Code)
}

// 未经过 SessionBindingContext 注入时（异常挂载顺序/单测直调），回退 trusted_proxies 链，
// 等价于开关关闭时的历史行为。
func TestSecurityClientIPFallsBackWithoutInjectedBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		c.String(200, SecurityClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "9.9.9.9", w.Body.String())
}

func TestRequestSessionBindingPrefersInjectedBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)

	cfg := &config.Config{}
	cfg.SetTrustForwardedIPForAPIKeyACL(true)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies([]string{"127.0.0.1"}))
	r.Use(SessionBindingContext(cfg))
	r.GET("/t", func(c *gin.Context) {
		issued := &service.SessionBinding{IP: "1.2.3.4", UserAgent: "test-agent"}
		require.Equal(t, issued.Hash(), requestSessionBinding(c).Hash())
		c.Status(200)
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "127.0.0.1:54321"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	req.Header.Set("User-Agent", "test-agent")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
}
