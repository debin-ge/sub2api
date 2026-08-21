package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const miniMaxRemainsErrorBodyMaxBytes = 512
const miniMaxAPIKeyRedaction = "[REDACTED_API_KEY]"
const miniMaxDefaultRemainsBaseURL = "https://www.minimax.io"
const miniMaxChinaRemainsBaseURL = "https://api.minimaxi.com"

type MiniMaxTokenPlanRemains struct {
	Text5hLimit     int64
	Text5hRemaining int64
	Raw             map[string]any
}

type MiniMaxTokenPlanClient struct {
	baseURL         string
	baseURLExplicit bool
	httpClient      *http.Client
}

func NewMiniMaxTokenPlanClient(baseURL string, httpClient *http.Client) *MiniMaxTokenPlanClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	baseURLExplicit := baseURL != ""
	if baseURL == "" {
		baseURL = miniMaxDefaultRemainsBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &MiniMaxTokenPlanClient{baseURL: baseURL, baseURLExplicit: baseURLExplicit, httpClient: httpClient}
}

func (c *MiniMaxTokenPlanClient) FetchRemains(ctx context.Context, apiKey string) (*MiniMaxTokenPlanRemains, error) {
	return c.fetchRemains(ctx, apiKey, c.baseURL)
}

func (c *MiniMaxTokenPlanClient) FetchRemainsForAccount(ctx context.Context, account *Account) (*MiniMaxTokenPlanRemains, error) {
	if account == nil || !account.IsMiniMaxTokenPlan() {
		return nil, fmt.Errorf("minimax token plan account is required")
	}
	if !miniMaxOfficialRemainsProbeSupported(account) {
		return nil, fmt.Errorf("minimax official remains probe is not supported for a third-party provider endpoint")
	}
	baseURL := c.baseURL
	if !c.baseURLExplicit {
		baseURL = miniMaxRemainsBaseURLForAccount(account)
	}
	return c.fetchRemains(ctx, account.GetMiniMaxAPIKey(), baseURL)
}

func (c *MiniMaxTokenPlanClient) fetchRemains(ctx context.Context, apiKey string, baseURL string) (*MiniMaxTokenPlanRemains, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("minimax token plan api key is required")
	}
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = miniMaxDefaultRemainsBaseURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/token_plan/remains", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("minimax remains status %d: %s", resp.StatusCode, sanitizeMiniMaxErrorBody(body, apiKey))
	}

	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode minimax remains: %w", err)
	}
	if codeValue, ok := raw["code"]; ok {
		code := int64FromAny(codeValue)
		if code != 0 {
			message := sanitizeMiniMaxErrorBody(body, apiKey)
			if msg, ok := raw["msg"]; ok {
				message = sanitizeMiniMaxErrorBody([]byte(fmt.Sprint(msg)), apiKey)
			} else if msg, ok := raw["message"]; ok {
				message = sanitizeMiniMaxErrorBody([]byte(fmt.Sprint(msg)), apiKey)
			}
			return nil, fmt.Errorf("minimax remains code %d: %s", code, message)
		}
	}
	if baseResp, ok := raw["base_resp"].(map[string]any); ok {
		statusCode := int64FromAny(baseResp["status_code"])
		if statusCode != 0 {
			message := sanitizeMiniMaxErrorBody(body, apiKey)
			if msg, ok := baseResp["status_msg"]; ok {
				message = sanitizeMiniMaxErrorBody([]byte(fmt.Sprint(msg)), apiKey)
			} else if msg, ok := baseResp["message"]; ok {
				message = sanitizeMiniMaxErrorBody([]byte(fmt.Sprint(msg)), apiKey)
			}
			return nil, fmt.Errorf("minimax remains base_resp %d: %s", statusCode, message)
		}
	}

	remains := &MiniMaxTokenPlanRemains{Raw: raw}
	if data, ok := raw["data"].(map[string]any); ok {
		limit, limitOK := int64FromAnyOK(data["text_5h_limit"])
		remaining, remainingOK := int64FromAnyOK(data["text_5h_remaining"])
		if limitOK && remainingOK && limit > 0 && remaining >= 0 && remaining <= limit {
			remains.Text5hLimit = limit
			remains.Text5hRemaining = remaining
		}
	}
	if remains.Text5hLimit == 0 {
		applyMiniMaxModelRemains(remains, raw["model_remains"])
	}
	if remains.Text5hLimit <= 0 || remains.Text5hRemaining < 0 || remains.Text5hRemaining > remains.Text5hLimit {
		return nil, fmt.Errorf("invalid minimax remains response: missing valid 5h limit and remaining fields")
	}
	return remains, nil
}

func miniMaxRemainsBaseURLForAccount(account *Account) string {
	for _, raw := range []string{account.GetMiniMaxAnthropicBaseURL(), account.GetMiniMaxOpenAIBaseURL()} {
		parsed, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			continue
		}
		switch strings.ToLower(parsed.Hostname()) {
		case miniMaxChinaHost:
			return miniMaxChinaRemainsBaseURL
		case miniMaxInternationalHost:
			return miniMaxDefaultRemainsBaseURL
		}
	}
	return miniMaxDefaultRemainsBaseURL
}

func applyMiniMaxModelRemains(remains *MiniMaxTokenPlanRemains, value any) {
	if remains == nil {
		return
	}
	items, ok := value.([]any)
	if !ok {
		return
	}
	var fallback map[string]any
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		limit, limitOK := int64FromAnyOK(entry["current_interval_total_count"])
		used, usedOK := int64FromAnyOK(entry["current_interval_usage_count"])
		if !limitOK || !usedOK || limit <= 0 || used < 0 || used > limit {
			continue
		}
		if fallback == nil {
			fallback = entry
		}
		modelName := strings.ToLower(strings.TrimSpace(fmt.Sprint(entry["model_name"])))
		if strings.HasPrefix(modelName, "minimax-m") {
			setMiniMaxModelRemainValues(remains, entry)
			return
		}
	}
	if fallback != nil {
		setMiniMaxModelRemainValues(remains, fallback)
	}
}

func setMiniMaxModelRemainValues(remains *MiniMaxTokenPlanRemains, entry map[string]any) {
	limit, _ := int64FromAnyOK(entry["current_interval_total_count"])
	used, _ := int64FromAnyOK(entry["current_interval_usage_count"])
	remaining := limit - used
	if remaining < 0 {
		remaining = 0
	}
	remains.Text5hLimit = limit
	remains.Text5hRemaining = remaining
}

func sanitizeMiniMaxErrorBody(body []byte, apiKey string) string {
	sanitized := strings.Join(strings.Fields(string(body)), " ")
	if sanitized == "" {
		return "empty upstream error body"
	}
	if apiKey != "" {
		sanitized = strings.ReplaceAll(sanitized, "Bearer "+apiKey, "Bearer "+miniMaxAPIKeyRedaction)
		sanitized = strings.ReplaceAll(sanitized, apiKey, miniMaxAPIKeyRedaction)
	}
	if len(sanitized) > miniMaxRemainsErrorBodyMaxBytes {
		return sanitized[:miniMaxRemainsErrorBodyMaxBytes] + "..."
	}
	return sanitized
}

func int64FromAny(v any) int64 {
	value, _ := int64FromAnyOK(v)
	return value
}

func int64FromAnyOK(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		return int64(n), n == float64(int64(n))
	case int64:
		return n, true
	case int:
		return int64(n), true
	case json.Number:
		i, err := n.Int64()
		return i, err == nil
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i, err == nil
	default:
		return 0, false
	}
}
