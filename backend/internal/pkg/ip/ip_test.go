//go:build unit

package ip

import (
	"net"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// privatePeer 模拟「反代与应用同机 / 同 Docker 网络」的直连对端。
const privatePeer = "172.18.0.2:12345"

func TestGetTrustedClientIPUsesGinClientIP(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))

	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetTrustedClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "1.2.3.4")
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "9.9.9.9", w.Body.String())
}

func TestGetClientIPPreservesLegacyDockerForwardedHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "192.168.32.1:12345"
	req.Header.Set("X-Forwarded-For", "10.0.0.2, 203.0.113.42")
	req.Header.Set("X-Real-IP", "192.168.32.1")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "203.0.113.42", w.Body.String())
}

func TestCheckIPRestrictionWithCompiledRules(t *testing.T) {
	whitelist := CompileIPRules([]string{"10.0.0.0/8", "192.168.1.2"})
	blacklist := CompileIPRules([]string{"10.1.1.1"})

	allowed, reason := CheckIPRestrictionWithCompiledRules("10.2.3.4", whitelist, blacklist)
	require.True(t, allowed)
	require.Equal(t, "", reason)

	allowed, reason = CheckIPRestrictionWithCompiledRules("10.1.1.1", whitelist, blacklist)
	require.False(t, allowed)
	require.Equal(t, "access denied", reason)
}

func TestCheckIPRestrictionWithCompiledRules_InvalidWhitelistStillDenies(t *testing.T) {
	// 与旧实现保持一致：白名单有配置但全无效时，最终应拒绝访问。
	invalidWhitelist := CompileIPRules([]string{"not-a-valid-pattern"})
	allowed, reason := CheckIPRestrictionWithCompiledRules("8.8.8.8", invalidWhitelist, nil)
	require.False(t, allowed)
	require.Equal(t, "access denied", reason)
}

func TestGetSecurityClientIPSwitchEnabledUsesLegacyHeadersFromTrustedPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetSecurityClientIP(c, true))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = privatePeer
	req.Header.Set("X-Real-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, 200, w.Code)
	require.Equal(t, "1.2.3.4", w.Body.String())
}

