package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

const opencodeModelsFetchTimeout = 15 * time.Second

// FetchOpenCodeAvailableModelIDs reads the model catalog exposed by the
// configured OpenCode2API-compatible upstream.
func FetchOpenCodeAvailableModelIDs(ctx context.Context, account *Account, httpClient *http.Client) ([]string, error) {
	apiKey, baseURL, err := validateOpenCodeAccount(account)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = newDefaultCompatibleGatewayHTTPClient(opencodeModelsFetchTimeout)
		httpClient.Timeout = opencodeModelsFetchTimeout
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, openCodeEndpointURL(baseURL, "/v1/models"), nil)
	if err != nil {
		return nil, fmt.Errorf("build opencode models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch opencode models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("fetch opencode models: upstream status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read opencode models response: %w", err)
	}

	modelIDs, err := parseOpenCodeModelListBody(body)
	if err != nil {
		return nil, err
	}
	if len(modelIDs) == 0 {
		return nil, fmt.Errorf("fetch opencode models: no models returned")
	}
	return modelIDs, nil
}

func parseOpenCodeModelListBody(body []byte) ([]string, error) {
	return parseCompatibleGatewayModelListBody(body, "opencode")
}
