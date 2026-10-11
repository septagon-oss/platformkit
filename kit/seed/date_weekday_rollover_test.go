package seed

import (
	"testing"
	"time"
)

// A weekday expression names the next such moment from the injected clock, in
// UTC whatever zone the clock was handed in: the same weekday later today is
// today, the same weekday earlier today is a week on, and the instant itself
// is not in the past.
func TestRelativeWeekdayIsTheNextSuchMomentInUTC(t *testing.T) {
	// Thursday 2026-10-01 10:30 UTC, handed in as 12:30 in a +02:00 zone.
	now := time.Date(2026, time.October, 1, 12, 30, 0, 0, time.FixedZone("CEST", 2*60*60))
	for _, tc := range []struct {
		expression string
		want       string
	}{
		{"thursday 11:00", "2026-10-01T11:00:00Z"},
		{"thursday 10:30", "2026-10-01T10:30:00Z"},
		{"thursday 09:00", "2026-10-08T09:00:00Z"},
		{"wednesday 23:59", "2026-10-07T23:59:00Z"},
		{"+0d", "2026-10-01T10:30:00Z"},
	} {
		got, err := ResolveDate(now, tc.expression, false)
		if err != nil || got.Format(time.RFC3339) != tc.want {
			t.Errorf("ResolveDate(%q) = %s, %v; want %s", tc.expression, got.Format(time.RFC3339), err, tc.want)
		}
	}
	if got, err := ResolveDate(now, "-1d", true); err != nil || got.Format(time.RFC3339) != "2026-09-30T00:00:00Z" {
		t.Errorf("date-only -1d = %s, %v; want 2026-09-30T00:00:00Z", got.Format(time.RFC3339), err)
	}
	for _, expression := range []string{"Thursday 11:00", "thursday 11:00:00", "+3w", "thursday", "+d"} {
		if got, err := ResolveDate(now, expression, false); err == nil {
			t.Errorf("ResolveDate(%q) = %s; want a refusal", expression, got.Format(time.RFC3339))
		}
	}
}
