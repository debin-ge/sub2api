package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// runwareModelIndexURL 是 Runware 公开的模型索引（无需 key），每项带 AIR ID 与能力列表。
const runwareModelIndexURL = "https://runware.ai/docs/models/index.json"

// buildRunwareUpstreamModelsRequest 拉取 Runware 公开索引；不走账号 base_url，因为索引
// 不在 API 域名上。账号仍须是 apikey 类型，与其他平台的同步前提一致。
func (d *UpstreamModelDiscoverer) buildRunwareUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if account == nil || account.Type != AccountTypeAPIKey {
		return nil, newUpstreamModelSyncUnsupportedError("Unsupported Runware account for upstream model sync", nil)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, runwareModelIndexURL, nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid Runware model index URL", err)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// buildWaveSpeedUpstreamModelsRequest 调 WaveSpeed 的 GET {base}/models（需 key）。
func (d *UpstreamModelDiscoverer) buildWaveSpeedUpstreamModelsRequest(ctx context.Context, account *Account) (*http.Request, error) {
	if account == nil || account.Type != AccountTypeAPIKey {
		return nil, newUpstreamModelSyncUnsupportedError("Unsupported WaveSpeed account for upstream model sync", nil)
	}
	apiKey := strings.TrimSpace(account.GetCredential("api_key"))
	if apiKey == "" {
		return nil, newUpstreamModelSyncConfigError("No WaveSpeed API key is available", nil)
	}
	baseURL := strings.TrimSpace(account.GetCredential("base_url"))
	if baseURL == "" {
		baseURL = waveSpeedDefaultBaseURL
	}
	validatedBaseURL, err := d.validateUpstreamBaseURL(baseURL)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid WaveSpeed base URL", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, buildVendorURL(validatedBaseURL, "/models", nil), nil)
	if err != nil {
		return nil, newUpstreamModelSyncConfigError("Invalid WaveSpeed model list URL", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	account.ApplyHeaderOverrides(req.Header)
	return req, nil
}

type runwareModelIndexEntry struct {
	ID           string   `json:"id"`
	AIR          string   `json:"air"`
	Status       string   `json:"status"`
	Capabilities []string `json:"capabilities"`
}

// parseRunwareModelIndex 只保留在线、具备文生 / 图生视频能力的模型，返回 AIR ID。
func parseRunwareModelIndex(body []byte) ([]string, error) {
	var entries []runwareModelIndexEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		var wrapped struct {
			Models []runwareModelIndexEntry `json:"models"`
			Data   []runwareModelIndexEntry `json:"data"`
		}
		if wrappedErr := json.Unmarshal(body, &wrapped); wrappedErr != nil {
			return nil, fmt.Errorf("parse runware model index: %w", err)
		}
		entries = append(wrapped.Models, wrapped.Data...)
	}
	models := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.EqualFold(strings.TrimSpace(entry.Status), "deprecated") {
			continue
		}
		if !runwareEntryGeneratesVideo(entry.Capabilities) {
			continue
		}
		if air := strings.TrimSpace(entry.AIR); air != "" {
			models = append(models, air)
		}
	}
	return dedupeAndSortModelIDs(models), nil
}

func runwareEntryGeneratesVideo(capabilities []string) bool {
	for _, capability := range capabilities {
		normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(capability), "_", ""), "-", ""))
		switch normalized {
		case "texttovideo", "imagetovideo", "t2v", "i2v", "videoinference":
			return true
		}
	}
	return false
}

// parseWaveSpeedModelList 解析 GET /models 的 {code, data:[{model_id, type}]}，只保留视频类型。
func parseWaveSpeedModelList(body []byte) ([]string, error) {
	var response struct {
		Code int `json:"code"`
		Data []struct {
			ModelID string `json:"model_id"`
			Type    string `json:"type"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("parse wavespeed model list: %w", err)
	}
	models := make([]string, 0, len(response.Data))
	for _, entry := range response.Data {
		switch strings.ToLower(strings.TrimSpace(entry.Type)) {
		case "text-to-video", "image-to-video":
			if id := strings.TrimSpace(entry.ModelID); id != "" {
				models = append(models, id)
			}
		}
	}
	return dedupeAndSortModelIDs(models), nil
}