// TestGetSecurityClientIPTrustedPeerGuard 覆盖 SEC-002 的核心语义：
// 只有可信直连对端发来的转发头才会被采纳；XFF 取最右侧不可信跳；
// CF-Connecting-IP 退到最后。
func TestGetSecurityClientIPTrustedPeerGuard(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name              string
		remoteAddr        string
		trustedProxies    []string
		proxiesConfigured bool
		requestHeaders    map[string]string
		// extraXFF 以 Header.Add 追加第二个 X-Forwarded-For 头（模拟多跳代理各写一行）。
		extraXFF string
		want     string
	}{
		{
			name:       "public peer forging CF-Connecting-IP resolves to the peer",
			remoteAddr: "9.9.9.9:12345",
			requestHeaders: map[string]string{
				"CF-Connecting-IP": "1.2.3.4",
			},
			want: "9.9.9.9",
		},
		{
			name:       "public peer forging XFF and X-Real-IP resolves to the peer",
			remoteAddr: "9.9.9.9:12345",
			requestHeaders: map[string]string{
				"X-Forwarded-For": "1.2.3.4",
				"X-Real-IP":       "1.2.3.4",
				"True-Client-IP":  "1.2.3.4",
			},
			want: "9.9.9.9",
		},
		{
			name:           "private peer with single XFF hop",
			remoteAddr:     privatePeer,
			requestHeaders: map[string]string{"X-Forwarded-For": "8.8.8.8"},
			want:           "8.8.8.8",
		},
		{
			name:           "private peer with nginx-appended XFF takes the rightmost untrusted hop",
			remoteAddr:     privatePeer,
			requestHeaders: map[string]string{"X-Forwarded-For": "1.1.1.1, 8.8.8.8"},
			want:           "8.8.8.8",
		},
		{
			name:       "XFF skips trailing private proxy hops",
			remoteAddr: privatePeer,
			requestHeaders: map[string]string{
				"X-Forwarded-For": "1.1.1.1, 8.8.8.8, 10.0.0.5, 127.0.0.1",
			},
			want: "8.8.8.8",
		},
		{
			name:           "multiple XFF headers are joined in order",
			remoteAddr:     privatePeer,
			requestHeaders: map[string]string{"X-Forwarded-For": "1.1.1.1"},
			extraXFF:       "8.8.8.8, 10.0.0.9",
			want:           "8.8.8.8",
		},
		{
			name:       "XFF beats X-Real-IP and CF-Connecting-IP",
			remoteAddr: privatePeer,
			requestHeaders: map[string]string{
				"X-Forwarded-For":  "8.8.8.8",
				"X-Real-IP":        "4.4.4.4",
				"CF-Connecting-IP": "1.1.1.1",
			},
			want: "8.8.8.8",
		},
		{
			name:       "X-Real-IP beats CF-Connecting-IP",
			remoteAddr: privatePeer,
			requestHeaders: map[string]string{
				"X-Real-IP":        "4.4.4.4",
				"CF-Connecting-IP": "1.1.1.1",
			},
			want: "4.4.4.4",
		},
		{
			name:           "CF-Connecting-IP is still honored from a trusted peer when nothing else exists",
			remoteAddr:     privatePeer,
			requestHeaders: map[string]string{"CF-Connecting-IP": "1.1.1.1"},
			want:           "1.1.1.1",
		},
		{
			name:           "all XFF hops private without configured proxies falls back to the peer",
			remoteAddr:     privatePeer,
			requestHeaders: map[string]string{"X-Forwarded-For": "192.168.1.30, 10.0.0.5"},
			want:           "172.18.0.2",
		},
		{
			name:              "configured trusted proxies exclude a private peer",
			remoteAddr:        privatePeer,
			trustedProxies:    []string{"10.0.0.0/8"},
			proxiesConfigured: true,
			requestHeaders:    map[string]string{"X-Forwarded-For": "8.8.8.8", "X-Real-IP": "8.8.8.8"},
			want:              "172.18.0.2",
		},
		{
			name:              "configured trusted proxies honor a matching peer",
			remoteAddr:        "10.1.1.1:443",
			trustedProxies:    []string{"10.0.0.0/8"},
			proxiesConfigured: true,
			requestHeaders:    map[string]string{"X-Forwarded-For": "203.0.113.7, 10.2.2.2"},
			want:              "203.0.113.7",
		},
		{
			name:              "configured trusted proxies keep a private LAN client hop",
			remoteAddr:        "10.1.1.1:443",
			trustedProxies:    []string{"10.0.0.0/8"},
			proxiesConfigured: true,
			requestHeaders:    map[string]string{"X-Forwarded-For": "203.0.113.7, 192.168.1.30"},
			want:              "192.168.1.30",
		},
		{
			name:              "single IP trusted proxy entry matches exactly",
			remoteAddr:        "127.0.0.1:443",
			trustedProxies:    []string{"127.0.0.1"},
			proxiesConfigured: true,
			requestHeaders:    map[string]string{"X-Real-IP": "8.8.8.8"},
			want:              "8.8.8.8",
		},
		{
			name:              "explicitly empty trusted proxies trust nobody",
			remoteAddr:        "127.0.0.1:443",
			trustedProxies:    []string{},
			proxiesConfigured: true,
			requestHeaders:    map[string]string{"X-Forwarded-For": "8.8.8.8"},
			want:              "127.0.0.1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			policy := NewTrustedProxyPolicy(test.trustedProxies, test.proxiesConfigured)
			r.GET("/t", func(c *gin.Context) {
				SetForwardedIPSettingsWithPolicy(c, true, nil, policy)
				c.String(200, GetSecurityClientIP(c, false))
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = test.remoteAddr
			for name, value := range test.requestHeaders {
				req.Header.Set(name, value)
			}
			if test.extraXFF != "" {
				req.Header.Add("X-Forwarded-For", test.extraXFF)
			}
			r.ServeHTTP(w, req)

			require.Equal(t, test.want, w.Body.String())
		})
	}
}

