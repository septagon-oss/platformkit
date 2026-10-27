package view_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestACommandOwnedFieldArrivesReadOnlyWhenTheRowOmitsIt reads the promise
// view.Description.Immutable makes on an update page whose row is present but does not
// carry the command-owned field: a read projected onto fewer columns than the form
// takes, or a row written before the field existed. The mark belongs to the resource's
// list, so a row that says nothing about that field says nothing about the mark. A
// build that consulted the list wherever a row was present, and there only for the
// values that row held, drew a typable box on this page while every case that hands
// Build no row at all, or a row carrying the field, stayed green. The writable sibling
// is asked for in the same page so this case cannot pass by every control arriving
// read-only.
func TestACommandOwnedFieldArrivesReadOnlyWhenTheRowOmitsIt(t *testing.T) {
	t.Parallel()

	in := input(view.KindForm, []map[string]any{{"id": "t1", "title": "Write", "status": "open"}},
		func(s *view.State) { s.Form = view.FormState{Title: "Edit task"} })
	in.Resource.Immutable = []string{"dueAt"}
	controls := formControlsByName(build(t, in))

	owned, drawn := controls["dueAt"]
	if !drawn {
		t.Fatal("the update page whose row omits dueAt drew no dueAt control, so this case would inspect nothing")
	}
	if owned.Value != nil {
		t.Errorf("the control for a field the row does not carry arrived holding a value: %+v", owned)
	}
	if !owned.ReadOnly {
		t.Errorf("the update page whose row omits dueAt draws the command-owned dueAt as a control a person may type into; want it shown read-only because the resource, not the row, names it")
	}
	if plain := controls["title"]; plain.ReadOnly {
		t.Errorf("the update page whose row omits dueAt draws the plain title field read-only too, so the mark says nothing: %+v", plain)
	}
}
