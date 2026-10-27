package view_test

import (
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// dayShown is one zone of a page showing one field's calendar day: where it is shown,
// the kind it claims and the value it carries.
type dayShown struct {
	where string
	kind  view.ValueKind
	value any
}

// TestACalendarDayReachesItsCellAndItsControlCutInTheZoneTheCallerLoaded reads one
// stored moment out of the three zones a page shows a value in — a record's field, a
// list's cell and a form's control — and asks that all three write one calendar day: the
// caller's zone's own. A day is the only value in the domain whose spelling depends on a
// location the caller supplied, so a zone honoured in one zone of the page and dropped in
// another leaves a person reading the next day in a table and the day before it in the
// form of the same record, each zone true to itself. The zone is fixed rather than loaded,
// so the answer is the same on a host with no zone database, and every zone of the page
// refuses the case when it shows no day at all, so it cannot pass by inspecting nothing.
func TestACalendarDayReachesItsCellAndItsControlCutInTheZoneTheCallerLoaded(t *testing.T) {
	t.Parallel()

	row := map[string]any{"id": "t1", "title": "n", "dueAt": "2026-07-01T23:30:00Z", "dayAt": "2026-07-01T23:30:00Z"}
	in := domainInput(row, time.FixedZone("east", 5*3600))
	const want = "2026-07-02"

	list := in
	list.State.Kind = view.KindList
	form := in
	form.State.Kind = view.KindForm
	form.State.Form = view.FormState{Create: true, Title: "New task"}

	for _, shown := range []dayShown{
		dayInRecord(t, "record field", in),
		dayInList(t, "list cell", list),
		dayInForm(t, "form control", form),
	} {
		if shown.kind != view.ValueDate {
			t.Errorf("the %s carries the day as %s, want %s", shown.where, shown.kind, view.ValueDate)
		}
		if shown.value != want {
			t.Errorf("the %s shows %v, want %s, which is the caller's own zone's day for that moment",
				shown.where, shown.value, want)
		}
	}
}

// dayInRecord is the calendar day a record page shows in one of its sections' fields.
func dayInRecord(t *testing.T, where string, in view.Input) dayShown {
	t.Helper()
	for _, section := range build(t, in).Sections {
		for _, f := range section.Fields {
			if f.Name == "dayAt" {
				return dayShown{where: where, kind: f.ValueKind, value: f.Value}
			}
		}
	}
	t.Fatalf("the %s page shows no dayAt field, so this case would inspect nothing", where)
	return dayShown{}
}

// dayInList is the calendar day a list page shows in one of a row's cells.
func dayInList(t *testing.T, where string, in view.Input) dayShown {
	t.Helper()
	for _, row := range build(t, in).Table.Rows {
		for _, cell := range row.Cells {
			if cell.Field == "dayAt" {
				return dayShown{where: where, kind: cell.ValueKind, value: cell.Value}
			}
		}
	}
	t.Fatalf("the %s page shows no dayAt cell, so this case would inspect nothing", where)
	return dayShown{}
}

// dayInForm is the calendar day a form page starts its control at.
func dayInForm(t *testing.T, where string, in view.Input) dayShown {
	t.Helper()
	for _, c := range build(t, in).Form.Controls {
		if c.Name == "dayAt" {
			return dayShown{where: where, kind: c.ValueKind, value: c.Value}
		}
	}
	t.Fatalf("the %s page shows no dayAt control, so this case would inspect nothing", where)
	return dayShown{}
}