func TestGetSecurityClientIPCustomHeaderPrecedenceAndFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		trustForward   bool
		headers        []string
		requestHeaders map[string]string
		want           string
	}{
		{
			name:         "configured order precedes built-ins",
			trustForward: true,
			headers:      []string{"X-CDN-First", "X-CDN-Second"},
			requestHeaders: map[string]string{
				"X-CDN-First":      "198.51.100.10",
				"X-CDN-Second":     "203.0.113.20",
				"X-Forwarded-For":  "8.8.8.8",
				"CF-Connecting-IP": "8.8.8.8",
			},
			want: "198.51.100.10",
		},
		{
			name:         "comma candidates skip invalid and private values",
			trustForward: true,
			headers:      []string{"X-CDN-First", "X-CDN-Second"},
			requestHeaders: map[string]string{
				"X-CDN-First":  "not-an-ip, 10.0.0.8",
				"X-CDN-Second": "also-bad, 203.0.113.9",
			},
			want: "203.0.113.9",
		},
		{
			name:         "legacy public header wins over custom private fallback",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "10.0.0.8",
				"X-Real-IP": "1.2.3.4",
			},
			want: "1.2.3.4",
		},
		{
			name:         "custom private fallback retains configured precedence",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "10.0.0.8",
				"X-Real-IP": "192.168.1.4",
			},
			want: "10.0.0.8",
		},
		{
			name:         "invalid custom value continues to built-ins",
			trustForward: true,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":         "1.2.3.4:443",
				"CF-Connecting-IP": "4.4.4.4",
			},
			want: "4.4.4.4",
		},
		{
			name:         "invalid legacy values continue to a valid forwarded address",
			trustForward: true,
			requestHeaders: map[string]string{
				"CF-Connecting-IP": "unknown",
				"X-Real-IP":        "proxy.internal",
				"X-Forwarded-For":  "also-invalid, 203.0.113.50",
			},
			want: "203.0.113.50",
		},
		{
			name:         "all invalid legacy values fall back to the connection address",
			trustForward: true,
			requestHeaders: map[string]string{
				"CF-Connecting-IP": "unknown",
				"X-Real-IP":        "proxy.internal",
				"X-Forwarded-For":  "also-invalid",
			},
			want: "172.18.0.2",
		},
		{
			name:         "disabled mode ignores custom and legacy headers",
			trustForward: false,
			headers:      []string{"X-CDN-IP"},
			requestHeaders: map[string]string{
				"X-CDN-IP":  "1.2.3.4",
				"X-Real-IP": "4.4.4.4",
			},
			want: "172.18.0.2",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.GET("/t", func(c *gin.Context) {
				SetForwardedIPSettings(c, test.trustForward, test.headers)
				c.String(200, GetSecurityClientIP(c, !test.trustForward))
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = privatePeer
			for name, value := range test.requestHeaders {
				req.Header.Set(name, value)
			}
			r.ServeHTTP(w, req)

			require.Equal(t, test.want, w.Body.String())
		})
	}
}

// 开关关闭：完全交给 Gin 的 server.trusted_proxies 链。
func TestGetSecurityClientIPSwitchDisabledUsesConfiguredTrustedProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NoError(t, r.SetTrustedProxies([]string{"9.9.9.9"}))
	r.GET("/t", func(c *gin.Context) {
		SetLegacyForwardedIPTrust(c, false)
		c.String(200, GetSecurityClientIP(c, false))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "1.2.3.4", w.Body.String())
}

func TestGetSecurityClientIPSwitchDisabledIgnoresPrivatePeerHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetLegacyForwardedIPTrust(c, false)
		c.String(200, GetSecurityClientIP(c, true))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = privatePeer
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "172.18.0.2", w.Body.String())
}

