package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
)

const (
	ResellerBalanceStatusDisabled            = "disabled"
	ResellerBalanceStatusNotConfigured       = "not_configured"
	ResellerBalanceStatusOK                  = "ok"
	ResellerBalanceStatusAuthFailed          = "auth_failed"
	ResellerBalanceStatusUpstreamUnreachable = "upstream_unreachable"
	ResellerBalanceStatusInvalidResponse     = "invalid_response"
	ResellerBalanceStatusUpstreamError       = "upstream_error"
)

type ResellerBalanceRequest struct {
	Endpoint string
	APIKey   string
}

type ResellerBalanceResult struct {
	Enabled          bool      `json:"enabled"`
	Configured       bool      `json:"configured"`
	UpstreamEndpoint string    `json:"upstream_endpoint"`
	Balance          float64   `json:"balance"`
	UserID           int64     `json:"user_id,omitempty"`
	Status           string    `json:"status"`
	CheckedAt        time.Time `json:"checked_at,omitempty"`
}

type ResellerBalanceClient struct {
	httpClient *http.Client
	// allowPrivateHosts 为 false 时，Fetch 在请求前按 SSRF 策略拒绝私网/回环/云元数据地址，
	// 且默认客户端在拨号层再次校验解析后的真实 IP（防 DNS rebinding）。
	allowPrivateHosts bool
}

// NewResellerBalanceClient 构建上游余额查询客户端。
//
// httpClient 为 nil（生产注入路径）时使用加固的默认客户端：上游地址由管理员在后台配置，
// 但被劫持的管理员会话不应能把服务当作内网探针，因此不论全局 allow_private_hosts 如何设置，
// 都拒绝私网/回环/链路本地/云元数据目标，并以 urlvalidator.SafeDialContext 建连、不继承环境代理，
// 保证被校验的 IP 就是实际连接的 IP。
//
// 显式传入 httpClient 时，调用方被视为已自行决定出站策略（其 Transport 可自带 SSRF 防护或
// 面向测试的本地服务器），Fetch 仅做 scheme/格式校验，不再阻断私网地址。
func NewResellerBalanceClient(httpClient *http.Client) *ResellerBalanceClient {
	if httpClient != nil {
		return &ResellerBalanceClient{httpClient: httpClient, allowPrivateHosts: true}
	}
	transport := &http.Transport{
		// 不继承 HTTP(S)_PROXY：经代理时真实目的地由代理解析，会绕过下方拨号层校验。
		Proxy:                 nil,
		DialContext:           urlvalidator.SafeDialContext(false),
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          8,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &ResellerBalanceClient{
		httpClient:        &http.Client{Transport: transport, Timeout: 15 * time.Second},
		allowPrivateHosts: false,
	}
}

func (c *ResellerBalanceClient) Fetch(ctx context.Context, in ResellerBalanceRequest) (*ResellerBalanceResult, error) {
	endpoint := strings.TrimRight(strings.TrimSpace(in.Endpoint), "/")
	apiKey := strings.TrimSpace(in.APIKey)
	result := &ResellerBalanceResult{
		Enabled:          true,
		Configured:       endpoint != "" && apiKey != "",
		UpstreamEndpoint: endpoint,
		CheckedAt:        time.Now().UTC(),
	}
	if !result.Configured {
		result.Status = ResellerBalanceStatusNotConfigured
		return result, nil
	}

	// 出站地址校验：仅允许 http(s)；默认客户端下额外拒绝私网/回环/云元数据等目标。
	// 校验失败按“上游不可达”返回，避免把探测结果（可达/不可达）泄露给调用方。
	validatedEndpoint, err := urlvalidator.ValidateHTTPURL(endpoint, true, urlvalidator.ValidationOptions{AllowPrivate: c.allowPrivateHosts})
	if err != nil {
		slog.Warn("reseller_balance.endpoint_rejected", "endpoint", endpoint, "error", err)
		result.Status = ResellerBalanceStatusUpstreamUnreachable
		return result, nil
	}
	endpoint = validatedEndpoint

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/balance", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if errors.Is(err, urlvalidator.ErrBlockedHost) {
			slog.Warn("reseller_balance.endpoint_blocked_at_dial", "endpoint", endpoint, "error", err)
		}
		result.Status = ResellerBalanceStatusUpstreamUnreachable
		return result, nil
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		result.Status = ResellerBalanceStatusUpstreamUnreachable
		return result, nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		result.Status = ResellerBalanceStatusAuthFailed
		return result, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		result.Status = ResellerBalanceStatusUpstreamError
		return result, nil
	}

	var payload struct {
		Balance float64 `json:"balance"`
		UserID  int64   `json:"user_id"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.UserID <= 0 {
		result.Status = ResellerBalanceStatusInvalidResponse
		return result, nil
	}

	result.Status = ResellerBalanceStatusOK
	result.Balance = payload.Balance
	result.UserID = payload.UserID
	return result, nil
}
