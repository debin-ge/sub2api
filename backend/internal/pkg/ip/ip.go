// Package ip 提供客户端 IP 地址提取工具。
package ip

import (
	"net"
	"strings"

	"github.com/gin-gonic/gin"
)

const forwardedIPSettingsKey = "sub2api.forwarded_ip_settings"

// TrustedProxyPolicy 描述兼容模式（security.trust_forwarded_ip_for_api_key_acl=true）
// 下哪些地址被视为反向代理：只有可信对端直连时才会读取转发头，且 X-Forwarded-For
// 链路中匹配该策略的跳会被当作代理跳过。
//
//   - 显式配置了 server.trusted_proxies 时，仅信任列表中的 IP/CIDR（显式空列表 = 谁都不信）。
//   - 未配置时退化为信任回环、RFC1918/ULA 内网与链路本地地址，兼容「反代与应用同机 /
//     同 Docker 网络」这一最常见部署形态。
//
// 策略在进程启动时编译一次，随请求快照引用，热路径不再解析 CIDR。
type TrustedProxyPolicy struct {
	configured bool
	rules      *CompiledIPRules
}

// NewTrustedProxyPolicy 由 server.trusted_proxies 及其「是否显式配置」标记构建策略。
func NewTrustedProxyPolicy(proxies []string, configured bool) *TrustedProxyPolicy {
	if !configured {
		return &TrustedProxyPolicy{}
	}
	return &TrustedProxyPolicy{configured: true, rules: CompileIPRules(proxies)}
}

// Configured 返回策略是否来自显式配置的 server.trusted_proxies 列表。
func (p *TrustedProxyPolicy) Configured() bool {
	return p != nil && p.configured
}

// trusts 判断 parsed 是否为可信代理地址。nil 策略等价于未配置。
func (p *TrustedProxyPolicy) trusts(parsed net.IP) bool {
	if parsed == nil {
		return false
	}
	if p == nil || !p.configured {
		return isTrustedByDefault(parsed)
	}
	return matchesCompiledRules(parsed, p.rules)
}

// ForwardedIPSettings 是按请求快照的转发 IP 解析设置，由 SessionBindingContext
// 写入，保证单个请求内开关、自定义头列表与可信代理策略三者一致。
type ForwardedIPSettings struct {
	TrustForwarded bool
	Headers        []string
	TrustedProxies *TrustedProxyPolicy
}

// SetForwardedIPSettings snapshots the forwarded-IP mode and custom header list
// for this request. 未提供可信代理策略时按「未配置」处理（信任内网/回环对端）。
func SetForwardedIPSettings(c *gin.Context, enabled bool, headers []string) {
	SetForwardedIPSettingsWithPolicy(c, enabled, headers, nil)
}

// SetForwardedIPSettingsWithPolicy 在 SetForwardedIPSettings 的基础上附带可信代理策略。
func SetForwardedIPSettingsWithPolicy(c *gin.Context, enabled bool, headers []string, trustedProxies *TrustedProxyPolicy) {
	if c == nil {
		return
	}
	c.Set(forwardedIPSettingsKey, ForwardedIPSettings{
		TrustForwarded: enabled,
		Headers:        append([]string(nil), headers...),
		TrustedProxies: trustedProxies,
	})
}

// SetLegacyForwardedIPTrust records whether raw forwarding headers override
// Gin's server.trusted_proxies chain for this request.
func SetLegacyForwardedIPTrust(c *gin.Context, enabled bool) {
	SetForwardedIPSettings(c, enabled, nil)
}

func requestForwardedIPSettings(c *gin.Context) (ForwardedIPSettings, bool) {
	if c == nil {
		return ForwardedIPSettings{}, false
	}
	value, ok := c.Get(forwardedIPSettingsKey)
	if !ok {
		return ForwardedIPSettings{}, false
	}
	settings, ok := value.(ForwardedIPSettings)
	return settings, ok
}

