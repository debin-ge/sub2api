package service

import (
	"context"
	"fmt"
	"strings"
	"time"
)

type DeepSeekBalanceFetcher interface {
	FetchBalanceForAccount(ctx context.Context, account *Account) (*DeepSeekBalance, error)
}

type DeepSeekBalanceHealthService struct {
	accountRepo AccountRepository
	fetcher     DeepSeekBalanceFetcher
	now         func() time.Time
}

func NewDeepSeekBalanceHealthService(accountRepo AccountRepository, fetcher DeepSeekBalanceFetcher) *DeepSeekBalanceHealthService {
	return &DeepSeekBalanceHealthService{
		accountRepo: accountRepo,
		fetcher:     fetcher,
		now:         time.Now,
	}
}

func (s *DeepSeekBalanceHealthService) CheckAll(ctx context.Context) ([]DeepSeekBalance, error) {
	return s.CheckBatch(ctx, 0)
}

func (s *DeepSeekBalanceHealthService) CheckBatch(ctx context.Context, batchSize int) ([]DeepSeekBalance, error) {
	if s == nil || s.accountRepo == nil {
		return nil, fmt.Errorf("deepseek balance account repo unavailable")
	}
	accounts, err := s.accountRepo.ListByPlatform(ctx, PlatformDeepSeek)
	if err != nil {
		return nil, err
	}
	results := make([]DeepSeekBalance, 0, len(accounts))
	var firstErr error
	processed := 0
	for i := range accounts {
		account := accounts[i]
		if !account.IsDeepSeekAPIKey() {
			continue
		}
		if batchSize > 0 && processed >= batchSize {
			break
		}
		processed++
		result, err := s.CheckAccount(ctx, &account)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		results = append(results, *result)
	}
	return results, firstErr
}

func (s *DeepSeekBalanceHealthService) CheckAccount(ctx context.Context, account *Account) (*DeepSeekBalance, error) {
	if s == nil || s.accountRepo == nil || s.fetcher == nil {
		return nil, fmt.Errorf("deepseek balance health service unavailable")
	}
	if account == nil || !account.IsDeepSeekAPIKey() {
		return nil, fmt.Errorf("deepseek api key account is required")
	}
	checkedAt := s.now().UTC().Format(time.RFC3339)
	balance, err := s.fetcher.FetchBalanceForAccount(ctx, account)
	if err != nil {
		_ = s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
			"deepseek_balance_available":  false,
			"deepseek_balance_checked_at": checkedAt,
			"deepseek_balance_status":     "error",
			"deepseek_balance_error":      sanitizeDeepSeekBalanceError(err),
		})
		return nil, err
	}
	status := "ok"
	if !balance.Available {
		status = "unavailable"
	}
	if err := s.accountRepo.UpdateExtra(ctx, account.ID, map[string]any{
		"deepseek_balance_available":  balance.Available,
		"deepseek_balance_amount":     balance.Amount,
		"deepseek_balance_currency":   balance.Currency,
		"deepseek_balance_checked_at": checkedAt,
		"deepseek_balance_status":     status,
		"deepseek_balance_error":      "",
		"deepseek_balance_raw":        balance.Raw,
	}); err != nil {
		return nil, err
	}
	return balance, nil
}

func sanitizeDeepSeekBalanceError(err error) string {
	msg := strings.TrimSpace(err.Error())
	if msg == "" {
		return "unknown deepseek balance error"
	}
	return msg
}
