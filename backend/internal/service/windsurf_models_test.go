package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchWindsurfAvailableModelIDsParsesOpenAIModelList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-windsurf-test" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"claude-opus-4-7-xhigh"},{"id":"gpt-5-5-xhigh-priority"},{"id":"gpt-5-5-xhigh-priority"}]}`))
	}))
	defer server.Close()

	account := &Account{
		Platform: PlatformWindsurf,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-windsurf-test",
			"base_url": server.URL,
		},
	}

	modelIDs, err := FetchWindsurfAvailableModelIDs(context.Background(), account, server.Client())
	if err != nil {
		t.Fatalf("FetchWindsurfAvailableModelIDs error = %v", err)
	}
	assertStringSlicesEqual(t, modelIDs, []string{"claude-opus-4-7-xhigh", "gpt-5-5-xhigh-priority"})
}

func TestFetchWindsurfAvailableModelIDsParsesModelsArray(t *testing.T) {
	modelIDs, err := parseWindsurfModelListBody([]byte(`{"models":["claude-opus-4-7-max",{"model_uid":"gpt-5-5-high"}]}`))
	if err != nil {
		t.Fatalf("parseWindsurfModelListBody error = %v", err)
	}
	assertStringSlicesEqual(t, modelIDs, []string{"claude-opus-4-7-max", "gpt-5-5-high"})
}
