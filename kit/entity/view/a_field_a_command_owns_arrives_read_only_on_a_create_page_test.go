package view_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAFieldACommandOwnsArrivesReadOnlyOnACreatePage reads Description.Immutable —
// "shown read-only, never typed" — on the create page, where the create route
// refuses such a field outright, so a writable control there asks a person for a
// value the write will drop. A build that consulted the resource's list only while
// filling an update form passes every other case here: no other case names the list
// on a create page, and the plain sibling rules out passing by everything arriving
// read-only.
func TestAFieldACommandOwnsArrivesReadOnlyOnACreatePage(t *testing.T) {
	t.Parallel()

	in := input(view.KindForm, []map[string]any{{"id": "t1", "title": "Write", "dueAt": "2026-10-24T08:00:00Z", "revision": json.Number("3")}}, func(s *view.State) {
		s.Form = view.FormState{Create: true, Title: "New task"}
	})
	in.Resource.Immutable = []string{"dueAt"}
	controls := formControlsByName(build(t, in))

	owned, drawn := controls["dueAt"]
	if !drawn {
		t.Fatal("the create page drew no dueAt control, so this case would inspect nothing")
	}
	if !owned.ReadOnly {
		t.Errorf("the create page draws the command-owned dueAt as a control a person may type into; want it shown read-only")
	}
	written, drawn := controls["title"]
	if !drawn {
		t.Fatal("the create page drew no title control, so nothing here is writable to compare against")
	}
	if written.ReadOnly {
		t.Errorf("the create page draws the plain title field read-only too, so the read-only mark says nothing: %v", controls)
	}
}
