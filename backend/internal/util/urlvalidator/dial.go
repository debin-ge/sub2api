package urlvalidator

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"
)

// Resolver 是 SafeDialContext 使用的最小 DNS 接口，与 *net.Resolver 兼容；
// 测试可注入假解析器以控制解析结果。
type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

// ContextDialer 是 SafeDialContext 实际建连所用的最小接口，与 *net.Dialer 兼容。
type ContextDialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// DialContextFunc 与 http.Transport.DialContext 签名一致。
type DialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

// SafeDialOption 调整 SafeDialContext 的解析器与底层拨号器。
type SafeDialOption func(*safeDialer)

// WithResolver 指定 DNS 解析器（默认 net.DefaultResolver）。
func WithResolver(r Resolver) SafeDialOption {
	return func(d *safeDialer) {
		if r != nil {
			d.resolver = r
		}
	}
}

// WithDialer 指定底层拨号器（默认 5s 超时、30s keep-alive 的 net.Dialer）。
func WithDialer(dialer ContextDialer) SafeDialOption {
	return func(d *safeDialer) {
		if dialer != nil {
			d.dialer = dialer
		}
	}
}

type safeDialer struct {
	allowPrivate bool
	resolver     Resolver
	dialer       ContextDialer
}

// SafeDialContext 返回一个可直接赋给 http.Transport.DialContext 的拨号函数：
// 先按 IsBlockedHost/IsBlockedIP 规则校验目标，再对解析出的、已通过校验的 IP 逐个建连，
// 使“被校验的 IP”与“实际连接的 IP”是同一个，消除 DNS rebinding（校验时解析到公网、
// 连接时再解析到内网）的时间窗。
//
//   - allowPrivate 为 true 时跳过所有地址策略检查，只保留“解析后按 IP 建连”的行为；
//   - 字面量 IP 直接校验后建连；
//   - 主机名先做黑名单（localhost/*.internal/云元数据等）判断，再解析并逐 IP 过滤；
//   - 被策略拒绝的错误可用 errors.Is(err, ErrBlockedHost) 识别。
//
// 注意：若 Transport 配置了代理，实际目的地由代理解析，本函数只会校验到代理地址；
// 需要严格阻断时应同时把 Transport.Proxy 置为 nil。
func SafeDialContext(allowPrivate bool, opts ...SafeDialOption) DialContextFunc {
	d := &safeDialer{
		allowPrivate: allowPrivate,
		resolver:     net.DefaultResolver,
		dialer:       &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(d)
		}
	}
	return d.dialContext
}

func (d *safeDialer) dialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil, &net.AddrError{Err: "missing host", Addr: address}
	}
	if !d.allowPrivate && isBlockedHost(host) {
		return nil, fmt.Errorf("dial %s: %w", address, ErrBlockedHost)
	}

	// 字面量 IP：上面已校验，直接建连。
	if ip := net.ParseIP(strings.SplitN(host, "%", 2)[0]); ip != nil {
		return d.dialer.DialContext(ctx, network, address)
	}

	addrs, err := d.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, &net.AddrError{Err: "no addresses for host", Addr: host}
	}

	var lastErr error
	for _, a := range addrs {
		if !networkAcceptsIP(network, a.IP) {
			continue
		}
		if !d.allowPrivate && isBlockedIP(a.IP) {
			lastErr = fmt.Errorf("dial %s (%s): %w", address, a.IP.String(), ErrBlockedHost)
			continue
		}
		conn, dialErr := d.dialer.DialContext(ctx, network, net.JoinHostPort(a.IP.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
		if ctx.Err() != nil {
			break
		}
	}
	if lastErr == nil {
		lastErr = &net.AddrError{Err: "no usable addresses", Addr: host}
	}
	return nil, lastErr
}

// networkAcceptsIP 按 network 后缀（tcp4/tcp6/udp4/udp6）过滤地址族；无后缀时全部接受。
func networkAcceptsIP(network string, ip net.IP) bool {
	isV4 := ip.To4() != nil
	switch {
	case strings.HasSuffix(network, "4"):
		return isV4
	case strings.HasSuffix(network, "6"):
		return !isV4
	default:
		return true
	}
}
