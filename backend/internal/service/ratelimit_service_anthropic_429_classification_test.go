//go:build unit

package service

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// anthropic429RepoStub 记录账号级与模型级限流写入，用于验证 Anthropic 429 分类结果。
type anthropic429RepoStub struct {
	mockAccountRepoForGemini
	rateLimitCalls  int
	lastRateLimitID int64
	lastReset       time.Time

	modelLimitCalls int
	lastModelScope  string
	lastModelReset  time.Time
	lastModelReason string
}

func (r *anthropic429RepoStub) SetRateLimited(_ context.Context, id int64, resetAt time.Time) error {
	r.rateLimitCalls++
	r.lastRateLimitID = id
	r.lastReset = resetAt
	return nil
}

func (r *anthropic429RepoStub) SetModelRateLimit(_ context.Context, id int64, scope string, resetAt time.Time, reason ...string) error {
	r.modelLimitCalls++
	r.lastRateLimitID = id
	r.lastModelScope = scope
	r.lastModelReset = resetAt
	if len(reason) > 0 {
		r.lastModelReason = reason[0]
	}
	return nil
}

func newAnthropic429TestService(repo *anthropic429RepoStub) *RateLimitService {
	return NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
}

func requireResetWithin(t *testing.T, got time.Time, before, after time.Time, d time.Duration) {
	t.Helper()
	require.False(t, got.Before(before.Add(d)), "reset %v earlier than %v+%v", got, before, d)
	require.False(t, got.After(after.Add(d)), "reset %v later than %v+%v", got, after, d)
}

