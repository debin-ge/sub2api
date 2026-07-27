package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Slot    string `json:"slot"`
}

func (a *app) healthProbe(ctx context.Context, port int) (healthResponse, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
			DialContext: (&net.Dialer{
				Timeout: 3 * time.Second,
			}).DialContext,
		},
	}
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return healthResponse{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return healthResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return healthResponse{}, fmt.Errorf("%s 返回 HTTP %d", url, response.StatusCode)
	}
	var health healthResponse
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(&health); err != nil {
		return healthResponse{}, fmt.Errorf("解析 %s 响应: %w", url, err)
	}
	return health, nil
}

func (a *app) waitForHealth(ctx context.Context, site resolvedSite, slot string, port int) (healthResponse, error) {
	deadline := time.NewTimer(time.Duration(site.HealthTimeoutSeconds) * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Duration(site.HealthIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		health, err := a.healthProbe(ctx, port)
		if err == nil {
			return health, nil
		}
		select {
		case <-ctx.Done():
			return healthResponse{}, ctx.Err()
		case <-deadline.C:
			return healthResponse{}, fmt.Errorf("健康门禁超时（%ds）", site.HealthTimeoutSeconds)
		case <-ticker.C:
		}
	}
}
