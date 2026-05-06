package service

import (
	"context"
	"io"
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
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Fatalf("Accept = %q", got)
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

func TestMiniMaxTokenPlanClientFetchRemainsParsesChinaModelRemains(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"model_remains":[
				{
					"model_name":"MiniMax-M*",
					"current_interval_total_count":4500,
					"current_interval_usage_count":4
				}
			],
			"base_resp":{"status_code":0,"status_msg":"success"}
		}`))
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
	if remains.Text5hRemaining != 4496 {
		t.Fatalf("Text5hRemaining = %d", remains.Text5hRemaining)
	}
}

func TestMiniMaxTokenPlanClientFetchRemainsForAccountDerivesChinaRegionBaseURL(t *testing.T) {
	var capturedURL string
	client := NewMiniMaxTokenPlanClient("", &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		capturedURL = req.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
				"model_remains":[{"model_name":"MiniMax-M*","current_interval_total_count":4500,"current_interval_usage_count":4}],
				"base_resp":{"status_code":0,"status_msg":"success"}
			}`)),
		}, nil
	})})
	account := &Account{
		Platform: PlatformMiniMax,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":            "sk-cp-test",
			"base_url_anthropic": "https://api.minimaxi.com/anthropic",
			"base_url_openai":    "https://api.minimaxi.com/v1",
		},
	}

	remains, err := client.FetchRemainsForAccount(context.Background(), account)
	if err != nil {
		t.Fatalf("FetchRemainsForAccount error = %v", err)
	}
	if capturedURL != "https://api.minimaxi.com/v1/token_plan/remains" {
		t.Fatalf("captured url = %q", capturedURL)
	}
	if remains.Text5hLimit != 4500 || remains.Text5hRemaining != 4496 {
		t.Fatalf("remains = %+v", remains)
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

func TestMiniMaxTokenPlanClientFetchRemainsRedactsAPIKeyFromNon2xxErrorBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "proxy echoed Authorization: Bearer sk-cp-test for key sk-cp-test", http.StatusBadGateway)
	}))
	defer srv.Close()

	client := NewMiniMaxTokenPlanClient(srv.URL, srv.Client())
	_, err := client.FetchRemains(context.Background(), "sk-cp-test")
	if err == nil {
		t.Fatalf("expected status error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "minimax remains status 502") {
		t.Fatalf("error = %q", msg)
	}
	if strings.Contains(msg, "sk-cp-test") || strings.Contains(msg, "Bearer sk-cp-test") {
		t.Fatalf("expected redacted api key in error: %q", msg)
	}
	if !strings.Contains(msg, "[REDACTED_API_KEY]") {
		t.Fatalf("expected redaction marker in error: %q", msg)
	}
}

func TestMiniMaxTokenPlanClientFetchRemainsRejectsNonZeroBaseRespStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"base_resp":{"status_code":2049,"status_msg":"invalid api key"}}`))
	}))
	defer srv.Close()

	client := NewMiniMaxTokenPlanClient(srv.URL, srv.Client())
	remains, err := client.FetchRemains(context.Background(), "sk-cp-test")
	if err == nil {
		t.Fatalf("expected base_resp status error")
	}
	if remains != nil {
		t.Fatalf("expected no remains on error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "minimax remains base_resp 2049") {
		t.Fatalf("error = %q", msg)
	}
	if !strings.Contains(msg, "invalid api key") {
		t.Fatalf("error = %q", msg)
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

func TestMiniMaxTokenPlanClientFetchRemainsRedactsAPIKeyFromBodyCodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1001,"msg":"invalid Bearer sk-cp-test for key sk-cp-test"}`))
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
	if strings.Contains(msg, "sk-cp-test") || strings.Contains(msg, "Bearer sk-cp-test") {
		t.Fatalf("expected redacted api key in error: %q", msg)
	}
	if !strings.Contains(msg, "[REDACTED_API_KEY]") {
		t.Fatalf("expected redaction marker in error: %q", msg)
	}
}

func TestMiniMaxTokenPlanClientRejectsMissingAPIKey(t *testing.T) {
	client := NewMiniMaxTokenPlanClient("https://www.minimax.io", http.DefaultClient)
	if _, err := client.FetchRemains(context.Background(), " "); err == nil {
		t.Fatalf("expected missing key error")
	}
}
