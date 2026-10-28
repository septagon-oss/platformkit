package view_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestACreatePageDrawsNoStepForARecordThatDoesNotExistYet asks that the trail above a
// create form stops at the collection: the page describes a record nobody has written,
// so it carries no step that points at one and no step of it carries an id. The same
// row drawn as an update form is asked alongside, so the case cannot pass because the
// builder drew no crumbs at all — the create trail has to be the update's with its
// record step gone, not the accident of an empty trail.
func TestACreatePageDrawsNoStepForARecordThatDoesNotExistYet(t *testing.T) {
	t.Parallel()

	row := map[string]any{"id": "t1", "title": "Write", "status": "open",
		"dueAt": "2026-07-01T14:12:00Z", "revision": json.Number("3")}

	create := build(t, input(view.KindForm, []map[string]any{row}, func(s *view.State) {
		s.Home = "Workspace"
		s.Form = view.FormState{Create: true, Title: "New task"}
	})).Breadcrumbs
	update := build(t, input(view.KindForm, []map[string]any{row}, func(s *view.State) {
		s.Home = "Workspace"
		s.Form = view.FormState{Title: "Edit task"}
	})).Breadcrumbs

	if len(update) != len(create)+1 {
		t.Fatalf("the create page draws %d crumbs and the update page %d, want the update's trail to be the create's plus the record it edits", len(create), len(update))
	}
	for i, crumb := range create {
		if crumb.Ref.ID != "" {
			t.Errorf("create crumb %d is %q at %+v, want no id, which is a record the caller has not written", i, crumb.Label, crumb.Ref)
		}
	}
	last := create[len(create)-1]
	if last.Label != "tasks" || last.Ref != (view.Ref{Resource: "m/task"}) {
		t.Errorf("the create page's trail ends at %q at %+v, want the collection by its policy name with no id", last.Label, last.Ref)
	}
}
