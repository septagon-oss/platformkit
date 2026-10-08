package components_test

import (
	"strings"
	"testing"
	"time"

	c "github.com/septagon-oss/platformkit/ui/components"
)

func timelineFixture() c.TimelineProps {
	return c.TimelineProps{Label: "Activity", ActorLabel: "Actor", TimeLabel: "Time", DetailsLabel: "Details", BeforeLabel: "Before", AfterLabel: "After", Items: []c.TimelineItem{
		{ID: "b", ActorText: "Unknown actor", Summary: "Second in server order", Time: c.TimeText{AtUTC: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), Text: "29 September, 12:00 UTC"}, Changes: []c.Change{{Label: "Title", BeforeText: "First", AfterText: "Second"}}},
		{ID: "a", ActorText: "Alex", Summary: "First identifier, later in server order", Time: c.TimeText{AtUTC: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC), Text: "29 September, 12:00 UTC"}},
	}}
}

func TestTimelineKeepsEqualTimeServerOrderAndNativeDisclosure(t *testing.T) {
	for _, layout := range []string{"timeline", "audit"} {
		p := timelineFixture()
		p.Layout = layout
		out := html(t, c.Timeline(p))
		if strings.Index(out, "Second in server order") > strings.Index(out, "First identifier") {
			t.Fatal("P10: reordered equal-time events")
		}
		for _, want := range []string{`datetime="2026-09-29T12:00:00Z"`, "29 September, 12:00 UTC", "<details>", "Before", "After", "First", "Second"} {
			if !strings.Contains(out, want) {
				t.Fatalf("P10: missing %s", want)
			}
		}
		p.Items[0].Changes = []c.Change{{Label: "Title", Redacted: true, RedactedText: "Restricted"}}
		if out := html(t, c.Timeline(p)); !strings.Contains(out, "Restricted") || strings.Contains(out, ">First<") {
			t.Fatal("P10: redacted output retained a value")
		}
	}
}

func TestTimelineRefusesRedactedValuesAndMalformedInstantsBeforeBytes(t *testing.T) {
	for _, change := range []func(*c.TimelineProps){
		func(p *c.TimelineProps) {
			p.Items[0].Changes[0].Redacted = true
			p.Items[0].Changes[0].RedactedText = "Restricted"
		},
		func(p *c.TimelineProps) {
			p.Items[0].Time.AtUTC = p.Items[0].Time.AtUTC.In(time.FixedZone("offset", 3600))
		},
		func(p *c.TimelineProps) { p.Items[1].ID = "b" },
		func(p *c.TimelineProps) { p.Items[0].ActorText = "" },
		func(p *c.TimelineProps) {
			p.State = c.ContentState{Status: c.MediaRefused, Title: "Unavailable", Text: "No access"}
		},
	} {
		p := timelineFixture()
		change(&p)
		var out strings.Builder
		if err := c.Timeline(p).Render(&out); err == nil || out.Len() != 0 {
			t.Fatalf("P1/P10: invalid event rendered: %s", out.String())
		}
	}
}
