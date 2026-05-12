package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type DeepSeekBalance struct {
	Available bool
	Amount    string
	Currency  string
	Raw       map[string]any
}

type DeepSeekBalanceClient struct {
	httpClient *http.Client
}

func NewDeepSeekBalanceClient(httpClient *http.Client) *DeepSeekBalanceClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &DeepSeekBalanceClient{httpClient: httpClient}
}

func (c *DeepSeekBalanceClient) FetchBalanceForAccount(ctx context.Context, account *Account) (*DeepSeekBalance, error) {
	if account == nil || !account.IsDeepSeekAPIKey() {
		return nil, fmt.Errorf("deepseek api key account is required")
	}
	apiKey := account.GetDeepSeekAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("deepseek api key is required")
	}
	baseURL := strings.TrimRight(account.GetDeepSeekOpenAIBaseURL(), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/user/balance", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("deepseek balance status %d: %s", resp.StatusCode, strings.Join(strings.Fields(string(body)), " "))
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode deepseek balance: %w", err)
	}
	return parseDeepSeekBalance(raw), nil
}

func parseDeepSeekBalance(raw map[string]any) *DeepSeekBalance {
	balance := &DeepSeekBalance{Raw: raw}
	balance.Available, _ = raw["is_available"].(bool)

	infos, _ := raw["balance_infos"].([]any)
	var first map[string]any
	for _, item := range infos {
		info, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if first == nil {
			first = info
		}
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(info["currency"])), "CNY") {
			applyDeepSeekBalanceInfo(balance, info)
			return balance
		}
	}
	if first != nil {
		applyDeepSeekBalanceInfo(balance, first)
	}
	return balance
}

func applyDeepSeekBalanceInfo(balance *DeepSeekBalance, info map[string]any) {
	balance.Currency = strings.TrimSpace(fmt.Sprint(info["currency"]))
	balance.Amount = strings.TrimSpace(fmt.Sprint(info["total_balance"]))
}
