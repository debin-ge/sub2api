package service

import (
	"net"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

const (
	compatibleGatewayDefaultUpstreamTimeout = 60 * time.Second
	compatibleGatewayDialTimeout            = 15 * time.Second
	compatibleGatewayTLSHandshakeTimeout    = 10 * time.Second
	compatibleGatewayIdleConnTimeout        = 90 * time.Second
)

func compatibleGatewayUpstreamTimeoutFromConfig(cfg *config.Config) time.Duration {
	if cfg == nil || cfg.Gateway.CompatibleUpstreamTimeoutSeconds <= 0 {
		return compatibleGatewayDefaultUpstreamTimeout
	}
	return time.Duration(cfg.Gateway.CompatibleUpstreamTimeoutSeconds) * time.Second
}

func newDefaultCompatibleGatewayHTTPClient(responseHeaderTimeout time.Duration) *http.Client {
	if responseHeaderTimeout <= 0 {
		responseHeaderTimeout = compatibleGatewayDefaultUpstreamTimeout
	}
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   compatibleGatewayDialTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       compatibleGatewayIdleConnTimeout,
			TLSHandshakeTimeout:   compatibleGatewayTLSHandshakeTimeout,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: responseHeaderTimeout,
		},
	}
}
