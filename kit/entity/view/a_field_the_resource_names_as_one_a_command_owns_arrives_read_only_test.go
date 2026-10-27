package view_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAFieldTheResourceNamesAsOneACommandOwnsArrivesReadOnly reads the promise
// view.Description.Immutable makes: a field a dedicated command owns is shown on a
// form and never typed. present.Resolve computes the same fact from its own list and
// drops such a field from the layout it hands the form, so on a composed page the two
// halves agree and the description's half is never what decides; the builder reads it
// again at kit/entity/view/build.go. A build that stopped reading it would draw a
// box a person could type into beside a value only a command may write, and no case
// in this package, nor the reviewed fixture that names command-owned fields — which
// draws neither of them — would notice. The writable sibling is asked for in the same
// page so this case cannot pass by every control arriving read-only.
func TestAFieldTheResourceNamesAsOneACommandOwnsArrivesReadOnly(t *testing.T) {
	t.Parallel()

	in := input(view.KindForm, []map[string]any{{"id": "t1", "title": "Write", "dueAt": "2026-10-24T08:00:00Z", "revision": json.Number("3")}}, func(s *view.State) {
		s.Form = view.FormState{Title: "Edit task"}
	})
	in.Resource.Immutable = []string{"dueAt"}
	controls := formControlsByName(build(t, in))

	owned, drawn := controls["dueAt"]
	if !drawn {
		t.Fatal("the form drew no dueAt control, so this case would inspect nothing")
	}
	if !owned.ReadOnly {
		t.Errorf("the form draws the command-owned dueAt as a control a person may type into; want it shown read-only")
	}
	written, drawn := controls["title"]
	if !drawn {
		t.Fatal("the form drew no title control, so nothing here is writable to compare against")
	}
	if written.ReadOnly {
		t.Errorf("the form draws the plain title field read-only too, so the read-only mark says nothing: %v", controls)
	}
}
