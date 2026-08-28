package timezone

import (
	"testing"
	"time"
)

// A calendar date names a whole day, so the window must be half-open
// [date 00:00, next day 00:00) — this is what makes "today" on the usage page
// cover exactly the same rows as the dashboard's "today" card.
func TestResolveRangeFromDates(t *testing.T) {
	if err := Init("UTC"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	start, err := ResolveRangeStart("", "2026-08-28", "Asia/Shanghai")
	if err != nil || start == nil {
		t.Fatalf("ResolveRangeStart: %v, %v", start, err)
	}
	end, err := ResolveRangeEnd("", "2026-08-28", "Asia/Shanghai")
	if err != nil || end == nil {
		t.Fatalf("ResolveRangeEnd: %v, %v", end, err)
	}

	if got := start.Format(time.RFC3339); got != "2026-08-28T00:00:00+08:00" {
		t.Errorf("start = %s, want 2026-08-28T00:00:00+08:00", got)
	}
	// End is the *next* midnight, not 23:59:59, so the window stays half-open.
	if got := end.Format(time.RFC3339); got != "2026-08-29T00:00:00+08:00" {
		t.Errorf("end = %s, want 2026-08-29T00:00:00+08:00", got)
	}
	if d := end.Sub(*start); d != 24*time.Hour {
		t.Errorf("window = %v, want 24h", d)
	}
}

// The whole point of start_time/end_time: a rolling window keeps its exact
// bounds instead of being widened to whole calendar days. Expressing "last 24
// hours" as two dates spans up to 48 hours.
func TestResolveRangeInstantsWinAndAreExact(t *testing.T) {
	if err := Init("UTC"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	start, err := ResolveRangeStart("2026-08-27T14:37:00+08:00", "2026-08-27", "Asia/Shanghai")
	if err != nil || start == nil {
		t.Fatalf("ResolveRangeStart: %v, %v", start, err)
	}
	end, err := ResolveRangeEnd("2026-08-28T14:37:00+08:00", "2026-08-28", "Asia/Shanghai")
	if err != nil || end == nil {
		t.Fatalf("ResolveRangeEnd: %v, %v", end, err)
	}

	if d := end.Sub(*start); d != 24*time.Hour {
		t.Errorf("rolling window = %v, want exactly 24h", d)
	}
	// The date params were present too; the instants must have won outright.
	if got := start.Format(time.RFC3339); got != "2026-08-27T14:37:00+08:00" {
		t.Errorf("start = %s, want the instant, not the date's midnight", got)
	}
	// An end instant is used verbatim — it must NOT be pushed to the next day.
	if got := end.Format(time.RFC3339); got != "2026-08-28T14:37:00+08:00" {
		t.Errorf("end = %s, want the instant verbatim", got)
	}
}

func TestResolveRangeEmptyAndInvalid(t *testing.T) {
	if err := Init("UTC"); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Neither form supplied: no bound, no error — callers apply their default.
	if got, err := ResolveRangeStart("", "", ""); got != nil || err != nil {
		t.Errorf("ResolveRangeStart(empty) = %v, %v; want nil, nil", got, err)
	}
	if got, err := ResolveRangeEnd("  ", "  ", ""); got != nil || err != nil {
		t.Errorf("ResolveRangeEnd(blank) = %v, %v; want nil, nil", got, err)
	}

	if _, err := ResolveRangeStart("", "2026-13-45", ""); err == nil {
		t.Error("ResolveRangeStart should reject a malformed date")
	}
	if _, err := ResolveRangeStart("not-a-timestamp", "", ""); err == nil {
		t.Error("ResolveRangeStart should reject a malformed instant")
	}
	// A date-shaped value in the instant slot is not RFC3339 and must be rejected
	// rather than silently falling through to the date branch.
	if _, err := ResolveRangeEnd("2026-08-28", "", ""); err == nil {
		t.Error("ResolveRangeEnd should reject a bare date in end_time")
	}
}

// Handlers echo start_date/end_date back to the client. The end bound is
// exclusive, so the last day it covers is one instant earlier — which must hold
// for both a midnight-aligned date window and a rolling window ending mid-day.
func TestFormatRangeEndDate(t *testing.T) {
	if err := Init("UTC"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}

	// Date window [08-28 00:00, 08-29 00:00) covers 08-28.
	midnight := time.Date(2026, 8, 29, 0, 0, 0, 0, loc)
	if got := FormatRangeEndDate(midnight, "Asia/Shanghai"); got != "2026-08-28" {
		t.Errorf("FormatRangeEndDate(next midnight) = %s, want 2026-08-28", got)
	}

	// Rolling window ending 08-28 14:37 still covers 08-28 — the old
	// `end.Add(-24h)` echo reported 08-27 here.
	rolling := time.Date(2026, 8, 28, 14, 37, 0, 0, loc)
	if got := FormatRangeEndDate(rolling, "Asia/Shanghai"); got != "2026-08-28" {
		t.Errorf("FormatRangeEndDate(mid-day) = %s, want 2026-08-28", got)
	}

	// Rendered in the caller's timezone, not the server's.
	if got := FormatRangeDate(rolling, "UTC"); got != "2026-08-28" {
		t.Errorf("FormatRangeDate(UTC) = %s, want 2026-08-28", got)
	}
	early := time.Date(2026, 8, 28, 3, 0, 0, 0, loc) // 2026-08-27 19:00 UTC
	if got := FormatRangeDate(early, "UTC"); got != "2026-08-27" {
		t.Errorf("FormatRangeDate(UTC) = %s, want 2026-08-27", got)
	}
}
