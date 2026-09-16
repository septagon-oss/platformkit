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
