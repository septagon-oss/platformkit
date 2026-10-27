package view_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestACommandOwnedFieldArrivesReadOnlyOnACreatePageThatCarriesNoRow reads the
// promise view.Description.Immutable makes where a create page really is empty: the
// reviewed fixture for command-owned fields carries no row, so a mark that came from
// a row's value rather than the resource's list would answer nothing here while both
// cases that do carry a row stayed green. The writable sibling is asked for in the
// same page so this case cannot pass by every control arriving read-only.
func TestACommandOwnedFieldArrivesReadOnlyOnACreatePageThatCarriesNoRow(t *testing.T) {
	t.Parallel()

	in := input(view.KindForm, nil, func(s *view.State) {
		s.Form = view.FormState{Create: true, Title: "New task"}
	})
	in.Resource.Immutable = []string{"dueAt"}
	controls := formControlsByName(build(t, in))

	owned, drawn := controls["dueAt"]
	if !drawn {
		t.Fatal("a create page with no row drew no dueAt control, so this case would inspect nothing")
	}
	if !owned.ReadOnly {
		t.Errorf("the empty create page draws the command-owned dueAt as a control a person may type into; want it shown read-only with no row to read")
	}
	if plain := controls["title"]; plain.ReadOnly {
		t.Errorf("the empty create page draws the plain title field read-only too, so the mark says nothing: %+v", plain)
	}
}
