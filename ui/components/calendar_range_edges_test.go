package components_test

import (
	"bytes"
	"strings"
	"testing"
	"time"

	c "github.com/septagon-oss/platformkit/ui/components"
)

// A week displays [start, end): its first and last civil days may be selected,
// the exclusive end and the day before the start may not.
func TestCalendarSelectedDateHonoursTheRangesEdges(t *testing.T) {
	week := func(selected string) c.CalendarProps {
		return c.CalendarProps{
			ComponentProps: c.ComponentProps{ID: "calendar"},
			Label:          "Events",
			View:           "week",
			DateStrip: c.DateStripProps{Label: "Dates", SelectedDate: selected,
				Days: []c.DateChoice{{Date: selected, Label: selected, Href: "/calendar?date=" + selected}}},
			RangeStartDate: "2026-10-19", RangeEndDate: "2026-10-26",
			TimeZone: "Europe/Lisbon", TimeZoneLabel: "Lisbon", Language: "pt-PT", FirstWeekday: 1,
			NowUTC:      time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
			AllDayLabel: "Todo o dia", AgendaLabel: "Agenda", GridLabel: "Semana", FallbackText: "Agenda disponível",
			Events: []c.CalendarEvent{{ID: "event", Title: "Reunião", TimeText: "09:00", StatusLabel: "Disponível",
				StartUTC: time.Date(2026, 10, 21, 8, 0, 0, 0, time.UTC), EndUTC: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}},
		}
	}
	for _, day := range []string{"2026-10-19", "2026-10-25"} {
		var out bytes.Buffer
		if err := c.Calendar(week(day)).Render(&out); err != nil || !strings.Contains(out.String(), "Reunião") {
			t.Errorf("selected %s inside the displayed week must render the week: %v", day, err)
		}
	}
	for _, day := range []string{"2026-10-18", "2026-10-26"} {
		var out bytes.Buffer
		if err := c.Calendar(week(day)).Render(&out); err == nil || out.Len() != 0 {
			t.Errorf("selected %s outside [2026-10-19, 2026-10-26) must refuse with no output: %v, %d bytes", day, err, out.Len())
		}
	}
}
