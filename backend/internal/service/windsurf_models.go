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

const windsurfModelsFetchTimeout = 15 * time.Second

// FetchWindsurfAvailableModelIDs reads the model catalog exposed by the
// Windsurf reverse proxy. The caller should fall back to DefaultWindsurfModelIDs
// when the upstream does not implement /v1/models.
func FetchWindsurfAvailableModelIDs(ctx context.Context, account *Account, httpClient *http.Client) ([]string, error) {
	apiKey, err := validateWindsurfAccount(account)
	if err != nil {
		return nil, err
	}
	if httpClient == nil {
		httpClient = newDefaultCompatibleGatewayHTTPClient(windsurfModelsFetchTimeout)
		httpClient.Timeout = windsurfModelsFetchTimeout
	}

	upstreamURL := strings.TrimRight(account.GetWindsurfBaseURL(), "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, upstreamURL, nil)
	if err != nil {
		return nil, fmt.Errorf("build windsurf models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch windsurf models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("fetch windsurf models: upstream status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read windsurf models response: %w", err)
	}

	modelIDs, err := parseWindsurfModelListBody(body)
	if err != nil {
		return nil, err
	}
	if len(modelIDs) == 0 {
		return nil, fmt.Errorf("fetch windsurf models: no models returned")
	}
	return modelIDs, nil
}

func parseWindsurfModelListBody(body []byte) ([]string, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse windsurf models response: %w", err)
	}
	return normalizeWindsurfFetchedModelIDs(collectWindsurfModelIDs(payload)), nil
}

func collectWindsurfModelIDs(value any) []string {
	switch typed := value.(type) {
	case []any:
		var modelIDs []string
		for _, item := range typed {
			modelIDs = append(modelIDs, collectWindsurfModelIDs(item)...)
		}
		return modelIDs
	case map[string]any:
		for _, key := range []string{"data", "models"} {
			if nested, ok := typed[key]; ok {
				if modelIDs := collectWindsurfModelIDs(nested); len(modelIDs) > 0 {
					return modelIDs
				}
			}
		}
		for _, key := range []string{"id", "model", "model_uid"} {
			if modelID, ok := typed[key].(string); ok {
				return []string{modelID}
			}
		}
	case string:
		return []string{typed}
	}
	return nil
}

func normalizeWindsurfFetchedModelIDs(candidates []string) []string {
	seen := make(map[string]struct{}, len(candidates))
	modelIDs := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		modelID := strings.TrimSpace(candidate)
		if modelID == "" {
			continue
		}
		if _, ok := seen[modelID]; ok {
			continue
		}
		seen[modelID] = struct{}{}
		modelIDs = append(modelIDs, modelID)
	}
	return modelIDs
}
