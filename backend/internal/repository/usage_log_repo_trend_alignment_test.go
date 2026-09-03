package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

// timezone.Init mutates process-global state (time.Local included), and this
// package's integration harness pins UTC. Restore whatever was set so these
// tests cannot leak Asia/Shanghai into the ones that run after them.
func useServerTimezone(t *testing.T, name string) {
	t.Helper()
	prev := timezone.Name()
	if err := timezone.Init(name); err != nil {
		t.Fatalf("Init(%q): %v", name, err)
	}
	t.Cleanup(func() {
		if prev == "" {
			return
		}
		if err := timezone.Init(prev); err != nil {
			t.Fatalf("restore timezone %q: %v", prev, err)
		}
	})
}

// The rollup tables hold whole buckets and are selected with
// `bucket_start >= $1`, so a window starting mid-bucket would silently lose its
// first partial bucket. Rolling windows (start_time/end_time) must therefore
// fall back to the raw usage_logs query.
func TestShouldUsePreaggregatedTrend_RequiresBucketAlignedWindow(t *testing.T) {
	useServerTimezone(t, "Asia/Shanghai")
	loc := timezone.Location()

	dayStart := time.Date(2026, 8, 27, 0, 0, 0, 0, loc)
	dayEnd := time.Date(2026, 8, 28, 0, 0, 0, 0, loc)
	hourStart := time.Date(2026, 8, 27, 14, 0, 0, 0, loc)
	hourEnd := time.Date(2026, 8, 28, 14, 0, 0, 0, loc)
	rollingStart := time.Date(2026, 8, 27, 14, 37, 0, 0, loc)
	rollingEnd := time.Date(2026, 8, 28, 14, 37, 0, 0, loc)

	cases := []struct {
		name        string
		start, end  time.Time
		granularity string
		want        bool
	}{
		{"day granularity, midnight aligned", dayStart, dayEnd, "day", true},
		{"hour granularity, hour aligned", hourStart, hourEnd, "hour", true},
		{"hour granularity, rolling window", rollingStart, rollingEnd, "hour", false},
		{"hour granularity, rolling start only", rollingStart, hourEnd, "hour", false},
		{"hour granularity, rolling end only", hourStart, rollingEnd, "hour", false},
		// An hour-aligned window is not day-aligned: the daily rollup keys on
		// whole dates, so 14:00 bounds cannot be served from it.
		{"day granularity, hour aligned only", hourStart, hourEnd, "day", false},
		{"unsupported granularity", dayStart, dayEnd, "minute", false},
		{"zero bounds", time.Time{}, time.Time{}, "day", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldUsePreaggregatedTrend(tc.start, tc.end, tc.granularity, 0, 0, 0, 0, "", nil, nil, nil, "", nil, nil)
			if got != tc.want {
				t.Errorf("shouldUsePreaggregatedTrend = %v, want %v", got, tc.want)
			}
		})
	}
}

// Alignment is judged in the server timezone, because that is what the
// aggregation job buckets on. A caller in a half-hour-offset zone sending a
// local midnight is not aligned to the server's buckets and must not be served
// from the rollup.
func TestIsBucketAligned_UsesServerTimezone(t *testing.T) {
	useServerTimezone(t, "Asia/Shanghai")

	kolkata, err := time.LoadLocation("Asia/Kolkata") // UTC+05:30
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	// Local midnight in Kolkata is 02:30 in Asia/Shanghai — neither hour- nor
	// day-aligned for the rollup.
	kolkataMidnight := time.Date(2026, 8, 28, 0, 0, 0, 0, kolkata)
	if isBucketAligned(kolkataMidnight, "hour") {
		t.Error("Kolkata midnight must not count as hour-aligned in Asia/Shanghai")
	}
	if isBucketAligned(kolkataMidnight, "day") {
		t.Error("Kolkata midnight must not count as day-aligned in Asia/Shanghai")
	}

	// The same instant expressed in a whole-hour-offset zone is hour-aligned:
	// alignment follows the instant, not the caller's label for it.
	utcHour := time.Date(2026, 8, 27, 6, 0, 0, 0, time.UTC) // 14:00 in Shanghai
	if !isBucketAligned(utcHour, "hour") {
		t.Error("14:00 Asia/Shanghai expressed as UTC must be hour-aligned")
	}
	if isBucketAligned(utcHour, "day") {
		t.Error("14:00 Asia/Shanghai must not be day-aligned")
	}

	// Sub-second drift breaks alignment too.
	if isBucketAligned(time.Date(2026, 8, 28, 0, 0, 0, 1, timezone.Location()), "day") {
		t.Error("a nanosecond past midnight must not be day-aligned")
	}
}
