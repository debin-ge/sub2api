package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const miniMaxRemainsErrorBodyMaxBytes = 512
const miniMaxAPIKeyRedaction = "[REDACTED_API_KEY]"

type MiniMaxTokenPlanRemains struct {
	Text5hLimit     int64
	Text5hRemaining int64
	Raw             map[string]any
}

type MiniMaxTokenPlanClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewMiniMaxTokenPlanClient(baseURL string, httpClient *http.Client) *MiniMaxTokenPlanClient {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		baseURL = "https://www.minimax.io"
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &MiniMaxTokenPlanClient{baseURL: baseURL, httpClient: httpClient}
}

func (c *MiniMaxTokenPlanClient) FetchRemains(ctx context.Context, apiKey string) (*MiniMaxTokenPlanRemains, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return nil, fmt.Errorf("minimax token plan api key is required")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/token_plan/remains", nil)
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

	remains := &MiniMaxTokenPlanRemains{Raw: raw}
	if data, ok := raw["data"].(map[string]any); ok {
		remains.Text5hLimit = int64FromAny(data["text_5h_limit"])
		remains.Text5hRemaining = int64FromAny(data["text_5h_remaining"])
	}
	return remains, nil
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
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i
	default:
		return 0
	}
}
