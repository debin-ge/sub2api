package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newUsageFilterContext(t *testing.T, query string) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, query, nil)
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 7})
	return c
}

// "Last 24 hours" is a rolling window. Sent as start_date/end_date it becomes
// [yesterday 00:00, tomorrow 00:00) — up to 48 hours — which is why the usage
// page reported roughly twice the tokens of the dashboard's "today" card. The
// instants must therefore win outright over the dates.
func TestParseUserUsageFilters_InstantsWinOverDates(t *testing.T) {
	h := &UsageHandler{}

	c := newUsageFilterContext(t,
		"/?timezone=Asia/Shanghai"+
			"&start_date=2026-08-27&end_date=2026-08-28"+
			"&start_time=2026-08-27T06:37:00Z&end_time=2026-08-28T06:37:00Z")

	parsed, ok := h.parseUserUsageFilters(c, true)
	require.True(t, ok)
	require.Equal(t, time.Date(2026, 8, 27, 6, 37, 0, 0, time.UTC), parsed.StartTime.UTC())
	require.Equal(t, time.Date(2026, 8, 28, 6, 37, 0, 0, time.UTC), parsed.EndTime.UTC())
	require.Equal(t, 24*time.Hour, parsed.EndTime.Sub(parsed.StartTime))
	require.Equal(t, parsed.StartTime, *parsed.Filters.StartTime)
	require.Equal(t, parsed.EndTime, *parsed.Filters.EndTime)

	// The echoed dates name the days the window covers. The end bound is
	// exclusive, so a window ending mid-day still covers that day.
	startDate, endDate := parsed.echoDateRange()
	require.Equal(t, "2026-08-27", startDate)
	require.Equal(t, "2026-08-28", endDate)
}

// Without instants a date pair still means whole calendar days in the caller's
// timezone, half-open so the last day is fully included exactly once.
func TestParseUserUsageFilters_DatesRemainWholeDays(t *testing.T) {
	h := &UsageHandler{}

	c := newUsageFilterContext(t, "/?timezone=Asia/Shanghai&start_date=2026-08-28&end_date=2026-08-28")

	parsed, ok := h.parseUserUsageFilters(c, true)
	require.True(t, ok)
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)
	require.Equal(t, time.Date(2026, 8, 28, 0, 0, 0, 0, loc), parsed.StartTime.In(loc))
	require.Equal(t, time.Date(2026, 8, 29, 0, 0, 0, 0, loc), parsed.EndTime.In(loc))

	startDate, endDate := parsed.echoDateRange()
	require.Equal(t, "2026-08-28", startDate)
	require.Equal(t, "2026-08-28", endDate)
}

func TestParseUserUsageFilters_RejectsMalformedInstants(t *testing.T) {
	h := &UsageHandler{}

	c := newUsageFilterContext(t, "/?start_time=2026-08-27")
	_, ok := h.parseUserUsageFilters(c, true)
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, c.Writer.Status())

	c = newUsageFilterContext(t, "/?end_time=not-a-timestamp")
	_, ok = h.parseUserUsageFilters(c, true)
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, c.Writer.Status())
}
