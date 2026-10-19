package screens

import (
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
)

// window_test.go checks the two rules the document owns and no query does: the
// window is fourteen UTC days ending in the day the request was answered in, and
// a day the query named no rows for is a zero on a named day rather than a hole a
// shell would have to interpret.

func utc(y int, m time.Month, d, hh, mm int) time.Time {
	return time.Date(y, m, d, hh, mm, 0, 0, time.UTC)
}

func TestTrendWindowIsFourteenUTCDaysEndingInTheDayAskedAbout(t *testing.T) {
	asOf := utc(2026, 10, 16, 17, 2)
	from, to := TrendWindow(asOf)
	if !from.Equal(utc(2026, 10, 3, 0, 0)) {
		t.Errorf("the window opens at %s, want the start of the thirteenth day before asOf", from)
	}
	if !to.Equal(utc(2026, 10, 17, 0, 0)) {
		t.Errorf("the window closes at %s, want the instant after asOf's own day", to)
	}
	// One second before midnight still belongs to the day it is in: a window that
	// moved at the second instead of the date would drop a day of the sparkline.
	late := utc(2026, 10, 16, 23, 59)
	lateFrom, lateTo := TrendWindow(late)
	if !lateFrom.Equal(from) || !lateTo.Equal(to) {
		t.Errorf("asked at 23:59 the window is %s..%s, want the same %s..%s it was at 17:02", lateFrom, lateTo, from, to)
	}
	first := utc(2026, 10, 17, 0, 0)
	nextFrom, nextTo := TrendWindow(first)
	if !nextFrom.Equal(from.Add(24*time.Hour)) || !nextTo.Equal(to.Add(24*time.Hour)) {
		t.Errorf("asked one day later the window is %s..%s, want this one slid by a single day: %s..%s", nextFrom, nextTo, from.Add(24*time.Hour), to.Add(24*time.Hour))
	}
	if days := int(nextTo.Sub(nextFrom) / (24 * time.Hour)); days != TrendDays {
		t.Errorf("the window spans %d days, want the %d the document prints", days, TrendDays)
	}
}

func TestATrendNamesFourteenDaysIncludingAnEmptyOne(t *testing.T) {
	from, _ := TrendWindow(utc(2026, 10, 16, 12, 0))
	counted := []crud.DayCount{
		{Day: utc(2026, 10, 16, 23, 0), Count: 2},
		{Day: utc(2026, 10, 4, 1, 0), Count: 1},
		{Day: utc(2026, 9, 1, 0, 0), Count: 99}, // outside the window entirely
	}
	got := days(counted, from)
	if len(got) != TrendDays {
		t.Fatalf("the series names %d days, want %d", len(got), TrendDays)
	}
	if got[0].Day != "2026-10-03" || got[TrendDays-1].Day != "2026-10-16" {
		t.Errorf("the series runs %s to %s, oldest first", got[0].Day, got[TrendDays-1].Day)
	}
	if got[TrendDays-1].Count != 2 {
		t.Errorf("today holds %d rows, want the 2 counted at 23:00 of it", got[TrendDays-1].Count)
	}
	if got[1].Day != "2026-10-04" || got[1].Count != 1 {
		t.Errorf("the second bucket is %+v, want 2026-10-04 with its one row", got[1])
	}
	for _, day := range got {
		switch day.Day {
		case "2026-10-03", "2026-10-04", "2026-10-16":
		default:
			if day.Count != 0 {
				t.Errorf("%s answers %d on a day nothing was counted for, want a zero", day.Day, day.Count)
			}
		}
	}
}
