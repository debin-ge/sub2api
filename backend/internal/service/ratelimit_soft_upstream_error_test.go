package service

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type softUpstreamErrorRepo struct {
	AccountRepository
	setErrorCalls       int
	tempUnschedCalls    int
	rateLimitedCalls    int
	overloadedCalls     int
	modelRateLimitCalls int
}

func (r *softUpstreamErrorRepo) SetError(_ context.Context, _ int64, _ string) error {
	r.setErrorCalls++
	return nil
}

func (r *softUpstreamErrorRepo) SetTempUnschedulable(_ context.Context, _ int64, _ time.Time, _ string) error {
	r.tempUnschedCalls++
	return nil
}

func (r *softUpstreamErrorRepo) SetRateLimited(_ context.Context, _ int64, _ time.Time) error {
	r.rateLimitedCalls++
	return nil
}

func (r *softUpstreamErrorRepo) SetOverloaded(_ context.Context, _ int64, _ time.Time) error {
	r.overloadedCalls++
	return nil
}

func (r *softUpstreamErrorRepo) SetModelRateLimit(_ context.Context, _ int64, _ string, _ time.Time, _ ...string) error {
	r.modelRateLimitCalls++
	return nil
}

func (r *softUpstreamErrorRepo) stateMutations() int {
	return r.setErrorCalls + r.tempUnschedCalls + r.rateLimitedCalls + r.overloadedCalls + r.modelRateLimitCalls
}

func softUpstreamErrorMetricCount(platform string, statusCode int) uint64 {
	for _, metric := range SoftUpstreamErrorMetricsSnapshot() {
		if metric.Platform == platform && metric.StatusCode == statusCode {
			return metric.Count
		}
	}
	return 0
}

func TestRateLimitService_HandleUpstreamErrorSoft(t *testing.T) {
	newAccount := func() *Account {
		return &Account{
			ID:          7,
			Platform:    PlatformOpenAI,
			Type:        AccountTypeAPIKey,
			Status:      StatusActive,
			Schedulable: true,
			Credentials: map[string]any{"api_key": "sk-test"},
		}
	}
	headers := http.Header{"Retry-After": []string{"30"}}

	t.Run("429 and 5xx only observe", func(t *testing.T) {
		for _, statusCode := range []int{http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, 529} {
			repo := &softUpstreamErrorRepo{}
			svc := &RateLimitService{accountRepo: repo, cfg: &config.Config{}}
			before := softUpstreamErrorMetricCount(PlatformOpenAI, statusCode)

			shouldDisable := svc.HandleUpstreamErrorSoft(context.Background(), newAccount(), statusCode, headers, []byte(`{"error":{"message":"busy"}}`), "gpt-5.4")

			require.False(t, shouldDisable, "status=%d", statusCode)
			require.Zero(t, repo.stateMutations(), "status=%d must not touch account state: %+v", statusCode, repo)
			require.Equal(t, before+1, softUpstreamErrorMetricCount(PlatformOpenAI, statusCode), "status=%d", statusCode)
		}
	})

	t.Run("non rate-limit errors still delegate to full handling", func(t *testing.T) {
		repo := &softUpstreamErrorRepo{}
		svc := &RateLimitService{accountRepo: repo, cfg: &config.Config{}}

		shouldDisable := svc.HandleUpstreamErrorSoft(context.Background(), newAccount(), http.StatusUnauthorized, http.Header{}, []byte(`{"error":{"message":"Incorrect API key provided"}}`))

		require.True(t, shouldDisable)
		require.Equal(t, 1, repo.setErrorCalls, "401 on an API key account must still disable it")
	})
}