func TestGetClientIPSwitchDisabledUsesTrustedProxyChain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		SetLegacyForwardedIPTrust(c, false)
		c.String(200, GetClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "9.9.9.9:12345"
	req.Header.Set("X-Real-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "9.9.9.9", w.Body.String())
}

// 未挂载 SessionBindingContext（无请求快照）且对端不在默认可信范围时，
// 仍尊重 Gin 侧显式配置的可信代理，不会比 Gin 更宽松也不会更严格。
func TestGetClientIPWithoutSnapshotDefersToGinForUntrustedPeer(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	require.NoError(t, r.SetTrustedProxies([]string{"104.23.251.120"}))
	r.GET("/t", func(c *gin.Context) {
		c.String(200, GetClientIP(c))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "104.23.251.120:443"
	req.Header.Set("X-Forwarded-For", "203.0.113.42")
	req.Header.Set("CF-Connecting-IP", "1.2.3.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "203.0.113.42", w.Body.String())
}

// 有请求快照（生产路径）时，可信对端守卫只看快照里的策略，不依赖 Gin 引擎的
// 可信代理配置——即使引擎保持 Gin 默认的「信任所有代理」，公网对端也无法伪造。
func TestGetSecurityClientIPGuardIgnoresGinDefaultTrustAll(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New() // 不调用 SetTrustedProxies：保持 Gin 默认 0.0.0.0/0 + ::/0
	r.GET("/t", func(c *gin.Context) {
		SetForwardedIPSettings(c, true, nil)
		c.String(200, GetSecurityClientIP(c, false))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = "203.0.113.10:5678"
	req.Header.Set("X-Forwarded-For", "198.51.100.2")
	req.Header.Set("X-Real-IP", "198.51.100.2")
	r.ServeHTTP(w, req)

	require.Equal(t, "203.0.113.10", w.Body.String())
}

func TestGetSecurityClientIPRequestSnapshotCopiesCustomHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	require.NoError(t, r.SetTrustedProxies(nil))
	r.GET("/t", func(c *gin.Context) {
		headers := []string{"X-Original-IP"}
		SetForwardedIPSettings(c, true, headers)
		headers[0] = "X-Mutated-IP"
		c.String(200, GetSecurityClientIP(c, false))
	})

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/t", nil)
	req.RemoteAddr = privatePeer
	req.Header.Set("X-Original-IP", "1.2.3.4")
	req.Header.Set("X-Mutated-IP", "4.4.4.4")
	r.ServeHTTP(w, req)

	require.Equal(t, "1.2.3.4", w.Body.String())
}

func TestGetSecurityClientIPRequestSnapshotOverridesLiveFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name          string
		requestTrust  bool
		fallbackTrust bool
		want          string
	}{
		{name: "captured secure mode wins", requestTrust: false, fallbackTrust: true, want: "172.18.0.2"},
		{name: "captured compatibility mode wins", requestTrust: true, fallbackTrust: false, want: "1.2.3.4"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			r := gin.New()
			require.NoError(t, r.SetTrustedProxies(nil))
			r.GET("/t", func(c *gin.Context) {
				SetLegacyForwardedIPTrust(c, test.requestTrust)
				c.String(200, GetSecurityClientIP(c, test.fallbackTrust))
			})

			w := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/t", nil)
			req.RemoteAddr = privatePeer
			req.Header.Set("X-Real-IP", "1.2.3.4")
			r.ServeHTTP(w, req)

			require.Equal(t, test.want, w.Body.String())
		})
	}
}

func TestTrustedProxyPolicyTrusts(t *testing.T) {
	parse := func(s string) net.IP { return parseValidIP(s) }

	var nilPolicy *TrustedProxyPolicy
	require.False(t, nilPolicy.Configured())
	for _, addr := range []string{"127.0.0.1", "::1", "10.1.2.3", "172.31.255.1", "192.168.0.1", "fd00::1", "169.254.1.1", "fe80::1"} {
		require.True(t, nilPolicy.trusts(parse(addr)), addr)
	}
	for _, addr := range []string{"9.9.9.9", "203.0.113.1", "2001:db8::1", "0.0.0.0"} {
		require.False(t, nilPolicy.trusts(parse(addr)), addr)
	}
	require.False(t, nilPolicy.trusts(nil))

	configured := NewTrustedProxyPolicy([]string{"10.0.0.0/8", "203.0.113.9", "garbage"}, true)
	require.True(t, configured.Configured())
	require.True(t, configured.trusts(parse("10.9.9.9")))
	require.True(t, configured.trusts(parse("203.0.113.9")))
	require.False(t, configured.trusts(parse("127.0.0.1")), "explicit configuration replaces the private default")
	require.False(t, configured.trusts(parse("172.18.0.2")))

	empty := NewTrustedProxyPolicy(nil, true)
	require.True(t, empty.Configured())
	require.False(t, empty.trusts(parse("127.0.0.1")))
}
