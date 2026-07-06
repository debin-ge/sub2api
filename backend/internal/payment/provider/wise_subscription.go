package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const wiseWebhookTriggerBalanceCredit = "balances#credit"
const wiseWebhookDeliveryVersion = "4.0.0"
const wiseSubscriptionHTTPTimeout = 15 * time.Second

type WiseProfileSubscriptionRequest struct {
	ProfileID       string
	Name            string
	TriggerOn       string
	DeliveryVersion string
	DeliveryURL     string
}

type WiseProfileSubscriptionResponse struct {
	ID        string `json:"id"`
	TriggerOn string `json:"trigger_on"`
	Delivery  struct {
		Version string `json:"version"`
		URL     string `json:"url"`
	} `json:"delivery"`
}

type WiseSubscriptionClient struct {
	apiBase    string
	apiToken   string
	httpClient *http.Client
}

func NewWiseSubscriptionClient(apiBase, apiToken string) *WiseSubscriptionClient {
	apiBase = strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if apiBase == "" {
		apiBase = wiseDefaultAPIBase
	}
	return &WiseSubscriptionClient{
		apiBase:    apiBase,
		apiToken:   strings.TrimSpace(apiToken),
		httpClient: &http.Client{Timeout: wiseSubscriptionHTTPTimeout},
	}
}

func (c *WiseSubscriptionClient) CreateProfileSubscription(ctx context.Context, req WiseProfileSubscriptionRequest) (*WiseProfileSubscriptionResponse, error) {
	if c == nil {
		return nil, fmt.Errorf("wise subscription: nil client")
	}

	profileID := strings.TrimSpace(req.ProfileID)
	if profileID == "" {
		return nil, fmt.Errorf("wise subscription: missing profile id")
	}
	apiToken := strings.TrimSpace(c.apiToken)
	if apiToken == "" {
		return nil, fmt.Errorf("wise subscription: missing API token")
	}

	triggerOn := strings.TrimSpace(req.TriggerOn)
	if triggerOn == "" {
		triggerOn = wiseWebhookTriggerBalanceCredit
	}
	deliveryVersion := strings.TrimSpace(req.DeliveryVersion)
	if deliveryVersion == "" {
		deliveryVersion = wiseWebhookDeliveryVersion
	}
	deliveryURL, err := normalizeWiseHTTPSURL(req.DeliveryURL, "webhookDeliveryUrl")
	if err != nil {
		return nil, err
	}

	body := struct {
		Name      string `json:"name"`
		TriggerOn string `json:"trigger_on"`
		Delivery  struct {
			Version string `json:"version"`
			URL     string `json:"url"`
		} `json:"delivery"`
	}{
		Name:      strings.TrimSpace(req.Name),
		TriggerOn: triggerOn,
	}
	body.Delivery.Version = deliveryVersion
	body.Delivery.URL = deliveryURL

	encodedBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("wise subscription encode request: %w", err)
	}

	apiBase := strings.TrimRight(strings.TrimSpace(c.apiBase), "/")
	if apiBase == "" {
		apiBase = wiseDefaultAPIBase
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		apiBase+"/v3/profiles/"+url.PathEscape(profileID)+"/subscriptions",
		bytes.NewReader(encodedBody),
	)
	if err != nil {
		return nil, fmt.Errorf("wise subscription build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiToken)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: wiseSubscriptionHTTPTimeout}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("wise subscription request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, wiseMaxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("wise subscription read response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("wise subscription HTTP %d: %s", resp.StatusCode, maskWiseSecretSummary(responseBody, apiToken))
	}

	var out WiseProfileSubscriptionResponse
	if err := json.Unmarshal(responseBody, &out); err != nil {
		return nil, fmt.Errorf("wise subscription parse response: %w", err)
	}
	return &out, nil
}

func (c *WiseSubscriptionClient) DeleteProfileSubscription(ctx context.Context, profileID string, subscriptionID string) error {
	if c == nil {
		return fmt.Errorf("wise subscription delete: nil client")
	}
	profileID = strings.TrimSpace(profileID)
	if profileID == "" {
		return fmt.Errorf("wise subscription delete: missing profile id")
	}
	subscriptionID = strings.TrimSpace(subscriptionID)
	if subscriptionID == "" {
		return fmt.Errorf("wise subscription delete: missing subscription id")
	}
	apiToken := strings.TrimSpace(c.apiToken)
	if apiToken == "" {
		return fmt.Errorf("wise subscription delete: missing API token")
	}

	apiBase := strings.TrimRight(strings.TrimSpace(c.apiBase), "/")
	if apiBase == "" {
		apiBase = wiseDefaultAPIBase
	}
	httpReq, err := http.NewRequestWithContext(
		ctx,
		http.MethodDelete,
		apiBase+"/v3/profiles/"+url.PathEscape(profileID)+"/subscriptions/"+url.PathEscape(subscriptionID),
		nil,
	)
	if err != nil {
		return fmt.Errorf("wise subscription delete build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+apiToken)
	httpReq.Header.Set("Accept", "application/json")

	client := c.httpClient
	if client == nil {
		client = &http.Client{Timeout: wiseSubscriptionHTTPTimeout}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("wise subscription delete request: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, wiseMaxResponseSize))
	if err != nil {
		return fmt.Errorf("wise subscription delete read response: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		return nil
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("wise subscription delete HTTP %d: %s", resp.StatusCode, maskWiseSecretSummary(responseBody, apiToken))
	}
	return nil
}

func maskWiseSecretSummary(body []byte, secret string) string {
	summary := summarizeWiseResponse(body)
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return summary
	}
	return strings.ReplaceAll(summary, secret, "[REDACTED]")
}
