package view_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAFieldWhoseAuthorDeclaredNoColourWearsNoneInItsFieldAndItsCell holds that a
// colour on a Page comes from a declaration and never from the value being there. A
// field whose declaration names no value to colour wears no tone at all, in the
// record's field and in the list's cell, whatever it carries: these rows show text, a
// boolean, an amount, an instant, a list, a map and an integer, and the schema declares
// tones for one field only. That one shows the tone the policy resolved for the value
// the row holds, read from the same build rather than copied out of it, so what the
// plain fields refuse is a reading a shell paints with, not an empty string nobody
// looks at.
func TestAFieldWhoseAuthorDeclaredNoColourWearsNoneInItsFieldAndItsCell(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"id": "t1", "title": "Write", "status": "open", "done": true,
			"amount": json.Number("1250"), "currency": "EUR", "dueAt": "2026-07-01T14:12:00Z",
			"labels": []any{"a"}, "meta": map[string]any{"source": "plan"}, "revision": json.Number("3")},
		{"id": "t2", "title": "Ship", "status": "done", "amount": json.Number("-50"),
			"currency": "EUR", "labels": []any{}, "revision": json.Number("1")},
	}
	// Every field these rows show that declares no value to colour, in either Page.
	plain := []string{"title", "done", "amount", "currency", "dueAt", "labels", "meta", "revision"}
	seen := 0

	for _, at := range tonesAtEveryPlace(t, rows) {
		if at.field == "status" {
			if at.tone != at.wanted {
				t.Errorf("%s shows %s=%q with tone %q, want the %q its own build resolved for that value",
					at.surface, at.field, at.value, at.tone, at.wanted)
			}
			continue
		}
		for _, name := range plain {
			if at.field != name {
				continue
			}
			seen++
			if at.tone != "" {
				t.Errorf("%s shows %s with tone %q, which nothing declared: the colour is invented from the value being there",
					at.surface, at.field, at.tone)
			}
		}
	}
	if seen == 0 {
		t.Fatal("no plain field reached a Page, so this case inspected nothing")
	}
}

// place is one field as one Page showed it, with the tone the build's own policy
// resolved for the value it was shown.
type place struct {
	surface, field, value, tone, wanted string
}

// tonesAtEveryPlace is every field these rows are shown with, in a record and in a
// list, each with the tone it wore.
func tonesAtEveryPlace(t *testing.T, rows []map[string]any) []place {
	t.Helper()

	var out []place
	for _, row := range rows {
		in := input(view.KindRecord, []map[string]any{row}, nil)
		for _, section := range build(t, in).Sections {
			for _, f := range section.Fields {
				out = append(out, place{surface: "the record", field: f.Name,
					value: f.Text, tone: f.Tone, wanted: resolvedTone(in, f.Name, f.Text)})
			}
		}
	}
	in := input(view.KindList, rows, nil)
	for _, r := range build(t, in).Table.Rows {
		for _, c := range r.Cells {
			out = append(out, place{surface: "the list", field: c.Field,
				value: c.Text, tone: c.Tone, wanted: resolvedTone(in, c.Field, c.Text)})
		}
	}
	if len(out) == 0 {
		t.Fatal("these rows reached no Page")
	}
	return out
}

// resolvedTone is the colour the same build's policy holds for one field's reading:
// "" for a field that declares no value to colour, so a plain field's wanted tone is
// its empty one rather than a figure this case guesses.
func resolvedTone(in view.Input, name, shown string) string {
	for _, rf := range in.Policy.Fields {
		if rf.Name != name {
			continue
		}
		for _, o := range rf.Options {
			if o.Label == shown || o.Value == shown {
				return o.Tone
			}
		}
	}
	return ""
}
