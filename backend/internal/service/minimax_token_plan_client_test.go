package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestMiniMaxTokenPlanClientFetchRemainsSanitizesNon2xxErrorBody(t *testing.T) {
	longBody := "upstream failed\n" + strings.Repeat("x", 600)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, longBody, http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewMiniMaxTokenPlanClient(srv.URL, srv.Client())
	remains, err := client.FetchRemains(context.Background(), "sk-cp-test")
	if err == nil {
		t.Fatalf("expected status error")
	}
	if remains != nil {
		t.Fatalf("expected no remains on error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "minimax remains status 500") {
		t.Fatalf("error = %q", msg)
	}
	if strings.Contains(msg, "\n") {
		t.Fatalf("expected sanitized error without newlines: %q", msg)
	}
	if strings.Contains(msg, longBody) {
		t.Fatalf("expected truncated error body")
	}
	if !strings.Contains(msg, "...") {
		t.Fatalf("expected truncation marker in error: %q", msg)
	}
}

func TestMiniMaxTokenPlanClientFetchRemainsRejectsNonZeroBodyCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1001,"message":"quota exhausted","data":{"text_5h_limit":4500,"text_5h_remaining":3200}}`))
	}))
	defer srv.Close()

	client := NewMiniMaxTokenPlanClient(srv.URL, srv.Client())
	remains, err := client.FetchRemains(context.Background(), "sk-cp-test")
	if err == nil {
		t.Fatalf("expected body code error")
	}
	if remains != nil {
		t.Fatalf("expected no remains on error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "minimax remains code 1001") {
		t.Fatalf("error = %q", msg)
	}
	if !strings.Contains(msg, "quota exhausted") {
		t.Fatalf("error = %q", msg)
	}
}

func TestMiniMaxTokenPlanClientRejectsMissingAPIKey(t *testing.T) {
	client := NewMiniMaxTokenPlanClient("https://www.minimax.io", http.DefaultClient)
	if _, err := client.FetchRemains(context.Background(), " "); err == nil {
		t.Fatalf("expected missing key error")
	}
}
