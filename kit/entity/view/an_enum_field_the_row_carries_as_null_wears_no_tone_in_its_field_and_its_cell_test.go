package view_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAnEnumFieldTheRowCarriesAsNullWearsNoToneInItsFieldAndItsCell holds that a
// declared colour belongs to a value. A row that carries an enum's key as JSON null, how
// a field a command cleared arrives, and a row that never carried it, how a row written
// before the field arrived, both stored nothing, so both read with no tone at all beside
// the policy's word for nothing. A row naming a declared value wears the colour its
// author declared, which is what makes the first two a reading rather than a channel
// that reaches no shell. A carried null read as a value would wear "neutral", and a
// shell would colour a field nobody filled.
func TestAnEnumFieldTheRowCarriesAsNullWearsNoToneInItsFieldAndItsCell(t *testing.T) {
	t.Parallel()

	row := map[string]any{"id": "t1", "title": "n"}
	carried, carriedCell := statusTones(t, map[string]any{"id": "t1", "title": "n", "status": nil})
	omitted, omittedCell := statusTones(t, row)
	declared, declaredCell := statusTones(t, map[string]any{"id": "t1", "title": "n", "status": "done"})
	missing := absentField(t, row) // the policy's word for nothing, read off this build

	if carried != omitted || carriedCell != omittedCell {
		t.Errorf("a status the row carries as null reads %s in its field and %s in its cell; the same field the row never carried reads %s and %s: each stored nothing, and neither may wear a colour",
			carried, carriedCell, omitted, omittedCell)
	}
	for _, absent := range []string{omitted, omittedCell} {
		tone, text, _ := strings.Cut(absent, "|")
		if tone != "" || text != missing {
			t.Errorf("an absent status reads tone %q and text %q, want no tone beside the policy's own word %q", tone, text, missing)
		}
	}
	for _, named := range []string{declared, declaredCell} {
		tone, text, _ := strings.Cut(named, "|")
		if tone != "success" || text == "" || text == missing {
			t.Errorf("the declared status %q reads tone %q and text %q, want the tone its author declared beside a text of its own, not the absent one's %q", "done", tone, text, missing)
		}
	}
}

// statusTones is one row's status as the record shows it and as the list's cell shows
// it, each one's tone beside its text.
func statusTones(t *testing.T, row map[string]any) (field, cell string) {
	t.Helper()

	in := input(view.KindRecord, []map[string]any{row}, nil)
	for _, section := range build(t, in).Sections {
		for _, f := range section.Fields {
			if f.Name == "status" {
				field = f.Tone + "|" + f.Text
			}
		}
	}
	in.State.Kind = view.KindList
	for _, c := range build(t, in).Table.Rows[0].Cells {
		if c.Field == "status" {
			cell = c.Tone + "|" + c.Text
		}
	}
	if field == "" || cell == "" {
		t.Fatalf("status reads %q in the record and %q in the list: this case needs both to inspect anything", field, cell)
	}
	return field, cell
}

// absentField is an option-less field the row never carried: its text is the policy's
// own word for nothing, read from the build rather than copied out of a catalogue.
func absentField(t *testing.T, row map[string]any) string {
	t.Helper()

	for _, section := range build(t, input(view.KindRecord, []map[string]any{row}, nil)).Sections {
		for _, f := range section.Fields {
			if f.Name == "dueAt" {
				return f.Text
			}
		}
	}
	t.Fatal("the record shows no absent field to read the missing word from")
	return ""
}
