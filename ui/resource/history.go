package resource

// history.go is the version series beside the record it describes.
//
// A version is the one part of a record a detail view cannot read off the row: the
// row says what it is, and only the series says what it used to be. The panel is
// drawn from what the adapter read behind the record's own read guard, inside the
// request's own transaction — ui/resource still draws and does no I/O, so nothing
// here opens a store or asks a question of a caller.
//
// Restoring is a command like any other, and the panel carries that command rather
// than redrawing it: each version's form posts to the address the Spec mounted,
// with the version it asks for and the write count this page was drawn from. That
// second number is why a form here can be refused: two people reading one record,
// one of them restoring first, is the ordinary case, and a write that arrives
// quoting a revision the record has left behind is refused and writes nothing.

import (
	"strconv"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/ui/components"
)

// HistoryPanel is one record's versions, beside the two addresses and the words
// their controls need. The adapter reads the series; this package draws it.
type HistoryPanel struct {
	// Action is where a restore posts: the record's own address plus the verb of
	// the command the Spec mounted. Carries names that verb, so the page that shows
	// this panel shows the restore once and not twice.
	Action  string
	Carries string
	// Condition is the row's own write count as this page read it, posted back as
	// the condition of every restore below. Empty means the row named no write
	// count, which is a resource kit/rest refuses at mount.
	Condition string
	// Title is what the disclosure is called in the shell's language; Restore is
	// the button word the command's author declared.
	Title   string
	Restore string
	Rows    []VersionRow
}

// VersionRow is one stored version: its number, when it was written and by whom as
// this shell words them, and which of the record's fields it holds. Those fields
// are the comparison a person came for — the record above says what it says now,
// and this says what the write moved to arrive here.
type VersionRow struct {
	Number  int64
	When    string
	Who     string
	Changes []VersionChange
}

// VersionChange is one field of one stored version: what the form calls the field,
// and the value that version held, as a screen shows a value.
type VersionChange struct{ Label, Value string }

// historyDisclosure is the panel: a real details element, so the record stays the
// thing a person came to read and its past is one click away, and so a reader with
// no JavaScript still gets every version in the document. The summary carries the
// name and nothing else; every line under it wears the size the record's own copy
// already wears, because a record page that speaks three sizes of body text is
// three pages.
func historyDisclosure(o Options, p *HistoryPanel) g.Node {
	if p == nil || len(p.Rows) == 0 {
		// An empty group rather than a nil node: the caller appends this to the
		// page's body, and a body carrying a nil node is a render that panics on
		// every record of every resource that keeps no versions.
		return g.Group{}
	}
	versions := make([]g.Node, 0, len(p.Rows))
	for _, v := range p.Rows {
		meta := "v" + strconv.FormatInt(v.Number, 10)
		if v.When != "" {
			meta += " · " + v.When
		}
		if v.Who != "" {
			meta += " · " + v.Who
		}
		lines := []g.Node{components.FormSentence(components.FormSentenceProps{Text: meta})}
		for _, change := range v.Changes {
			lines = append(lines, components.FormSentence(components.FormSentenceProps{
				Text: change.Label + ": " + change.Value,
			}))
		}
		if len(v.Changes) == 0 {
			// A version that moved no field is the write that made the record: the
			// first one, which has no earlier state to be compared with.
			lines = append(lines, components.FormSentence(components.FormSentenceProps{
				Text: o.Text("screens.history.first", "The record as it was first written."),
			}))
		}
		lines = append(lines, components.Form(components.FormProps{
			Action: p.Action, Label: p.Restore + " v" + strconv.FormatInt(v.Number, 10),
		},
			h.Input(h.Type("hidden"), h.Name("version"), h.Value(strconv.FormatInt(v.Number, 10))),
			h.Input(h.Type("hidden"), h.Name("expectedRevision"), h.Value(p.Condition)),
			components.Button(components.ButtonProps{Label: p.Restore, Type: "submit", Size: "md"}),
		))
		versions = append(versions, h.Div(lines...))
	}
	return h.Details(
		h.Summary(g.Text(p.Title)),
		h.Div(h.Aria("label", p.Title), g.Group(versions)),
	)
}
