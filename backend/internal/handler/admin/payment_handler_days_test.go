package admin

import "testing"

func TestParsePaymentDashboardDaysClampsRange(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{name: "missing uses default", raw: "", want: 30},
		{name: "non numeric uses default", raw: "abc", want: 30},
		{name: "zero uses default", raw: "0", want: 30},
		{name: "negative uses default", raw: "-7", want: 30},
		{name: "normal value kept", raw: "90", want: 90},
		{name: "one is the lower bound", raw: "1", want: 1},
		{name: "366 is the upper bound", raw: "366", want: 366},
		{name: "beyond upper bound clamps", raw: "3650", want: 366},
		{name: "huge value clamps", raw: "999999999", want: 366},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parsePaymentDashboardDays(tc.raw); got != tc.want {
				t.Fatalf("parsePaymentDashboardDays(%q)=%d want=%d", tc.raw, got, tc.want)
			}
		})
	}
}
