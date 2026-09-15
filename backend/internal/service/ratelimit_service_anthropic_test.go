package service

import (
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestCalculateAnthropic429ResetTime_Only5hExceeded(t *testing.T) {
	now := time.Now()
	reset5h := now.Add(3 * time.Hour).Truncate(time.Second)
	reset7d := now.Add(5 * 24 * time.Hour).Truncate(time.Second)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.02")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.32")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(reset7d.Unix(), 10))

	result := calculateAnthropic429ResetTime(headers, now)
	assertAnthropicResult(t, result, reset5h.Unix())

	if result.fiveHourReset == nil || !result.fiveHourReset.Equal(reset5h) {
		t.Errorf("expected fiveHourReset=%v, got %v", reset5h, result.fiveHourReset)
	}
}

func TestCalculateAnthropic429ResetTime_Only7dExceeded(t *testing.T) {
	now := time.Now()
	reset5h := now.Add(3 * time.Hour).Truncate(time.Second)
	reset7d := now.Add(5 * 24 * time.Hour).Truncate(time.Second)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.50")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.05")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(reset7d.Unix(), 10))

	result := calculateAnthropic429ResetTime(headers, now)
	assertAnthropicResult(t, result, reset7d.Unix())

	// fiveHourReset should still be populated for session window calculation
	if result.fiveHourReset == nil || !result.fiveHourReset.Equal(reset5h) {
		t.Errorf("expected fiveHourReset=%v, got %v", reset5h, result.fiveHourReset)
	}
}

func TestCalculateAnthropic429ResetTime_BothExceeded(t *testing.T) {
	now := time.Now()
	reset5h := now.Add(3 * time.Hour).Truncate(time.Second)
	reset7d := now.Add(5 * 24 * time.Hour).Truncate(time.Second)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.10")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.02")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(reset7d.Unix(), 10))

	result := calculateAnthropic429ResetTime(headers, now)
	assertAnthropicResult(t, result, reset7d.Unix())
}

func TestCalculateAnthropic429ResetTime_BothExceeded_7dInvalid_FallsBackTo5h(t *testing.T) {
	now := time.Now()
	reset5h := now.Add(3 * time.Hour).Truncate(time.Second)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.10")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.02")
	headers.Set("anthropic-ratelimit-unified-7d-reset", "99999999999999") // 越界的异常值

	result := calculateAnthropic429ResetTime(headers, now)
	assertAnthropicResult(t, result, reset5h.Unix())
}

func TestCalculateAnthropic429ResetTime_BothExceeded_BothInvalid_ReturnsNil(t *testing.T) {
	now := time.Now()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.10")
	headers.Set("anthropic-ratelimit-unified-5h-reset", "99999999999999")
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.02")
	headers.Set("anthropic-ratelimit-unified-7d-reset", "99999999999999")

	if result := calculateAnthropic429ResetTime(headers, now); result != nil {
		t.Errorf("expected nil result when both windows are exceeded but both reset values are out of range, got %+v", result)
	}
}

func TestCalculateAnthropic429ResetTime_5hResetInPast_ReturnsNil(t *testing.T) {
	// 5h 被标记超限，但 reset 已经在过去（陈旧/重放的头）：不能采用，也不能借用
	// 未被标记超限的另一个窗口的 reset 时间。
	now := time.Now()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.05")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(now.Add(-time.Minute).Unix(), 10))

	if result := calculateAnthropic429ResetTime(headers, now); result != nil {
		t.Errorf("expected nil result when the exceeded window's reset is in the past, got %+v", result)
	}
}

func TestCalculateAnthropic429ResetTime_7dResetTooFarInFuture_ReturnsNil(t *testing.T) {
	// 7d 被标记超限，但 reset 超过 8 天上限（本次事故的复现场景：一个荒谬的
	// 远未来时间戳曾被无条件信任，导致账号被误锁 18 天以上）。
	now := time.Now()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.0")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(now.Add(30*24*time.Hour).Unix(), 10))

	if result := calculateAnthropic429ResetTime(headers, now); result != nil {
		t.Errorf("expected nil result when the exceeded window's reset exceeds the max age bound, got %+v", result)
	}
}