func requestUsesLegacyForwardedIPTrust(c *gin.Context) bool {
	settings, ok := requestForwardedIPSettings(c)
	return !ok || settings.TrustForwarded
}

// GetClientIP resolves the client address using the compatibility
// forwarding-header path (with the trusted-peer guard) when the switch is on,
// and Gin's trusted-proxy chain when it is off. It remains the path for request
// metadata and usage/error logs; security-sensitive callers must use
// GetTrustedClientIP or GetSecurityClientIP. 两者共用同一套解析，避免审计日志
// 与 ACL / 限流看到的客户端 IP 不一致。
func GetClientIP(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if !requestUsesLegacyForwardedIPTrust(c) {
		return GetTrustedClientIP(c)
	}
	settings, hasSnapshot := requestForwardedIPSettings(c)
	return resolveForwardedClientIP(c, settings, hasSnapshot)
}

// resolveForwardedClientIP 兼容模式下的客户端 IP 解析：
//
//  1. 可信对端守卫：只有 TCP 直连对端匹配 TrustedProxies 策略时才读取任何转发头，
//     否则直接返回对端地址——公网直连者伪造的 CF-Connecting-IP / X-Real-IP /
//     X-Forwarded-For 一律无效。
//  2. 头优先级：运营方自定义 forwarded_client_ip_headers → X-Forwarded-For →
//     X-Real-IP → CF-Connecting-IP。反代会用真实对端覆盖 XFF / X-Real-IP，
//     而 CF-Connecting-IP 往往被原样透传，因此不能再让它优先。
//  3. X-Forwarded-For 采用「最右侧不可信」算法：从右向左跳过可信代理跳
//     （策略匹配；未配置时即内网/回环/链路本地），取第一个剩余的合法 IP。
//     绝不取「最左侧公网 IP」——那是客户端可控的位置。
//  4. 全部无可用值时回退到对端地址。
//
// hasSnapshot 表示 settings 来自 SessionBindingContext 的请求快照（生产路径恒为 true）。
func resolveForwardedClientIP(c *gin.Context, settings ForwardedIPSettings, hasSnapshot bool) string {
	remote := remoteAddrIP(c)
	if remote == nil {
		// 无法解析对端（异常连接）：退回 Gin 的解析结果，不读取任何转发头。
		return GetTrustedClientIP(c)
	}
	remoteIP := remote.String()
	policy := settings.TrustedProxies
	if !policy.trusts(remote) {
		// 对端不可信：不读取任何转发头，直接归因到对端地址。
		if hasSnapshot {
			return remoteIP
		}
		// 未挂载 SessionBindingContext（单测 / 特殊路由）时没有可信代理策略可依，
		// 退回 Gin 的 server.trusted_proxies 链，尊重 Gin 侧的显式配置。
		if trusted := GetTrustedClientIP(c); trusted != "" {
			return trusted
		}
		return remoteIP
	}

	customIP, customFallback := resolveCustomForwardedClientIP(c, settings.Headers)
	if customIP != "" {
		return customIP
	}
	if xff := resolveRightmostUntrustedXFF(c.Request.Header.Values("X-Forwarded-For"), policy); xff != "" {
		return xff
	}
	for _, header := range []string{"X-Real-IP", "CF-Connecting-IP"} {
		parsed := parseValidIP(c.GetHeader(header))
		if parsed != nil && !policy.trusts(parsed) {
			return parsed.String()
		}
	}
	if customFallback != "" {
		return customFallback
	}
	return remoteIP
}

// resolveRightmostUntrustedXFF 对 X-Forwarded-For 链（多个同名头按出现顺序拼接）
// 从右向左扫描，跳过可信代理跳与非法值，返回第一个不属于代理的合法 IP。
func resolveRightmostUntrustedXFF(values []string, policy *TrustedProxyPolicy) string {
	var chain []string
	for _, value := range values {
		chain = append(chain, strings.Split(value, ",")...)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		parsed := parseValidIP(chain[i])
		if parsed == nil || policy.trusts(parsed) {
			continue
		}
		return parsed.String()
	}
	return ""
}

