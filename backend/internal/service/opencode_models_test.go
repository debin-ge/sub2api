package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchOpenCodeAvailableModelIDsParsesOpenAIModelList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-opencode-test" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"opencode/gpt5-nano"},{"id":"opencode/gpt5-high"},{"id":"opencode/gpt5-high"}]}`))
	}))
	defer server.Close()

	account := &Account{
		Platform: PlatformOpenCode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-opencode-test",
			"base_url": server.URL,
		},
	}

	modelIDs, err := FetchOpenCodeAvailableModelIDs(context.Background(), account, server.Client())
	if err != nil {
		t.Fatalf("FetchOpenCodeAvailableModelIDs error = %v", err)
	}
	assertStringSlicesEqual(t, modelIDs, []string{"opencode/gpt5-nano", "opencode/gpt5-high"})
}

func TestFetchOpenCodeAvailableModelIDsAvoidsDuplicatingV1BasePath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %q, want /v1/models", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"models":["opencode/big-pickle",{"model_uid":"gpt5-nano"}]}`))
	}))
	defer server.Close()

	account := &Account{
		Platform: PlatformOpenCode,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-opencode-test",
			"base_url": server.URL + "/v1",
		},
	}

	modelIDs, err := FetchOpenCodeAvailableModelIDs(context.Background(), account, server.Client())
	if err != nil {
		t.Fatalf("FetchOpenCodeAvailableModelIDs error = %v", err)
	}
	assertStringSlicesEqual(t, modelIDs, []string{"opencode/big-pickle", "gpt5-nano"})
}
