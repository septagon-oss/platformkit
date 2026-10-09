package components

// time.go is the one <time> element: the instant in the attribute a machine
// reads and the words a person reads, from the same pair.

import (
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// TimeProps is one instant as a screen shows it. The instant travels in the
// datetime attribute and the text is the caller's own words for it — the server
// writes UTC and the reader's browser may re-say the words in the reader's own
// zone (see ui/assets/js/times.js), which is only possible while the two are
// separate.
//
// Class is the only styling door. An inline <time> takes its type from the cell
// or the definition it sits in, which is what a generated list and a generated
// record want; a renderer that ships a hook it never styles is the thing the
// layer gate refuses, so nothing here declares a class list of its own.
type TimeProps struct {
	ComponentProps
	Instant TimeText
	// Title is the exact instant spelled out for a person who hovers the cell, in
	// the zone the server wrote it in. The reader's browser replaces it with the
	// same moment in the reader's own zone; until it runs, or where it cannot, the
	// wall time on the page still says which zone it is in rather than letting a
	// person guess.
	Title string
}

// Time renders TimeProps as a native <time>. It is the pair the timeline has
// always carried, exported: a table cell, a description list and a timeline row
// now state an instant the same way, and one test of the datetime attribute
// covers all three.
func Time(p TimeProps) g.Node {
	attrs := []g.Node{g.Attr("datetime", p.Instant.AtUTC.Format(time.RFC3339Nano))}
	if p.Title != "" {
		attrs = append(attrs, g.Attr("title", p.Title))
	}
	if p.Class != "" {
		attrs = append(attrs, h.Class(p.Class))
	}
	return h.Time(append(baseAttrs(p.ComponentProps), append(attrs, g.Text(p.Instant.Text))...)...)
}