func resolveCustomForwardedClientIP(c *gin.Context, headers []string) (string, string) {
	if c == nil {
		return "", ""
	}
	var fallback string
	for _, header := range headers {
		for _, value := range c.Request.Header.Values(header) {
			for _, candidate := range strings.Split(value, ",") {
				parsed := net.ParseIP(strings.TrimSpace(candidate))
				if parsed == nil {
					continue
				}
				normalized := parsed.String()
				if isPrivateIP(normalized) {
					if fallback == "" {
						fallback = normalized
					}
					continue
				}
				return normalized, fallback
			}
		}
	}
	return "", fallback
}

// remoteAddrIP 解析 TCP 直连对端地址（去端口），无法解析时返回 nil。
func remoteAddrIP(c *gin.Context) net.IP {
	if c == nil || c.Request == nil {
		return nil
	}
	return parseValidIP(c.Request.RemoteAddr)
}

// GetTrustedClientIP 从 Gin 的可信代理解析链提取客户端 IP。
// 该方法依赖 gin.Engine.SetTrustedProxies 配置，不会优先直接信任原始转发头值。
// 适用于 ACL / 风控等安全敏感场景。
func GetTrustedClientIP(c *gin.Context) string {
	if c == nil {
		return ""
	}
	return normalizeIP(c.ClientIP())
}

// GetSecurityClientIP returns the address used by security-sensitive paths.
// When legacy forwarded-IP trust is enabled, forwarding headers from trusted
// peers take over client-IP resolution. When disabled, Gin's
// server.trusted_proxies chain is authoritative.
func GetSecurityClientIP(c *gin.Context, trustForwarded bool) string {
	if c == nil {
		return ""
	}
	settings, hasSnapshot := requestForwardedIPSettings(c)
	if hasSnapshot {
		trustForwarded = settings.TrustForwarded
	}
	if trustForwarded {
		return resolveForwardedClientIP(c, settings, hasSnapshot)
	}
	return GetTrustedClientIP(c)
}

// normalizeIP 规范化 IP 地址，去除端口号和空格。
func normalizeIP(ip string) string {
	ip = strings.TrimSpace(ip)
	// 移除端口号（如 "192.168.1.1:8080" -> "192.168.1.1"）
	if host, _, err := net.SplitHostPort(ip); err == nil {
		return host
	}
	return ip
}

// parseValidIP 规范化并解析代理头 / 对端地址中的候选值（容忍端口后缀与空白），
// 非法值（unknown、主机名等）返回 nil。
func parseValidIP(value string) net.IP {
	return net.ParseIP(normalizeIP(value))
}

