package components_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	c "github.com/septagon-oss/platformkit/ui/components"
)

func TestCalendarSelectedDateBelongsToDisplayedRange(t *testing.T) {
	props := c.CalendarProps{
		ComponentProps: c.ComponentProps{ID: "calendar"},
		Label:          "Events",
		View:           "day",
		DateStrip: c.DateStripProps{Label: "Dates", SelectedDate: "2026-10-24",
			Days: []c.DateChoice{{Date: "2026-10-24", Label: "24 October", Href: "/calendar?date=2026-10-24"}}},
		RangeStartDate: "2026-10-24", RangeEndDate: "2026-10-25",
		TimeZone: "UTC", TimeZoneLabel: "UTC", Language: "en", FirstWeekday: 1,
		NowUTC:      time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC),
		AllDayLabel: "All day", AgendaLabel: "Agenda", GridLabel: "Day grid", FallbackText: "Agenda available",
		Events: []c.CalendarEvent{{ID: "event", Title: "Meeting", TimeText: "09:00 UTC", StatusLabel: "Available",
			StartUTC: time.Date(2026, 10, 24, 9, 0, 0, 0, time.UTC), EndUTC: time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)}},
	}
	var valid bytes.Buffer
	if err := c.Calendar(props).Render(&valid); err != nil || !strings.Contains(valid.String(), "Meeting") {
		t.Fatalf("a displayed selected day must render its events: %v, %q", err, valid.String())
	}

	props.DateStrip.SelectedDate = "2026-10-26"
	props.DateStrip.Days = []c.DateChoice{{Date: "2026-10-26", Label: "26 October", Href: "/calendar?date=2026-10-26"}}
	var out bytes.Buffer
	if err := c.Calendar(props).Render(&out); err == nil || out.Len() != 0 {
		t.Fatalf("day view selected a date outside its displayed range: %v, %q", err, out.String())
	}
}
