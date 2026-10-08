package components_test

import (
	"bytes"
	"testing"
	"time"

	c "github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	g "maragu.dev/gomponents"
)

func TestCalendarRefusesSkippedBoundariesBeforeRenderingOrCapture(t *testing.T) {
	for _, tc := range []struct{ name, start, end string }{
		{"inside range", "2011-12-28", "2011-12-31"},
		{"range start", "2011-12-30", "2012-01-01"},
		{"exclusive range end", "2011-12-28", "2011-12-30"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := c.CalendarProps{
				ComponentProps: c.ComponentProps{ID: "calendar"},
				Label:          "History", TimeZone: "Pacific/Apia", TimeZoneLabel: "Apia", Language: "en",
				NowUTC:         time.Date(2011, 12, 28, 0, 0, 0, 0, time.UTC),
				RangeStartDate: tc.start, RangeEndDate: tc.end,
				AllDayLabel: "All day", AgendaLabel: "Agenda", GridLabel: "Grid", FallbackText: "Agenda available",
				DateStrip: c.DateStripProps{Label: "Date", SelectedDate: tc.start,
					Days: []c.DateChoice{{Date: tc.start, Label: tc.start, Href: "/calendar"}}},
				Events: []c.CalendarEvent{{ID: "event", Title: "History", TimeText: "All days", StatusLabel: "Available",
					AllDay: true, StartDate: tc.start, EndDate: tc.end}},
			}
			if err := p.Validate(); err != nil {
				t.Fatalf("fixture must reach calendar computation: %v", err)
			}
			if days, err := c.CalendarAgenda(p); err == nil || days != nil {
				t.Fatalf("unrepresentable range must refuse without partial days: %+v, %v", days, err)
			}
			calls := 0
			slots := c.CalendarSlots{EventDetails: func(c.CalendarEvent) g.Node {
				calls++
				return g.Text("event detail")
			}}
			example := examples.ExampleWithSlots(examples.ExampleInfo{ID: "calendar/skipped", ComponentID: "pk-ui.component.calendar"}, p, slots, c.CalendarWithSlots)
			var out bytes.Buffer
			if err := example.Node.Render(&out); err == nil || out.Len() != 0 {
				t.Fatalf("unrepresentable range rendered %q, %v", out.String(), err)
			}
			if _, err := example.Describe(); err == nil {
				t.Fatal("unrepresentable range was captured")
			}
			if calls != 0 {
				t.Fatalf("refused calendar invoked event details %d times", calls)
			}
		})
	}
}
