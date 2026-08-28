package timezone

import (
	"strings"
	"time"
)

// Query windows arrive in two flavours:
//
//   - start_date / end_date — calendar dates (YYYY-MM-DD) read in the caller's
//     timezone. end_date names a day the caller wants included, so it expands to
//     the following midnight to keep the window half-open [start, end).
//   - start_time / end_time — exact RFC3339 instants, used verbatim.
//
// Instants win when both are present. Only a caller that means a rolling window
// sends them: "last 24 hours" expressed as two dates covers two calendar days
// (up to 48 hours), which is what made the usage page disagree with the
// dashboard's "today" card.

// locationFor resolves the caller's timezone, falling back to the
// server-configured one when userTZ is empty or unknown.
func locationFor(userTZ string) *time.Location {
	if userTZ != "" {
		if loc, err := time.LoadLocation(userTZ); err == nil {
			return loc
		}
	}
	return Location()
}

// ResolveRangeStart resolves the inclusive lower bound of a query window from
// an RFC3339 instant or a calendar date. Returns nil when neither is supplied.
func ResolveRangeStart(instant, date, userTZ string) (*time.Time, error) {
	return resolveRangeBound(instant, date, userTZ, false)
}

// ResolveRangeEnd resolves the exclusive upper bound of a query window. A
// calendar date expands to the following midnight so the named day is fully
// covered; an instant is used as-is.
func ResolveRangeEnd(instant, date, userTZ string) (*time.Time, error) {
	return resolveRangeBound(instant, date, userTZ, true)
}

func resolveRangeBound(instant, date, userTZ string, exclusiveEnd bool) (*time.Time, error) {
	if instant = strings.TrimSpace(instant); instant != "" {
		t, err := time.Parse(time.RFC3339, instant)
		if err != nil {
			return nil, err
		}
		return &t, nil
	}
	if date = strings.TrimSpace(date); date == "" {
		return nil, nil
	}
	t, err := ParseInUserLocation("2006-01-02", date, userTZ)
	if err != nil {
		return nil, err
	}
	if exclusiveEnd {
		// Next calendar day start rather than +24h, so DST transitions don't
		// clip or extend the window.
		t = t.AddDate(0, 0, 1)
	}
	return &t, nil
}

// FormatRangeDate renders t as a calendar date in the caller's timezone.
func FormatRangeDate(t time.Time, userTZ string) string {
	return t.In(locationFor(userTZ)).Format("2006-01-02")
}

// FormatRangeEndDate renders the last calendar day an exclusive upper bound
// covers. Handlers echo start_date/end_date back to the client; stepping back
// one nanosecond gives the right day for both a midnight-aligned date window
// (where end is the next day's 00:00) and an arbitrary rolling-window instant.
func FormatRangeEndDate(end time.Time, userTZ string) string {
	return FormatRangeDate(end.Add(-time.Nanosecond), userTZ)
}
