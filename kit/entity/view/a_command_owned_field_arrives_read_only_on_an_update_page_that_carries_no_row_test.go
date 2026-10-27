package view_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestACommandOwnedFieldArrivesReadOnlyOnAnUpdatePageThatCarriesNoRow reads the
// promise view.Description.Immutable makes on the update leg where the page holds no
// row: a refused write redrawn after the read came back empty, or a surface composed
// before the row exists. The mark belongs to the resource's list alone, so a row that
// is not there takes nothing away from it — the control is still one a person may not
// type into. A build that read the list only where a row carried a value drew a
// typable box on that page and stayed green behind every case that hands Build a row.
// The writable sibling is asked for in the same page so this case cannot pass by every
// control arriving read-only.
func TestACommandOwnedFieldArrivesReadOnlyOnAnUpdatePageThatCarriesNoRow(t *testing.T) {
	t.Parallel()

	in := input(view.KindForm, nil, func(s *view.State) {
		s.Form = view.FormState{Title: "Edit task"}
	})
	in.Resource.Immutable = []string{"dueAt"}
	controls := formControlsByName(build(t, in))

	owned, drawn := controls["dueAt"]
	if !drawn {
		t.Fatal("an update page with no row drew no dueAt control, so this case would inspect nothing")
	}
	if !owned.ReadOnly {
		t.Errorf("the update page with no row draws the command-owned dueAt as a control a person may type into; want it shown read-only with no row to read")
	}
	if plain := controls["title"]; plain.ReadOnly {
		t.Errorf("the update page with no row draws the plain title field read-only too, so the mark says nothing: %+v", plain)
	}
}
