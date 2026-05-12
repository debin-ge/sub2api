package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

type deepSeekBalanceRepoStub struct {
	AccountRepository

	accounts      []Account
	updateID      int64
	updatePayload map[string]any
}

func (r *deepSeekBalanceRepoStub) ListByPlatform(ctx context.Context, platform string) ([]Account, error) {
	var out []Account
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out, nil
}

func (r *deepSeekBalanceRepoStub) UpdateExtra(ctx context.Context, id int64, updates map[string]any) error {
	r.updateID = id
	r.updatePayload = updates
	return nil
}

type deepSeekBalanceFetcherStub struct {
	balance *DeepSeekBalance
	err     error
}

func (f deepSeekBalanceFetcherStub) FetchBalanceForAccount(ctx context.Context, account *Account) (*DeepSeekBalance, error) {
	return f.balance, f.err
}

func TestDeepSeekBalanceHealthServiceCheckAccountUpdatesExtra(t *testing.T) {
	repo := &deepSeekBalanceRepoStub{}
	svc := NewDeepSeekBalanceHealthService(repo, deepSeekBalanceFetcherStub{balance: &DeepSeekBalance{
		Available: true,
		Amount:    "10.50",
		Currency:  "CNY",
		Raw:       map[string]any{"is_available": true},
	}})
	svc.now = func() time.Time { return time.Date(2026, 5, 12, 3, 4, 5, 0, time.UTC) }

	result, err := svc.CheckAccount(context.Background(), &Account{
		ID:          401,
		Platform:    PlatformDeepSeek,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-deepseek-test"},
	})
	if err != nil {
		t.Fatalf("CheckAccount error = %v", err)
	}
	if !result.Available || result.Amount != "10.50" || result.Currency != "CNY" {
		t.Fatalf("result = %+v", result)
	}
	assertExtraValue(t, repo.updatePayload, "deepseek_balance_available", true)
	assertExtraValue(t, repo.updatePayload, "deepseek_balance_amount", "10.50")
	assertExtraValue(t, repo.updatePayload, "deepseek_balance_currency", "CNY")
	assertExtraValue(t, repo.updatePayload, "deepseek_balance_checked_at", "2026-05-12T03:04:05Z")
	assertExtraValue(t, repo.updatePayload, "deepseek_balance_status", "ok")
}

func TestDeepSeekBalanceHealthServiceCheckAccountStoresError(t *testing.T) {
	repo := &deepSeekBalanceRepoStub{}
	svc := NewDeepSeekBalanceHealthService(repo, deepSeekBalanceFetcherStub{err: errors.New("upstream down")})
	svc.now = func() time.Time { return time.Date(2026, 5, 12, 3, 4, 5, 0, time.UTC) }

	_, err := svc.CheckAccount(context.Background(), &Account{
		ID:          401,
		Platform:    PlatformDeepSeek,
		Type:        AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "sk-deepseek-test"},
	})
	if err == nil {
		t.Fatalf("expected check error")
	}
	assertExtraValue(t, repo.updatePayload, "deepseek_balance_available", false)
	assertExtraValue(t, repo.updatePayload, "deepseek_balance_status", "error")
	assertExtraValue(t, repo.updatePayload, "deepseek_balance_error", "upstream down")
	assertExtraValue(t, repo.updatePayload, "deepseek_balance_checked_at", "2026-05-12T03:04:05Z")
}
