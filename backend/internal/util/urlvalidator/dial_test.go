package urlvalidator

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeResolver struct {
	addrs map[string][]net.IPAddr
	err   error
	calls int
}

func (r *fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return r.addrs[host], nil
}

type recordingDialer struct {
	dialed []string
	err    error
}

func (d *recordingDialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	d.dialed = append(d.dialed, address)
	if d.err != nil {
		return nil, d.err
	}
	client, server := net.Pipe()
	_ = server.Close()
	return client, nil
}

func TestSafeDialContextRejectsBlockedLiteralWithoutResolving(t *testing.T) {
	resolver := &fakeResolver{}
	dialer := &recordingDialer{}
	dial := SafeDialContext(false, WithResolver(resolver), WithDialer(dialer))

	for _, addr := range []string{
		"127.0.0.1:80", "10.0.0.1:443", "100.64.0.1:80", "169.254.169.254:80",
		"[::ffff:10.0.0.1]:80", "[fd00:ec2::254]:80", "metadata.google.internal:80",
		"svc.internal:8080", "printer.local:9100", "localhost:6379",
	} {
		_, err := dial(context.Background(), "tcp", addr)
		require.ErrorIs(t, err, ErrBlockedHost, addr)
	}
	require.Zero(t, resolver.calls, "blocked literals must not trigger DNS lookups")
	require.Empty(t, dialer.dialed, "blocked literals must never be dialed")
}

func TestSafeDialContextDialsOnlyResolvedAllowedIP(t *testing.T) {
	resolver := &fakeResolver{addrs: map[string][]net.IPAddr{
		// 第一条解析结果是内网地址（DNS rebinding 场景），必须被跳过，只连公网地址。
		"upstream.example.com": {{IP: net.ParseIP("10.0.0.8")}, {IP: net.ParseIP("93.184.216.34")}},
	}}
	dialer := &recordingDialer{}
	dial := SafeDialContext(false, WithResolver(resolver), WithDialer(dialer))

	conn, err := dial(context.Background(), "tcp", "upstream.example.com:443")
	require.NoError(t, err)
	require.NotNil(t, conn)
	_ = conn.Close()
	require.Equal(t, []string{"93.184.216.34:443"}, dialer.dialed, "the checked IP must be the dialed IP")
}

func TestSafeDialContextRejectsWhenAllResolvedIPsBlocked(t *testing.T) {
	resolver := &fakeResolver{addrs: map[string][]net.IPAddr{
		"evil.example.com": {{IP: net.ParseIP("127.0.0.1")}, {IP: net.ParseIP("100.64.1.1")}, {IP: net.ParseIP("fd00:ec2::254")}},
	}}
	dialer := &recordingDialer{}
	dial := SafeDialContext(false, WithResolver(resolver), WithDialer(dialer))

	_, err := dial(context.Background(), "tcp", "evil.example.com:80")
	require.ErrorIs(t, err, ErrBlockedHost)
	require.Empty(t, dialer.dialed)
}

func TestSafeDialContextAllowPrivateStillPinsResolvedIP(t *testing.T) {
	resolver := &fakeResolver{addrs: map[string][]net.IPAddr{
		"minio.internal": {{IP: net.ParseIP("10.0.0.9")}},
	}}
	dialer := &recordingDialer{}
	dial := SafeDialContext(true, WithResolver(resolver), WithDialer(dialer))

	conn, err := dial(context.Background(), "tcp", "minio.internal:9000")
	require.NoError(t, err)
	_ = conn.Close()
	require.Equal(t, []string{"10.0.0.9:9000"}, dialer.dialed)

	// 字面量私网地址在 allowPrivate 下也直接放行
	conn, err = dial(context.Background(), "tcp", "192.168.1.10:9000")
	require.NoError(t, err)
	_ = conn.Close()
	require.Equal(t, "192.168.1.10:9000", dialer.dialed[len(dialer.dialed)-1])
}

func TestSafeDialContextResolverErrorsAndEmptyResults(t *testing.T) {
	boom := errors.New("nxdomain")
	dial := SafeDialContext(false, WithResolver(&fakeResolver{err: boom}), WithDialer(&recordingDialer{}))
	_, err := dial(context.Background(), "tcp", "missing.example.com:80")
	require.ErrorIs(t, err, boom)

	dial = SafeDialContext(false, WithResolver(&fakeResolver{addrs: map[string][]net.IPAddr{}}), WithDialer(&recordingDialer{}))
	_, err = dial(context.Background(), "tcp", "empty.example.com:80")
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrBlockedHost)

	_, err = dial(context.Background(), "tcp", "no-port-here")
	require.Error(t, err)
}

func TestSafeDialContextHonorsNetworkFamily(t *testing.T) {
	resolver := &fakeResolver{addrs: map[string][]net.IPAddr{
		"dual.example.com": {{IP: net.ParseIP("2606:4700:4700::1111")}, {IP: net.ParseIP("1.1.1.1")}},
	}}
	dialer := &recordingDialer{}
	dial := SafeDialContext(false, WithResolver(resolver), WithDialer(dialer))

	conn, err := dial(context.Background(), "tcp4", "dual.example.com:443")
	require.NoError(t, err)
	_ = conn.Close()
	require.Equal(t, []string{"1.1.1.1:443"}, dialer.dialed)
}

func TestSafeDialContextConnectsToRealLoopbackListenerWhenPrivateAllowed(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			_ = c.Close()
		}
	}()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)

	resolver := &fakeResolver{addrs: map[string][]net.IPAddr{
		"svc.example.com": {{IP: net.ParseIP("127.0.0.1")}},
	}}
	// 默认拨号器 + 假解析器：确认解析→按 IP 建连的真实路径可用。
	dial := SafeDialContext(true, WithResolver(resolver))
	conn, err := dial(context.Background(), "tcp", net.JoinHostPort("svc.example.com", port))
	require.NoError(t, err)
	_ = conn.Close()

	// 同一目标在默认策略下必须被拒绝。
	_, err = SafeDialContext(false, WithResolver(resolver))(context.Background(), "tcp", net.JoinHostPort("svc.example.com", port))
	require.ErrorIs(t, err, ErrBlockedHost)
}
