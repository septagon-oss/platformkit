package resource

// The version panel renders supplied history without reading a store. Restore
// forms retain the displayed row's revision so the command can refuse stale writes.

import (
	"strconv"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui/components"
)

// HistoryPanel is one record's versions, beside the two addresses and the words
// their controls need. The adapter reads the series; this package draws it.
type HistoryPanel struct {
	// Resolved supplies the authorized restore command; nil preserves legacy panels.
	Resolved *present.ResolvedCommand
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
	// Asking is what the command says a restore will cost, in the author's own
	// words, and empty when the author declared no consequence. It rides on the
	// button as the question the page asks before it submits: a restore is not
	// destructive — nothing is removed — but it does overwrite what the person is
	// reading, and a panel that let them do that silently is a panel that hoped they
	// had read the header.
	Asking string
	Rows   []VersionRow
}

// VersionRow is one stored version: its number, when it was written and by whom as
// this shell words them, which of the record's fields it holds, and — for the write
// that brought an older one back — the version it came from. Those fields are the
// comparison a person came for: the record above says what it says now, and this
// says what the write moved to arrive here.
type VersionRow struct {
	Number int64
	When   string
	Who    string
	// RestoredFrom, above zero, is the version this write read in order to be
	// written. Zero is an ordinary save, which came from whatever the caller sent.
	RestoredFrom int64
	Changes      []VersionChange
}

// VersionChange is one field of one stored version: what the form calls the field,
// and the value as the comparison it is — the words this write left standing, the
// words it put in, the words it took out, in the order they read.
type VersionChange struct {
	Label string
	Edits []VersionEdit
}

// EditKind is what one stretch of a compared value is to the write being shown.
type EditKind int

const (
	// EditKept is a word both versions hold. EditInserted is one this write added,
	// EditRemoved one it dropped; nothing else is a comparison.
	EditKept EditKind = iota
	EditInserted
	EditRemoved
)

// VersionEdit is one stretch of a compared value, in the order a reader meets it.
// A field nobody compared arrives as one EditKept holding its whole value, which is
// why the panel never has to ask whether a comparison exists.
type VersionEdit struct {
	Text string
	Kind EditKind
}

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
	for i, v := range p.Rows {
		meta := "v" + strconv.FormatInt(v.Number, 10)
		if v.When != "" {
			meta += " · " + v.When
		}
		if v.Who != "" {
			meta += " · " + v.Who
		}
		lines := []g.Node{components.FormSentence(components.FormSentenceProps{Text: meta})}
		if v.RestoredFrom > 0 {
			// The write says where it came from, so the series reads as the sequence it
			// is: an older version brought back is a write like any other, and naming it
			// is the difference between "Body: Earlier text" as a mystery and as the
			// fourth entry in a record's story.
			lines = append(lines, components.FormSentence(components.FormSentenceProps{
				Text: o.Text("screens.history.restored", "Restored from v%[1]d", v.RestoredFrom),
			}))
		}
		for _, change := range v.Changes {
			body := []g.Node{g.Text(change.Label + ": ")}
			for _, edit := range change.Edits {
				body = append(body, editNode(edit))
			}
			lines = append(lines, components.FormSentenceWithSlots(
				components.FormSentenceProps{Text: change.Label},
				components.FormSentenceSlots{Body: body},
			))
		}
		if len(v.Changes) == 0 {
			// A version that moved no field. Only the first one of a series has no
			// earlier state to be compared with; every later one that moved nothing is a
			// write that saved what was already there, and saying "first written" about it
			// would be the panel inventing a beginning.
			sentence := o.Text("screens.history.unchanged", "No field changed.")
			if v.Number <= 1 {
				sentence = o.Text("screens.history.first", "The record as it was first written.")
			}
			lines = append(lines, components.FormSentence(components.FormSentenceProps{Text: sentence}))
		}
		if p.Action != "" {
			lines = append(lines, restoreForm(p, v, i == 0))
		}
		versions = append(versions, h.Div(lines...))
	}
	return h.Details(
		h.Summary(g.Text(p.Title)),
		h.Div(h.Aria("label", p.Title), g.Group(versions)),
	)
}

// editNode is one stretch of a comparison in the markup that means it: an
// insertion is an insertion, a deletion stays legible as what it was, and the rest
// reads as prose. The elements carry no class, so the browser's own convention —
// underline, strike-through — is the marking, and a reader with the sheet off still
// sees which is which.
func editNode(e VersionEdit) g.Node {
	switch e.Kind {
	case EditInserted:
		return g.El("ins", g.Text(e.Text))
	case EditRemoved:
		return g.El("del", g.Text(e.Text))
	default:
		return g.Text(e.Text)
	}
}

func restoreForm(p *HistoryPanel, v VersionRow, first bool) g.Node {
	button := components.ButtonProps{Label: p.Restore, Type: "submit", Size: "md", Variant: "outline"}
	if p.Resolved != nil {
		if p.Resolved.Destructive {
			button.Tone = "danger"
		}
		if p.Resolved.Primary && first {
			button.Variant = "primary"
		}
	}
	if p.Asking != "" {
		button.ComponentProps = components.ComponentProps{Attrs: map[string]string{
			"data-confirm": p.Asking, "data-confirm-label": p.Restore,
		}}
	}
	return components.Form(components.FormProps{
		Action: p.Action, Label: p.Restore + " v" + strconv.FormatInt(v.Number, 10),
	},
		h.Input(h.Type("hidden"), h.Name("version"), h.Value(strconv.FormatInt(v.Number, 10))),
		h.Input(h.Type("hidden"), h.Name("expectedRevision"), h.Value(p.Condition)),
		components.Button(button),
	)
}