func TestCalculateAnthropic429ResetTime_MillisecondScaleReset_AutoCorrected(t *testing.T) {
	now := time.Now()
	reset5h := now.Add(2 * time.Hour).Truncate(time.Second)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.0")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.UnixMilli(), 10))

	result := calculateAnthropic429ResetTime(headers, now)
	assertAnthropicResult(t, result, reset5h.Unix())
}

func TestCalculateAnthropic429ResetTime_NoPerWindowHeaders(t *testing.T) {
	now := time.Now()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(now.Add(2*time.Hour).Unix(), 10))

	result := calculateAnthropic429ResetTime(headers, now)
	if result != nil {
		t.Errorf("expected nil result when no per-window headers, got resetAt=%v", result.resetAt)
	}
}

func TestCalculateAnthropic429ResetTime_NoHeaders(t *testing.T) {
	result := calculateAnthropic429ResetTime(http.Header{}, time.Now())
	if result != nil {
		t.Errorf("expected nil result for empty headers, got resetAt=%v", result.resetAt)
	}
}

func TestCalculateAnthropic429ResetTime_SurpassedThreshold(t *testing.T) {
	now := time.Now()
	reset5h := now.Add(3 * time.Hour).Truncate(time.Second)
	reset7d := now.Add(5 * 24 * time.Hour).Truncate(time.Second)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-surpassed-threshold", "true")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d-surpassed-threshold", "false")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(reset7d.Unix(), 10))

	result := calculateAnthropic429ResetTime(headers, now)
	assertAnthropicResult(t, result, reset5h.Unix())
}

func TestCalculateAnthropic429ResetTime_UtilizationExactlyOne(t *testing.T) {
	now := time.Now()
	reset5h := now.Add(3 * time.Hour).Truncate(time.Second)
	reset7d := now.Add(5 * 24 * time.Hour).Truncate(time.Second)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.0")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.5")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(reset7d.Unix(), 10))

	result := calculateAnthropic429ResetTime(headers, now)
	assertAnthropicResult(t, result, reset5h.Unix())
}

// Neither window exceeded and no explicit rejected status: the reset headers merely
// echo the account's window state for a burst/concurrency 429. Parking the account
// until the window boundary would turn a seconds-long throttle into hours, so the
// per-window calculation must yield nil and let Retry-After / fallback decide.
func TestCalculateAnthropic429ResetTime_NeitherExceeded_ReturnsNil(t *testing.T) {
	now := time.Now()
	reset5h := now.Add(3 * time.Hour).Truncate(time.Second)      // sooner
	reset7d := now.Add(5 * 24 * time.Hour).Truncate(time.Second) // later

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "0.95")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "0.80")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(reset7d.Unix(), 10))

	if result := calculateAnthropic429ResetTime(headers, now); result != nil {
		t.Fatalf("expected nil result when neither window is exceeded and status is not rejected, got %+v", result)
	}
}

func TestCalculateAnthropic429ResetTime_Only5hResetHeader(t *testing.T) {
	now := time.Now()
	reset5h := now.Add(3 * time.Hour).Truncate(time.Second)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-5h-utilization", "1.05")
	headers.Set("anthropic-ratelimit-unified-5h-reset", strconv.FormatInt(reset5h.Unix(), 10))

	result := calculateAnthropic429ResetTime(headers, now)
	assertAnthropicResult(t, result, reset5h.Unix())
}

func TestCalculateAnthropic429ResetTime_Only7dResetHeader(t *testing.T) {
	now := time.Now()
	reset7d := now.Add(5 * 24 * time.Hour).Truncate(time.Second)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d-utilization", "1.03")
	headers.Set("anthropic-ratelimit-unified-7d-reset", strconv.FormatInt(reset7d.Unix(), 10))

	result := calculateAnthropic429ResetTime(headers, now)
	assertAnthropicResult(t, result, reset7d.Unix())

	if result.fiveHourReset != nil {
		t.Errorf("expected fiveHourReset=nil when no 5h headers, got %v", result.fiveHourReset)
	}
}