func TestHandle429_AnthropicExtraUsageRequired_MarksModelLevelOnly(t *testing.T) {
	repo := &anthropic429RepoStub{}
	svc := newAnthropic429TestService(repo)
	account := &Account{ID: 501, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	body := []byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Extra usage is required for long context requests."}}`)

	before := time.Now()
	svc.handle429(context.Background(), account, http.Header{}, body, "claude-opus-4-6[1m]")
	after := time.Now()

	require.Zero(t, repo.rateLimitCalls, "extra usage gate must not park the whole account")
	require.Equal(t, 1, repo.modelLimitCalls)
	require.Equal(t, int64(501), repo.lastRateLimitID)
	require.Equal(t, "claude-opus-4-6[1m]", repo.lastModelScope)
	require.Equal(t, "anthropic_extra_usage_required", repo.lastModelReason)
	requireResetWithin(t, repo.lastModelReset, before, after, anthropicExtraUsageModelCooldown)
}

func TestHandle429_AnthropicExtraUsageRequired_NoModelFallsBackToShortCooldown(t *testing.T) {
	repo := &anthropic429RepoStub{}
	svc := newAnthropic429TestService(repo)
	account := &Account{ID: 502, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	body := []byte(`{"error":{"type":"rate_limit_error","message":"Extra usage required"}}`)

	before := time.Now()
	svc.handle429(context.Background(), account, http.Header{}, body, "")
	after := time.Now()

	require.Zero(t, repo.modelLimitCalls)
	require.Equal(t, 1, repo.rateLimitCalls)
	requireResetWithin(t, repo.lastReset, before, after, time.Duration(defaultRateLimit429CooldownSeconds)*time.Second)
}

func TestHandle429_AnthropicCountTokensEndpointSkipsAccountPenalty(t *testing.T) {
	repo := &anthropic429RepoStub{}
	svc := newAnthropic429TestService(repo)
	account := &Account{ID: 503, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	headers := http.Header{}
	headers.Set("Retry-After", "7")
	body := []byte(`{"error":{"type":"rate_limit_error","message":"This request would exceed the rate limit for your organization"}}`)

	svc.handle429(WithCountTokensEndpoint(context.Background()), account, headers, body, "claude-sonnet-4-5")

	require.Zero(t, repo.rateLimitCalls, "count_tokens 429 must not mark the account")
	require.Zero(t, repo.modelLimitCalls)
}

func TestHandle429_AnthropicHTMLBodySkipsAccountPenalty(t *testing.T) {
	repo := &anthropic429RepoStub{}
	svc := newAnthropic429TestService(repo)
	account := &Account{ID: 504, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	headers := http.Header{}
	headers.Set("cf-mitigated", "challenge")
	body := []byte(`<!DOCTYPE html><html><head><title>Too Many Requests</title></head><body>rate limited</body></html>`)

	svc.handle429(context.Background(), account, headers, body, "claude-sonnet-4-5")

	require.Zero(t, repo.rateLimitCalls, "edge HTML 429 must not mark the account")
	require.Zero(t, repo.modelLimitCalls)
}

func TestHandle429_AnthropicNoResetHeadersUsesRetryAfter(t *testing.T) {
	repo := &anthropic429RepoStub{}
	svc := newAnthropic429TestService(repo)
	account := &Account{ID: 505, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	headers := http.Header{}
	headers.Set("Retry-After", "30")
	body := []byte(`{"error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit. Please try again later."}}`)

	before := time.Now()
	svc.handle429(context.Background(), account, headers, body, "claude-sonnet-4-5")
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.Equal(t, int64(505), repo.lastRateLimitID)
	requireResetWithin(t, repo.lastReset, before, after, 30*time.Second)
}

func TestHandle429_AnthropicRetryAfterIsClamped(t *testing.T) {
	repo := &anthropic429RepoStub{}
	svc := newAnthropic429TestService(repo)
	account := &Account{ID: 506, Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	headers := http.Header{}
	headers.Set("Retry-After", "600")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`), "claude-sonnet-4-5")
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	requireResetWithin(t, repo.lastReset, before, after, maxAnthropic429RetryAfter)
}

func TestHandle429_AnthropicBurstWithUnifiedHeadersUsesShortCooldown(t *testing.T) {
	repo := &anthropic429RepoStub{}
	svc := newAnthropic429TestService(repo)
	account := &Account{ID: 507, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	fiveHourReset := time.Now().Add(3 * time.Hour).Unix()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-5h-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.42")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(fiveHourReset, 10))
	headers.Set("anthropic-ratelimit-unified-7d-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.11")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(time.Now().Add(72*time.Hour).Unix(), 10))
	body := []byte(`{"error":{"type":"rate_limit_error","message":"Server is temporarily limiting requests (not your usage limit)"}}`)

	before := time.Now()
	svc.handle429(context.Background(), account, headers, body, "claude-sonnet-4-5")
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.True(t, repo.lastReset.Before(time.Unix(fiveHourReset, 0).Add(-time.Hour)), "burst 429 must not park the account until the 5h window boundary")
	requireResetWithin(t, repo.lastReset, before, after, time.Duration(defaultRateLimit429CooldownSeconds)*time.Second)
}

func TestHandle429_AnthropicBurstHonorsRetryAfter(t *testing.T) {
	repo := &anthropic429RepoStub{}
	svc := newAnthropic429TestService(repo)
	account := &Account{ID: 508, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.42")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(time.Now().Add(3*time.Hour).Unix(), 10))
	headers.Set("Retry-After", "12")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"slow down"}}`), "claude-sonnet-4-5")
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	requireResetWithin(t, repo.lastReset, before, after, 12*time.Second)
}

func TestHandle429_AnthropicWindowExhaustedStillUsesWindowReset(t *testing.T) {
	repo := &anthropic429RepoStub{}
	svc := newAnthropic429TestService(repo)
	account := &Account{ID: 509, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	fiveHourReset := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.0")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(fiveHourReset.Unix(), 10))
	headers.Set("Retry-After", "5")

	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit."}}`), "claude-sonnet-4-5")

	require.Equal(t, 1, repo.rateLimitCalls)
	require.True(t, repo.lastReset.Equal(fiveHourReset), "window exhaustion keeps the window reset, got %v want %v", repo.lastReset, fiveHourReset)
}

func TestCalculateAnthropic429ResetTime_NeitherExceeded_StatusRejected_UsesShorter(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.95")
	headers.Set("anthropic-ratelimit-unified-5h-reset", "1770998400") // sooner
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.80")
	headers.Set("anthropic-ratelimit-unified-7d-reset", "1771549200") // later

	result := calculateAnthropic429ResetTime(headers)
	assertAnthropicResult(t, result, 1770998400)
}

func TestIsAnthropicBurst429(t *testing.T) {
	require.False(t, isAnthropicBurst429(http.Header{}), "no unified headers is not a burst signal")

	burst := http.Header{}
	burst.Set("anthropic-ratelimit-unified-status", "allowed")
	burst.Set("anthropic-ratelimit-unified-5h-utilization", "0.3")
	burst.Set("anthropic-ratelimit-unified-5h-reset", "1770998400")
	require.True(t, isAnthropicBurst429(burst))

	exhausted := http.Header{}
	exhausted.Set("anthropic-ratelimit-unified-5h-utilization", "1.0")
	exhausted.Set("anthropic-ratelimit-unified-5h-reset", "1770998400")
	require.False(t, isAnthropicBurst429(exhausted))

	rejected := http.Header{}
	rejected.Set("anthropic-ratelimit-unified-5h-status", "rejected")
	rejected.Set("anthropic-ratelimit-unified-5h-reset", "1770998400")
	require.False(t, isAnthropicBurst429(rejected))
}

func TestIsAnthropicExtraUsageRequired(t *testing.T) {
	require.True(t, isAnthropicExtraUsageRequired([]byte(`{"error":{"type":"rate_limit_error","message":"Extra usage is required for long context requests."}}`)))
	require.True(t, isAnthropicExtraUsageRequired([]byte(`{"error":{"type":"rate_limit_error","message":"EXTRA USAGE required"}}`)))
	require.False(t, isAnthropicExtraUsageRequired([]byte(`{"error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit."}}`)))
	require.False(t, isAnthropicExtraUsageRequired(nil))
}
