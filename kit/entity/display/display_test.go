package display_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
)

func TestDisplayIsTheOneWayAValueIsShown(t *testing.T) {
	t.Parallel()
	status := entity.Field{Name: "status", Type: entity.TypeString, Enum: []string{"open", "in_progress"}}
	for _, c := range []struct {
		f    entity.Field
		v    any
		want string
	}{
		{status, "in_progress", "In progress"},
		{status, "", "—"},
		{entity.Field{Type: entity.TypeBool}, true, "Yes"},
		{entity.Field{Type: entity.TypeBool}, false, "No"},
		{entity.Field{Type: entity.TypeBool}, nil, "No"},
		{entity.Field{Type: entity.TypeTime}, "2026-09-02T14:30:00.123456Z", "2026-09-02 14:30"},
		{entity.Field{Type: entity.TypeTime}, nil, "—"},
		{entity.Field{Type: entity.TypeString}, "plain", "plain"},
		{entity.Field{Type: entity.TypeInt}, float64(3), "3"},
		{entity.Field{Type: entity.TypeList}, []any{"a", "b"}, "a, b"},
		{entity.Field{Type: entity.TypeString}, nil, "—"},
	} {
		if got := display.Display(c.f, c.v); got != c.want {
			t.Errorf("Display(%s, %#v) = %q, want %q", c.f.Type, c.v, got, c.want)
		}
	}
	// Text is the other one, and it is not this: a select posts back the enum's
	// own spelling and a checkbox posts "true".
	if display.Text("in_progress") != "in_progress" || display.Text(true) != "true" {
		t.Error("Text is rewriting what a control posts")
	}
	for name, want := range map[string]string{
		"slaDeadline": "Sla deadline", "in_progress": "In progress", "status": "Status",
	} {
		if got := display.Humanize(name); got != want {
			t.Errorf("Humanize(%q) = %q, want %q", name, got, want)
		}
	}
	// A label is the field's name, not its doc: the entities here write a
	// sentence in doc, which reads under a control and not on it.
	f := entity.Field{Name: "slaDeadline", Doc: "Hard SLA deadline; a breach is measured against this"}
	if display.FieldLabel(f) != "Sla deadline" || display.FieldHelp(f) != f.Doc {
		t.Errorf("FieldLabel/FieldHelp = %q / %q", display.FieldLabel(f), display.FieldHelp(f))
	}
}

// TestPluralKeepsAWordTheCatalogueAlreadyWroteAsASet is the rule behind "Settingss"
// and "Contents": a mass noun and a word that already ends in s are returned
// exactly as the schema wrote them, and everything else takes one "s". The list is
// the phone's eight words, so the two clients inflect one noun the same way.
func TestPluralKeepsAWordTheCatalogueAlreadyWroteAsASet(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ one, want string }{
		{"Task", "Tasks"}, {"plan", "plans"}, {"Note", "Notes"},
		// Mass nouns, kept whole, in either case.
		{"Content", "Content"}, {"content", "content"}, {"Settings", "Settings"},
		{"settings", "settings"}, {"News", "News"}, {"Media", "Media"}, {"Data", "Data"},
		{"Staff", "Staff"}, {"Feedback", "Feedback"}, {"Information", "Information"},
		// Words that end in s anyway: the rule that adds another is the defect.
		{"Business", "Business"}, {"Status", "Status"}, {"Analysis", "Analysis"},
		// Nothing to inflect, and an inflection nobody asked for is not an answer.
		{"", ""},
		// The input is returned as written — only the comparison is trimmed.
		{"  Task  ", "  Task  s"},
	} {
		if got := display.Plural(c.one); got != c.want {
			t.Errorf("Plural(%q) = %q, want %q", c.one, got, c.want)
		}
	}
}

// TestMomentSaysTheSameWordsItAlwaysSaid pins the reading, and Instant keeps the
// moment beside it: the zone a person reads is chosen by the reader's own engine,
// so the string every reading shares did not move when the datetime attribute
// arrived. A value that is no instant answers with its own raw text and ok false —
// never an invented date.
func TestMomentSaysTheSameWordsItAlwaysSaid(t *testing.T) {
	t.Parallel()
	at, text, ok := display.Moment("2026-07-01T14:12:00Z")
	if !ok || text != "2026-07-01 14:12" || !at.UTC().Equal(at) {
		t.Errorf("Moment = %q ok=%v, want the wall time the screens have always shown", text, ok)
	}
	if got := display.Display(entity.Field{Type: entity.TypeTime}, "2026-07-01T14:12:00.123456Z"); got != "2026-07-01 14:12" {
		t.Errorf("Display of an instant with microseconds = %q", got)
	}
	if _, text, ok := display.Moment("not a date"); ok || text != "not a date" {
		t.Errorf("Moment of a value that is no instant = %q ok=%v, want the stored text and no claim", text, ok)
	}
	if _, text, ok := display.Moment(nil); ok || text != "" {
		t.Errorf("Moment of nothing = %q ok=%v, want nothing and no claim", text, ok)
	}
	if _, ok := display.Instant("2026-07-01T14:12:00+01:00"); !ok {
		t.Error("Instant refused an offset instant")
	}
	if _, ok := display.Instant(nil); ok {
		t.Error("Instant read a moment out of nothing")
	}
}
