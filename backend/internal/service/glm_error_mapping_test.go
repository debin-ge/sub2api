package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
)

func TestGatewayServiceHandleGLMUpstreamErrorPersistsDegradation(t *testing.T) {
	repo := &miniMaxDegradeAccountRepo{}
	cfg := &config.Config{RateLimit: config.RateLimitConfig{OverloadCooldownMinutes: 7}}
	gateway := &GatewayService{
		accountRepo:      repo,
		rateLimitService: NewRateLimitService(repo, nil, cfg, nil, nil),
		cfg:              cfg,
	}
	account := &Account{ID: 101, Platform: PlatformGLM, Type: AccountTypeAPIKey}

	beforeRateLimit := time.Now()
	gateway.HandleGLMUpstreamError(context.Background(), account, &UpstreamFailoverError{
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
	gateway.HandleGLMUpstreamError(context.Background(), account, &UpstreamFailoverError{StatusCode: http.StatusInternalServerError})
	if repo.overloadedID != account.ID {
		t.Fatalf("overloaded account id = %d", repo.overloadedID)
	}
	if !repo.overloadUntil.After(beforeOverload.Add(6 * time.Minute)) {
		t.Fatalf("overload until should use configured cooldown, got %s", repo.overloadUntil)
	}
}
