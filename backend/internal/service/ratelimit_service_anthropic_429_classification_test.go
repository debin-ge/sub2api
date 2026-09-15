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
	now := time.Now()
	reset5h := now.Add(3 * time.Hour).Truncate(time.Second)      // sooner
	reset7d := now.Add(5 * 24 * time.Hour).Truncate(time.Second) // later

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.95")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.80")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(reset7d.Unix(), 10))

	result := calculateAnthropic429ResetTime(headers, now)
	assertAnthropicResult(t, result, reset5h.Unix())
}

// TestHandle429_AnthropicWindowExhaustedGarbageReset_FallsBackToShortCooldown 复现本次
// 生产事故：7d 窗口被 utilization=1.0 标记为耗尽，但 7d-reset 是一个越界的异常值
// （曾经把账号误锁 18 天以上）。修复后必须退化为按 Retry-After 的短时冷却，而不是
// 把异常值原样写入 rate_limit_reset_at。
func TestHandle429_AnthropicWindowExhaustedGarbageReset_FallsBackToShortCooldown(t *testing.T) {
	repo := &anthropic429RepoStub{}
	svc := newAnthropic429TestService(repo)
	account := &Account{ID: 510, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.0")
	headers.Set("anthropic-ratelimit-unified-7d-reset", "99999999999999") // 越界的异常值（本次事故复现）
	headers.Set("Retry-After", "20")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"This request would exceed your account's rate limit."}}`), "claude-sonnet-4-5")
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	require.True(t, repo.lastReset.Before(before.Add(maxAnthropic429RetryAfter+time.Second)), "must not be parked until the out-of-range reset value, got %v", repo.lastReset)
	requireResetWithin(t, repo.lastReset, before, after, 20*time.Second)
}

// TestHandle429_AnthropicAggregateResetOutOfRange_UsesRetryAfterFallback 覆盖 handle429
// 聚合头兜底分支（无 per-window 头，仅有越界的聚合 anthropic-ratelimit-unified-reset）。
func TestHandle429_AnthropicAggregateResetOutOfRange_UsesRetryAfterFallback(t *testing.T) {
	repo := &anthropic429RepoStub{}
	svc := newAnthropic429TestService(repo)
	account := &Account{ID: 511, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", "99999999999999") // 越界的聚合 reset
	headers.Set("Retry-After", "15")

	before := time.Now()
	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"rate limited"}}`), "claude-sonnet-4-5")
	after := time.Now()

	require.Equal(t, 1, repo.rateLimitCalls)
	requireResetWithin(t, repo.lastReset, before, after, 15*time.Second)
}

// TestHandle429_AnthropicAggregateResetValid_StillPersisted 确认聚合头兜底分支改成调用
// parseAnthropicAggregateReset 后，合法值仍然正常持久化（非回归）。
func TestHandle429_AnthropicAggregateResetValid_StillPersisted(t *testing.T) {
	repo := &anthropic429RepoStub{}
	svc := newAnthropic429TestService(repo)
	account := &Account{ID: 512, Platform: PlatformAnthropic, Type: AccountTypeOAuth}

	reset := time.Now().Add(2 * time.Hour).Truncate(time.Second)
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(reset.Unix(), 10))

	svc.handle429(context.Background(), account, headers, []byte(`{"error":{"type":"rate_limit_error","message":"rate limited"}}`), "claude-sonnet-4-5")

	require.Equal(t, 1, repo.rateLimitCalls)
	require.True(t, repo.lastReset.Equal(reset), "expected resetAt=%v, got %v", reset, repo.lastReset)
}

func TestIsAnthropicBurst429(t *testing.T) {
	require.False(t, isAnthropicBurst429(http.Header{}), "no unified headers is not a burst signal")

	aggregateOnly := http.Header{}
	aggregateOnly.Set("anthropic-ratelimit-unified-reset", "1770998400")
	require.False(t, isAnthropicBurst429(aggregateOnly), "an aggregate reset without status keeps legacy window handling")

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
