package urlvalidator

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateURLFormat(t *testing.T) {
	if _, err := ValidateURLFormat("", false); err == nil {
		t.Fatalf("expected empty url to fail")
	}
	if _, err := ValidateURLFormat("://bad", false); err == nil {
		t.Fatalf("expected invalid url to fail")
	}
	if _, err := ValidateURLFormat("http://example.com", false); err == nil {
		t.Fatalf("expected http to fail when allow_insecure_http is false")
	}
	if _, err := ValidateURLFormat("https://example.com", false); err != nil {
		t.Fatalf("expected https to pass, got %v", err)
	}
	if _, err := ValidateURLFormat("http://example.com", true); err != nil {
		t.Fatalf("expected http to pass when allow_insecure_http is true, got %v", err)
	}
	if _, err := ValidateURLFormat("https://example.com:bad", true); err == nil {
		t.Fatalf("expected invalid port to fail")
	}

	// 验证末尾斜杠被移除
	normalized, err := ValidateURLFormat("https://example.com/", false)
	if err != nil {
		t.Fatalf("expected trailing slash url to pass, got %v", err)
	}
	if normalized != "https://example.com" {
		t.Fatalf("expected trailing slash to be removed, got %s", normalized)
	}

	// 验证多个末尾斜杠被移除
	normalized, err = ValidateURLFormat("https://example.com///", false)
	if err != nil {
		t.Fatalf("expected multiple trailing slashes to pass, got %v", err)
	}
	if normalized != "https://example.com" {
		t.Fatalf("expected all trailing slashes to be removed, got %s", normalized)
	}

	// 验证带路径的 URL 末尾斜杠被移除
	normalized, err = ValidateURLFormat("https://example.com/api/v1/", false)
	if err != nil {
		t.Fatalf("expected trailing slash url with path to pass, got %v", err)
	}
	if normalized != "https://example.com/api/v1" {
		t.Fatalf("expected trailing slash to be removed from path, got %s", normalized)
	}
}

func TestValidateHTTPURL(t *testing.T) {
	if _, err := ValidateHTTPURL("http://example.com", false, ValidationOptions{}); err == nil {
		t.Fatalf("expected http to fail when allow_insecure_http is false")
	}
	if _, err := ValidateHTTPURL("http://example.com", true, ValidationOptions{}); err != nil {
		t.Fatalf("expected http to pass when allow_insecure_http is true, got %v", err)
	}
	if _, err := ValidateHTTPURL("https://example.com", false, ValidationOptions{RequireAllowlist: true}); err == nil {
		t.Fatalf("expected require allowlist to fail when empty")
	}
	if _, err := ValidateHTTPURL("https://example.com", false, ValidationOptions{AllowedHosts: []string{"api.example.com"}}); err == nil {
		t.Fatalf("expected host not in allowlist to fail")
	}
	if _, err := ValidateHTTPURL("https://api.example.com", false, ValidationOptions{AllowedHosts: []string{"api.example.com"}}); err != nil {
		t.Fatalf("expected allowlisted host to pass, got %v", err)
	}
	if _, err := ValidateHTTPURL("https://sub.api.example.com", false, ValidationOptions{AllowedHosts: []string{"*.example.com"}}); err != nil {
		t.Fatalf("expected wildcard allowlist to pass, got %v", err)
	}
	if _, err := ValidateHTTPURL("https://localhost", false, ValidationOptions{AllowPrivate: false}); err == nil {
		t.Fatalf("expected localhost to be blocked when allow_private_hosts is false")
	}
}

func TestValidateResolvedIPContextHonorsCallerCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := ValidateResolvedIPContext(ctx, "caller-cancellation-test.invalid")

	require.ErrorIs(t, err, context.Canceled)
}

func TestIsBlockedHost(t *testing.T) {
	for _, host := range []string{
		"localhost", "LOCALHOST", "foo.localhost", " 127.0.0.1 ",
		"127.0.0.1", "::1", "10.0.0.5", "172.16.0.1", "192.168.1.1",
		"169.254.169.254", "0.0.0.0", "::", "fe80::1", "fc00::1", "::ffff:127.0.0.1",
		// SEC-013 扩展：CGNAT / 保留段 / this-network / 基准测试段 / IETF 协议段
		"100.64.0.1", "100.127.255.254", "198.18.0.1", "198.19.255.1", "192.0.0.1", "240.0.0.1", "255.255.255.255", "0.1.2.3",
		// IPv4-mapped IPv6 形式
		"::ffff:10.0.0.1", "::ffff:100.64.0.1", "::ffff:169.254.169.254", "[::ffff:10.0.0.1]",
		// 云元数据与内网专用域
		"metadata.google.internal", "METADATA.GOOGLE.INTERNAL", "fd00:ec2::254",
		"foo.internal", "printer.local", "host.localdomain", "fe80::1%eth0",
	} {
		if !IsBlockedHost(host) {
			t.Fatalf("expected %q to be blocked", host)
		}
	}
	for _, host := range []string{
		"example.com", "cdn.example.com", "93.184.216.34", "8.8.8.8", "2606:4700:4700::1111", "",
		// 与被阻断网段相邻的公网地址必须继续放行
		"100.63.255.255", "100.128.0.0", "198.17.255.255", "198.20.0.0", "192.0.1.1", "239.255.255.255",
		"internal.example.com", "local.example.com", "metadata.example.com",
	} {
		if IsBlockedHost(host) {
			t.Fatalf("expected %q to be allowed", host)
		}
	}
}

func TestValidateHTTPURLBlocksExtendedPrivateRanges(t *testing.T) {
	blocked := []string{
		"http://100.64.0.1",
		"http://metadata.google.internal",
		"http://[::ffff:10.0.0.1]",
		"http://169.254.169.254",
		"http://169.254.169.254/latest/meta-data/",
		"http://[fd00:ec2::254]",
		"http://198.18.0.1:8080",
		"http://240.0.0.1",
		"http://0.0.0.1",
		"http://svc.internal",
		"http://nas.local",
	}
	for _, raw := range blocked {
		_, err := ValidateHTTPURL(raw, true, ValidationOptions{AllowPrivate: false})
		require.Error(t, err, raw)
	}
	// AllowPrivate 显式放行时保持旧行为（运营者可能确有私网上游）
	for _, raw := range blocked {
		_, err := ValidateHTTPURL(raw, true, ValidationOptions{AllowPrivate: true})
		require.NoError(t, err, raw)
	}
	for _, raw := range []string{
		"https://api.openai.com", "http://93.184.216.34", "https://[2606:4700:4700::1111]",
		"https://100.63.255.255", "https://internal.example.com",
	} {
		_, err := ValidateHTTPURL(raw, true, ValidationOptions{AllowPrivate: false})
		require.NoError(t, err, raw)
	}
}

func TestIsBlockedIP(t *testing.T) {
	require.True(t, IsBlockedIP(nil))
	require.True(t, IsBlockedIP(net.ParseIP("100.64.0.1")))
	require.True(t, IsBlockedIP(net.ParseIP("::ffff:100.64.0.1")))
	require.True(t, IsBlockedIP(net.ParseIP("fd00:ec2::254")))
	require.False(t, IsBlockedIP(net.ParseIP("1.1.1.1")))
	require.False(t, IsBlockedIP(net.ParseIP("2606:4700:4700::1111")))
}
