package urlvalidator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ValidationOptions struct {
	AllowedHosts     []string
	RequireAllowlist bool
	AllowPrivate     bool
}

// ValidateHTTPURL validates an outbound HTTP/HTTPS URL.
//
// It provides a single validation entry point that supports:
// - scheme 校验（https 或可选允许 http）
// - 可选 allowlist（支持 *.example.com 通配）
// - allow_private_hosts 策略（阻断 localhost/私网字面量 IP）
//
// 注意：DNS Rebinding 防护（解析后 IP 校验）应在实际发起请求时执行，避免 TOCTOU。
func ValidateHTTPURL(raw string, allowInsecureHTTP bool, opts ValidationOptions) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("url is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid url: %s", trimmed)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && (!allowInsecureHTTP || scheme != "http") {
		return "", fmt.Errorf("invalid url scheme: %s", parsed.Scheme)
	}

	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return "", errors.New("invalid host")
	}
	if !opts.AllowPrivate && isBlockedHost(host) {
		return "", fmt.Errorf("host is not allowed: %s", host)
	}

	if port := parsed.Port(); port != "" {
		num, err := strconv.Atoi(port)
		if err != nil || num <= 0 || num > 65535 {
			return "", fmt.Errorf("invalid port: %s", port)
		}
	}

	allowlist := normalizeAllowlist(opts.AllowedHosts)
	if opts.RequireAllowlist && len(allowlist) == 0 {
		return "", errors.New("allowlist is not configured")
	}
	if len(allowlist) > 0 && !isAllowedHost(host, allowlist) {
		return "", fmt.Errorf("host is not allowed: %s", host)
	}

	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return strings.TrimRight(parsed.String(), "/"), nil
}

func ValidateURLFormat(raw string, allowInsecureHTTP bool) (string, error) {
	// 最小格式校验：仅保证 URL 可解析且 scheme 合规，不做白名单/私网/SSRF 校验
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("url is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("invalid url: %s", trimmed)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && (!allowInsecureHTTP || scheme != "http") {
		return "", fmt.Errorf("invalid url scheme: %s", parsed.Scheme)
	}

	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return "", errors.New("invalid host")
	}

	if port := parsed.Port(); port != "" {
		num, err := strconv.Atoi(port)
		if err != nil || num <= 0 || num > 65535 {
			return "", fmt.Errorf("invalid port: %s", port)
		}
	}

	return strings.TrimRight(trimmed, "/"), nil
}

func ValidateHTTPSURL(raw string, opts ValidationOptions) (string, error) {
	return ValidateHTTPURL(raw, false, opts)
}

// ValidateResolvedIP 验证 DNS 解析后的 IP 地址是否安全
// 用于防止 DNS Rebinding 攻击：在实际 HTTP 请求时调用此函数验证解析后的 IP
func ValidateResolvedIP(host string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return ValidateResolvedIPContext(ctx, host)
}

// ValidateResolvedIPContext validates resolved addresses while honoring the
// caller's request cancellation and deadline. ValidateResolvedIP remains the
// compatibility wrapper for callers without a context.
func ValidateResolvedIPContext(ctx context.Context, host string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("dns resolution failed: %w", err)
	}
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("dns resolution failed: %w", err)
	}

	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("resolved ip %s is not allowed", ip.String())
		}
	}
	return nil
}

func normalizeAllowlist(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(values))
	for _, v := range values {
		entry := strings.ToLower(strings.TrimSpace(v))
		if entry == "" {
			continue
		}
		if host, _, err := net.SplitHostPort(entry); err == nil {
			entry = host
		}
		normalized = append(normalized, entry)
	}
	return normalized
}

func isAllowedHost(host string, allowlist []string) bool {
	for _, entry := range allowlist {
		if entry == "" {
			continue
		}
		if strings.HasPrefix(entry, "*.") {
			suffix := strings.TrimPrefix(entry, "*.")
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				return true
			}
			continue
		}
		if host == entry {
			return true
		}
	}
	return false
}

// ErrBlockedHost 标识目标主机/IP 被 SSRF 策略拒绝（字面量或解析结果落在私网、回环、
// 链路本地、云元数据等禁止范围）。调用方可用 errors.Is 区分“策略拒绝”与普通网络错误。
var ErrBlockedHost = errors.New("host is blocked by SSRF policy")

// blockedHostnames 是不经解析即拒绝的主机名（小写比较）：云元数据服务的固定域名。
var blockedHostnames = map[string]struct{}{
	"localhost":                {},
	"metadata.google.internal": {},
}

// blockedHostSuffixes 是不经解析即拒绝的主机名后缀：只在本机/内网有意义的域，
// 公网上游不会使用，出现即说明目标是内网。
var blockedHostSuffixes = []string{".localhost", ".internal", ".local", ".localdomain"}

// blockedCIDRs 补充 net.IP 内建判定（IsLoopback/IsPrivate/IsLinkLocal*/IsUnspecified）
// 未覆盖、但同样不应作为出站目标的网段。IPv4 网段对 IPv4-mapped IPv6（::ffff:a.b.c.d）同样生效，
// 因为 net.IPNet.Contains 会先做 To4 归一化。
var blockedCIDRs = mustParseCIDRs([]string{
	"0.0.0.0/8",          // "this network"
	"100.64.0.0/10",      // CGNAT（RFC 6598），常见于云内网/Tailscale
	"192.0.0.0/24",       // IETF 协议分配（RFC 6890）
	"198.18.0.0/15",      // 基准测试网段（RFC 2544）
	"240.0.0.0/4",        // 保留段（含 255.255.255.255 广播）
	"169.254.169.254/32", // 云元数据（已在 link-local 内，显式列出便于审计）
	"fd00:ec2::254/128",  // AWS IMDS IPv6（已在 ULA 内，显式列出便于审计）
})

func mustParseCIDRs(cidrs []string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic("urlvalidator: invalid CIDR " + c + ": " + err.Error())
		}
		out = append(out, n)
	}
	return out
}

// IsBlockedHost 报告 host 是否为 localhost、*.localhost、*.internal、*.local、*.localdomain、
// 云元数据域名，或回环、私网、链路本地、CGNAT、保留段、未指定地址的字面量 IP。
// 只判断字面量，域名的解析结果由 ValidateResolvedIP / SafeDialContext 校验。
func IsBlockedHost(host string) bool {
	return isBlockedHost(strings.ToLower(strings.TrimSpace(host)))
}

// IsBlockedIP 报告 ip 是否落在禁止作为出站目标的范围内（回环、私网、链路本地、
// CGNAT、保留段、未指定地址、云元数据地址）。nil 视为禁止。
func IsBlockedIP(ip net.IP) bool {
	return isBlockedIP(ip)
}

func isBlockedHost(host string) bool {
	if host == "" {
		return false
	}
	// URL 主机名可能带 IPv6 方括号或 zone（fe80::1%eth0），先剥离再判断字面量。
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	if _, blocked := blockedHostnames[host]; blocked {
		return true
	}
	for _, suffix := range blockedHostSuffixes {
		if strings.HasSuffix(host, suffix) {
			return true
		}
	}
	literal := host
	if idx := strings.IndexByte(literal, '%'); idx >= 0 {
		literal = literal[:idx]
	}
	if ip := net.ParseIP(literal); ip != nil {
		return isBlockedIP(ip)
	}
	return false
}

func isBlockedIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, n := range blockedCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}