// isTrustedByDefault 是未配置 server.trusted_proxies 时的默认可信对端范围：
// 回环、RFC1918/ULA 内网、链路本地。
func isTrustedByDefault(ip net.IP) bool {
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

// CompiledIPRules 表示预编译的 IP 匹配规则。
// PatternCount 记录原始规则数量，用于保留“规则存在但全无效”时的行为语义。
type CompiledIPRules struct {
	CIDRs        []*net.IPNet
	IPs          []net.IP
	PatternCount int
}

// CompileIPRules 将 IP/CIDR 字符串规则预编译为可复用结构。
// 非法规则会被忽略，但 PatternCount 会保留原始规则条数。
func CompileIPRules(patterns []string) *CompiledIPRules {
	compiled := &CompiledIPRules{
		CIDRs:        make([]*net.IPNet, 0, len(patterns)),
		IPs:          make([]net.IP, 0, len(patterns)),
		PatternCount: len(patterns),
	}
	for _, pattern := range patterns {
		normalized := strings.TrimSpace(pattern)
		if normalized == "" {
			continue
		}
		if strings.Contains(normalized, "/") {
			_, cidr, err := net.ParseCIDR(normalized)
			if err != nil || cidr == nil {
				continue
			}
			compiled.CIDRs = append(compiled.CIDRs, cidr)
			continue
		}
		parsedIP := net.ParseIP(normalized)
		if parsedIP == nil {
			continue
		}
		compiled.IPs = append(compiled.IPs, parsedIP)
	}
	return compiled
}

func matchesCompiledRules(parsedIP net.IP, rules *CompiledIPRules) bool {
	if parsedIP == nil || rules == nil {
		return false
	}
	for _, cidr := range rules.CIDRs {
		if cidr.Contains(parsedIP) {
			return true
		}
	}
	for _, ruleIP := range rules.IPs {
		if parsedIP.Equal(ruleIP) {
			return true
		}
	}
	return false
}

// isPrivateIP 判断是否为内网（RFC1918/ULA）或回环地址；用于自定义转发头中
// 「公网值优先、内网值仅作回退」的取值语义。
func isPrivateIP(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.IsPrivate() || ip.IsLoopback()
}

// MatchesPattern 检查 IP 是否匹配指定的模式（支持单个 IP 或 CIDR）。
// pattern 可以是：
// - 单个 IP: "192.168.1.100"
// - CIDR 范围: "192.168.1.0/24"
func MatchesPattern(clientIP, pattern string) bool {
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return false
	}

	// 尝试解析为 CIDR
	if strings.Contains(pattern, "/") {
		_, cidr, err := net.ParseCIDR(pattern)
		if err != nil {
			return false
		}
		return cidr.Contains(ip)
	}

	// 作为单个 IP 处理
	patternIP := net.ParseIP(pattern)
	if patternIP == nil {
		return false
	}
	return ip.Equal(patternIP)
}

// MatchesAnyPattern 检查 IP 是否匹配任意一个模式。
func MatchesAnyPattern(clientIP string, patterns []string) bool {
	for _, pattern := range patterns {
		if MatchesPattern(clientIP, pattern) {
			return true
		}
	}
	return false
}

// CheckIPRestriction 检查 IP 是否被 API Key 的 IP 限制允许。
// 返回值：(是否允许, 拒绝原因)
// 逻辑：
// 1. 先检查黑名单，如果在黑名单中则直接拒绝
// 2. 如果白名单不为空，IP 必须在白名单中
// 3. 如果白名单为空，允许访问（除非被黑名单拒绝）
func CheckIPRestriction(clientIP string, whitelist, blacklist []string) (bool, string) {
	return CheckIPRestrictionWithCompiledRules(
		clientIP,
		CompileIPRules(whitelist),
		CompileIPRules(blacklist),
	)
}

// CheckIPRestrictionWithCompiledRules 使用预编译规则检查 IP 是否允许访问。
func CheckIPRestrictionWithCompiledRules(clientIP string, whitelist, blacklist *CompiledIPRules) (bool, string) {
	// 规范化 IP
	clientIP = normalizeIP(clientIP)
	if clientIP == "" {
		return false, "access denied"
	}
	parsedIP := net.ParseIP(clientIP)
	if parsedIP == nil {
		return false, "access denied"
	}

	// 1. 检查黑名单
	if blacklist != nil && blacklist.PatternCount > 0 && matchesCompiledRules(parsedIP, blacklist) {
		return false, "access denied"
	}

	// 2. 检查白名单（如果设置了白名单，IP 必须在其中）
	if whitelist != nil && whitelist.PatternCount > 0 && !matchesCompiledRules(parsedIP, whitelist) {
		return false, "access denied"
	}

	return true, ""
}

// ValidateIPPattern 验证 IP 或 CIDR 格式是否有效。
func ValidateIPPattern(pattern string) bool {
	if strings.Contains(pattern, "/") {
		_, _, err := net.ParseCIDR(pattern)
		return err == nil
	}
	return net.ParseIP(pattern) != nil
}

// ValidateIPPatterns 验证多个 IP 或 CIDR 格式。
// 返回无效的模式列表。
func ValidateIPPatterns(patterns []string) []string {
	var invalid []string
	for _, p := range patterns {
		if !ValidateIPPattern(p) {
			invalid = append(invalid, p)
		}
	}
	return invalid
}
