package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeepSeekBalanceClientFetchBalanceForAccount(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/user/balance" {
			t.Fatalf("path = %q, want /user/balance", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"is_available": true,
			"balance_infos": [
				{"currency":"USD","total_balance":"1.00"},
				{"currency":"CNY","total_balance":"10.50","granted_balance":"0.00","topped_up_balance":"10.50"}
			]
		}`))
	}))
	t.Cleanup(srv.Close)

	client := NewDeepSeekBalanceClient(srv.Client())
	balance, err := client.FetchBalanceForAccount(context.Background(), &Account{
		Platform: PlatformDeepSeek,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":         "sk-deepseek-test",
			"base_url_openai": srv.URL,
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
