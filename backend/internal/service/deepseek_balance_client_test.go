package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDeepSeekBalanceClientFetchBalanceForAccount(t *testing.T) {
	var gotAuth string
	client := NewDeepSeekBalanceClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/user/balance" {
			t.Fatalf("path = %q, want /user/balance", r.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body: io.NopCloser(strings.NewReader(`{
			"is_available": true,
			"balance_infos": [
				{"currency":"USD","total_balance":"1.00"},
				{"currency":"CNY","total_balance":"10.50","granted_balance":"0.00","topped_up_balance":"10.50"}
			]
		}`)),
		}, nil
	})})
	balance, err := client.FetchBalanceForAccount(context.Background(), &Account{
		Platform: PlatformDeepSeek,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":         "sk-deepseek-test",
			"base_url_openai": DefaultDeepseekBaseURL,
		},
	})
	if err != nil {
		t.Fatalf("FetchBalanceForAccount error = %v", err)
	}
	if gotAuth != "Bearer sk-deepseek-test" {
		t.Fatalf("Authorization = %q", gotAuth)
	}
	if !balance.Available || balance.Amount != "10.50" || balance.Currency != "CNY" {
		t.Fatalf("balance = %+v", balance)
	}
}

func TestDeepSeekBalanceClientRejectsThirdPartyBeforeRequest(t *testing.T) {
	calls := 0
	client := NewDeepSeekBalanceClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return nil, context.DeadlineExceeded
	})})
	_, err := client.FetchBalanceForAccount(context.Background(), &Account{
		Platform: PlatformDeepSeek,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-third-party",
			"base_url": "https://relay.example/v1",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "third-party") {
		t.Fatalf("err = %v", err)
	}
	if calls != 0 {
		t.Fatalf("requests = %d, want 0", calls)
	}
}

func TestDeepSeekBalanceClientRejectsMissingBalanceSchema(t *testing.T) {
	client := NewDeepSeekBalanceClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"object":"list","data":[]}`)),
		}, nil
	})})
	_, err := client.FetchBalanceForAccount(context.Background(), &Account{
		Platform: PlatformDeepSeek,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":  "sk-official",
			"base_url": DefaultDeepseekBaseURL,
		},
	})
	if err == nil || !strings.Contains(err.Error(), "missing valid balance_infos") {
		t.Fatalf("err = %v", err)
	}
}
