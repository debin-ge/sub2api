package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
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
}

func NewResellerBalanceClient(httpClient *http.Client) *ResellerBalanceClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &ResellerBalanceClient{httpClient: httpClient}
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/v1/balance", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		result.Status = ResellerBalanceStatusUpstreamUnreachable
		return result, nil
	}
	defer resp.Body.Close()

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
