package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiniMaxTokenPlanClientFetchRemainsUsesBearerAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-cp-test" {
			t.Fatalf("Authorization = %q", got)
		}
		if r.URL.Path != "/v1/token_plan/remains" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"text_5h_limit":4500,"text_5h_remaining":3200}}`))
	}))
	defer srv.Close()

	client := NewMiniMaxTokenPlanClient(srv.URL, srv.Client())
	remains, err := client.FetchRemains(context.Background(), "sk-cp-test")
	if err != nil {
		t.Fatalf("FetchRemains error = %v", err)
	}
	if remains.Text5hLimit != 4500 {
		t.Fatalf("Text5hLimit = %d", remains.Text5hLimit)
	}
	if remains.Text5hRemaining != 3200 {
		t.Fatalf("Text5hRemaining = %d", remains.Text5hRemaining)
	}
	if len(remains.Raw) == 0 {
		t.Fatalf("expected raw response to be retained")
	}
}

func TestMiniMaxTokenPlanClientFetchRemainsParsesStringNumericFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"text_5h_limit":"4500","text_5h_remaining":"3200"}}`))
	}))
	defer srv.Close()

	client := NewMiniMaxTokenPlanClient(srv.URL, srv.Client())
	remains, err := client.FetchRemains(context.Background(), "sk-cp-test")
	if err != nil {
		t.Fatalf("FetchRemains error = %v", err)
	}
	if remains.Text5hLimit != 4500 {
		t.Fatalf("Text5hLimit = %d", remains.Text5hLimit)
	}
	if remains.Text5hRemaining != 3200 {
		t.Fatalf("Text5hRemaining = %d", remains.Text5hRemaining)
	}
}

func TestMiniMaxTokenPlanClientRejectsMissingAPIKey(t *testing.T) {
	client := NewMiniMaxTokenPlanClient("https://www.minimax.io", http.DefaultClient)
	if _, err := client.FetchRemains(context.Background(), " "); err == nil {
		t.Fatalf("expected missing key error")
	}
}
