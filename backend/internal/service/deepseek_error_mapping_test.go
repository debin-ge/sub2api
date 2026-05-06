package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestGatewayServiceHandleDeepSeekUpstreamErrorPersistsDegradation(t *testing.T) {
	repo := &miniMaxDegradeAccountRepo{}
	cfg := &config.Config{RateLimit: config.RateLimitConfig{OverloadCooldownMinutes: 7}}
	gateway := &GatewayService{
		accountRepo:      repo,
		rateLimitService: NewRateLimitService(repo, nil, cfg, nil, nil),
		cfg:              cfg,
	}
	account := &Account{ID: 401, Platform: PlatformDeepSeek, Type: AccountTypeAPIKey}

	beforeRateLimit := time.Now()
	gateway.HandleDeepSeekUpstreamError(context.Background(), account, &UpstreamFailoverError{
		StatusCode:   http.StatusTooManyRequests,
		ResponseBody: []byte(`{"error":{"message":"rate limited"}}`),
	})
	if repo.rateLimitedID != account.ID {
		t.Fatalf("rate limited account id = %d", repo.rateLimitedID)
	}
	if !repo.resetAt.After(beforeRateLimit) {
		t.Fatalf("rate limit reset should be in the future, got %s", repo.resetAt)
	}

	beforeOverload := time.Now()
	gateway.HandleDeepSeekUpstreamError(context.Background(), account, &UpstreamFailoverError{StatusCode: http.StatusServiceUnavailable})
	if repo.overloadedID != account.ID {
		t.Fatalf("overloaded account id = %d", repo.overloadedID)
	}
	if !repo.overloadUntil.After(beforeOverload.Add(6 * time.Minute)) {
		t.Fatalf("overload until should use configured cooldown, got %s", repo.overloadUntil)
	}
}

func TestGatewayServiceHandleDeepSeekUpstreamErrorMarksAuthAndBalanceFailures(t *testing.T) {
	repo := &miniMaxDegradeAccountRepo{}
	cfg := &config.Config{}
	gateway := &GatewayService{
		accountRepo:      repo,
		rateLimitService: NewRateLimitService(repo, nil, cfg, nil, nil),
		cfg:              cfg,
	}
	account := &Account{ID: 402, Platform: PlatformDeepSeek, Type: AccountTypeAPIKey}

	gateway.HandleDeepSeekUpstreamError(context.Background(), account, &UpstreamFailoverError{
		StatusCode:   http.StatusUnauthorized,
		ResponseBody: []byte(`{"error":{"message":"bad api key"}}`),
	})
	if repo.errorID != account.ID {
		t.Fatalf("auth error account id = %d, want %d", repo.errorID, account.ID)
	}
	if repo.errorMsg == "" {
		t.Fatalf("expected auth failure to persist an account error")
	}

	repo.errorID = 0
	repo.errorMsg = ""
	gateway.HandleDeepSeekUpstreamError(context.Background(), account, &UpstreamFailoverError{
		StatusCode:   http.StatusPaymentRequired,
		ResponseBody: []byte(`{"error":{"message":"insufficient balance"}}`),
	})
	if repo.errorID != account.ID {
		t.Fatalf("balance error account id = %d, want %d", repo.errorID, account.ID)
	}
	if repo.errorMsg == "" {
		t.Fatalf("expected balance failure to persist an account error")
	}
}