func TestIsAnthropicWindowExceeded(t *testing.T) {
	tests := []struct {
		name     string
		headers  http.Header
		window   string
		expected bool
	}{
		{
			name:     "utilization above 1.0",
			headers:  makeHeader("anthropic-ratelimit-unified-5h-utilization", "1.02"),
			window:   "5h",
			expected: true,
		},
		{
			name:     "utilization exactly 1.0",
			headers:  makeHeader("anthropic-ratelimit-unified-5h-utilization", "1.0"),
			window:   "5h",
			expected: true,
		},
		{
			name:     "utilization below 1.0",
			headers:  makeHeader("anthropic-ratelimit-unified-5h-utilization", "0.99"),
			window:   "5h",
			expected: false,
		},
		{
			name:     "surpassed-threshold true",
			headers:  makeHeader("anthropic-ratelimit-unified-7d-surpassed-threshold", "true"),
			window:   "7d",
			expected: true,
		},
		{
			name:     "surpassed-threshold True (case insensitive)",
			headers:  makeHeader("anthropic-ratelimit-unified-7d-surpassed-threshold", "True"),
			window:   "7d",
			expected: true,
		},
		{
			name:     "surpassed-threshold false",
			headers:  makeHeader("anthropic-ratelimit-unified-7d-surpassed-threshold", "false"),
			window:   "7d",
			expected: false,
		},
		{
			name:     "no headers",
			headers:  http.Header{},
			window:   "5h",
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isAnthropicWindowExceeded(tc.headers, tc.window)
			if got != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestSelectAnthropicFableWindowLimit_RejectedStatus(t *testing.T) {
	now := time.Now()
	reset := now.Add(80 * time.Hour).Truncate(time.Second)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d_oi-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-7d_oi-utilization", "1.0")
	headers.Set("anthropic-ratelimit-unified-7d_oi-surpassed-threshold", "1.0")
	headers.Set("anthropic-ratelimit-unified-7d_oi-reset", strconv.FormatInt(reset.Unix(), 10))

	limit := selectAnthropicFableWindowLimit(headers, now)
	if limit == nil {
		t.Fatal("expected non-nil limit")
	}
	if !limit.resetAt.Equal(reset) {
		t.Errorf("expected resetAt=%v, got %v", reset, limit.resetAt)
	}
	if limit.reason != anthropicFableWindowReason {
		t.Errorf("expected reason=%q, got %q", anthropicFableWindowReason, limit.reason)
	}
}

func TestSelectAnthropicFableWindowLimit_UtilizationOnly(t *testing.T) {
	// 无 status 头时，utilization >= 1.0 也应视为超限
	now := time.Now()
	reset := now.Add(3 * 24 * time.Hour).Truncate(time.Second)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d_oi-utilization", "1.0")
	headers.Set("anthropic-ratelimit-unified-7d_oi-reset", strconv.FormatInt(reset.Unix(), 10))

	limit := selectAnthropicFableWindowLimit(headers, now)
	if limit == nil {
		t.Fatal("expected non-nil limit")
	}
	if !limit.resetAt.Equal(reset) {
		t.Errorf("expected resetAt=%v, got %v", reset, limit.resetAt)
	}
}

func TestSelectAnthropicFableWindowLimit_AllowedReturnsNil(t *testing.T) {
	now := time.Now()
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d_oi-status", "allowed")
	headers.Set("anthropic-ratelimit-unified-7d_oi-utilization", "0.56")
	headers.Set("anthropic-ratelimit-unified-7d_oi-reset", strconv.FormatInt(now.Add(80*time.Hour).Unix(), 10))

	if limit := selectAnthropicFableWindowLimit(headers, now); limit != nil {
		t.Errorf("expected nil limit for allowed window, got %+v", limit)
	}
}

func TestSelectAnthropicFableWindowLimit_NoHeadersReturnsNil(t *testing.T) {
	if limit := selectAnthropicFableWindowLimit(http.Header{}, time.Now()); limit != nil {
		t.Errorf("expected nil limit for empty headers, got %+v", limit)
	}
}

func TestSelectAnthropicFableWindowLimit_FallsBackToAggregateReset(t *testing.T) {
	// 7d_oi-reset 缺失时回退聚合 anthropic-ratelimit-unified-reset
	now := time.Now()
	reset := now.Add(80 * time.Hour).Truncate(time.Second)

	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d_oi-status", "rejected")
	headers.Set("anthropic-ratelimit-unified-reset", strconv.FormatInt(reset.Unix(), 10))

	limit := selectAnthropicFableWindowLimit(headers, now)
	if limit == nil {
		t.Fatal("expected non-nil limit via aggregate reset fallback")
	}
	if !limit.resetAt.Equal(reset) {
		t.Errorf("expected resetAt=%v, got %v", reset, limit.resetAt)
	}
}

func TestSelectAnthropicFableWindowLimit_RejectedWithoutAnyResetReturnsNil(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d_oi-status", "rejected")

	if limit := selectAnthropicFableWindowLimit(headers, time.Now()); limit != nil {
		t.Errorf("expected nil limit when no reset time available, got %+v", limit)
	}
}

func TestParseAnthropicAggregateReset(t *testing.T) {
	now := time.Now()
	future := now.Add(80 * time.Hour).Truncate(time.Second)

	tests := []struct {
		name   string
		value  string
		want   time.Time
		wantOK bool
	}{
		{"valid seconds", strconv.FormatInt(future.Unix(), 10), future, true},
		{"valid milliseconds", strconv.FormatInt(future.UnixMilli(), 10), future, true},
		{"empty", "", time.Time{}, false},
		{"garbage", "abc", time.Time{}, false},
		{"in the past", strconv.FormatInt(now.Add(-time.Hour).Unix(), 10), time.Time{}, false},
		{"too far in the future", strconv.FormatInt(now.Add(30*24*time.Hour).Unix(), 10), time.Time{}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			headers := http.Header{}
			if tc.value != "" {
				headers.Set("anthropic-ratelimit-unified-reset", tc.value)
			}
			got, ok := parseAnthropicAggregateReset(headers, now)
			if ok != tc.wantOK {
				t.Fatalf("expected ok=%v, got %v", tc.wantOK, ok)
			}
			if ok && !got.Equal(tc.want) {
				t.Errorf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestIsAnthropicWindowRejected(t *testing.T) {
	headers := http.Header{}
	headers.Set("anthropic-ratelimit-unified-7d_oi-status", "Rejected")
	headers.Set("anthropic-ratelimit-unified-5h-status", "allowed")

	if !isAnthropicWindowRejected(headers, "7d_oi") {
		t.Error("expected 7d_oi to be rejected (case insensitive)")
	}
	if isAnthropicWindowRejected(headers, "5h") {
		t.Error("expected 5h not rejected")
	}
	if isAnthropicWindowRejected(headers, "7d") {
		t.Error("expected missing 7d status not rejected")
	}
}

// assertAnthropicResult is a test helper that verifies the result is non-nil and
// has the expected resetAt unix timestamp.
func assertAnthropicResult(t *testing.T, result *anthropic429Result, wantUnix int64) {
	t.Helper()
	if result == nil {
		t.Fatal("expected non-nil result")
		return // unreachable, but satisfies staticcheck SA5011
	}
	want := time.Unix(wantUnix, 0)
	if !result.resetAt.Equal(want) {
		t.Errorf("expected resetAt=%v, got %v", want, result.resetAt)
	}
}

func makeHeader(key, value string) http.Header {
	h := http.Header{}
	h.Set(key, value)
	return h
}
