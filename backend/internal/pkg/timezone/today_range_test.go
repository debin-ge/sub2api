package timezone

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTodayRangeInUserLocation_HalfOpenLocalDay(t *testing.T) {
	for _, tz := range []string{"UTC", "Asia/Shanghai", "America/New_York", "Asia/Kolkata", "Pacific/Kiritimati"} {
		t.Run(tz, func(t *testing.T) {
			loc, err := time.LoadLocation(tz)
			require.NoError(t, err)

			start, end := TodayRangeInUserLocation(tz)

			now := time.Now().In(loc)
			require.Equal(t, loc.String(), start.Location().String())
			require.Equal(t, 0, start.Hour())
			require.Equal(t, 0, start.Minute())
			require.Equal(t, 0, start.Second())
			require.Equal(t, now.Year(), start.Year())
			require.Equal(t, now.YearDay(), start.YearDay())
			require.False(t, now.Before(start), "now must be inside [start, end)")
			require.True(t, now.Before(end), "now must be inside [start, end)")
			// The end is the next calendar day, not start+24h.
			require.Equal(t, start.AddDate(0, 0, 1), end)
		})
	}
}

func TestTodayRangeInUserLocation_FallsBackToServerTimezone(t *testing.T) {
	serverStart, serverEnd := TodayRangeInUserLocation("")
	require.Equal(t, Today(), serverStart)
	require.Equal(t, Today().AddDate(0, 0, 1), serverEnd)

	unknownStart, unknownEnd := TodayRangeInUserLocation("Not/AZone")
	require.Equal(t, serverStart, unknownStart)
	require.Equal(t, serverEnd, unknownEnd)
}
