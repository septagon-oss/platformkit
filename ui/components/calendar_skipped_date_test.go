package components_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	c "github.com/septagon-oss/platformkit/ui/components"
)

func TestCalendarTerminatesAcrossASkippedCivilDate(t *testing.T) {
	// Isolate a nonterminating renderer so a regression cannot leave a goroutine
	// allocating memory throughout the rest of the package's test suite.
	const child = "PLATFORMKIT_REVIEW_CALENDAR_CHILD"
	if os.Getenv(child) != "1" {
		ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCalendarTerminatesAcrossASkippedCivilDate$")
		cmd.Env = append(os.Environ(), child+"=1")
		output, err := cmd.CombinedOutput()
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			t.Fatalf("a two-day calendar must finish or refuse; Pacific/Apia across 2011-12-30 did not return within 2s: %s", output)
		}
		if err != nil {
			t.Fatalf("calendar subprocess: %v\n%s", err, output)
		}
		return
	}

	p := c.CalendarProps{
		ComponentProps: c.ComponentProps{ID: "review-calendar"},
		Label:          "History", TimeZone: "UTC", TimeZoneLabel: "UTC", Language: "en",
		NowUTC:         time.Date(2011, 12, 29, 0, 0, 0, 0, time.UTC),
		RangeStartDate: "2011-12-29", RangeEndDate: "2011-12-31",
		AllDayLabel: "All day", AgendaLabel: "Agenda", GridLabel: "Grid", FallbackText: "Agenda available",
		DateStrip: c.DateStripProps{
			Label: "Date", SelectedDate: "2011-12-29",
			Days: []c.DateChoice{{Date: "2011-12-29", Label: "29 December", Href: "/calendar?date=2011-12-29"}},
		},
	}
	days, err := c.CalendarAgenda(p)
	if err != nil || len(days) != 2 || days[0].Date != "2011-12-29" || days[1].Date != "2011-12-30" {
		t.Fatalf("the ordinary UTC range must remain a successful read: %v, %+v", err, days)
	}
	p.TimeZone, p.TimeZoneLabel = "Pacific/Apia", "Apia"
	days, err = c.CalendarAgenda(p)
	if err != nil {
		return // A bounded composition refusal for an unrepresentable day is safe.
	}
	if len(days) > 2 {
		t.Fatalf("two civil dates produced %d days", len(days))
	}
	for i, day := range days {
		if day.Date < p.RangeStartDate || day.Date >= p.RangeEndDate || i > 0 && day.Date <= days[i-1].Date {
			t.Fatalf("calendar must advance in the bounded civil range: %+v", days)
		}
	}
}
