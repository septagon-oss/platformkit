package view_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAnUpdatePageWithNoTitleDrawsNoStepThatNamesNothing asks the one page kind that
// can reach a crumb trail with no title to show: a form over an existing record whose
// caller named no form title. The builder adds a record step only when it has a title
// for it, so such a page trails its root and its collection, and the record stays
// named by the page's own reference rather than by a crumb with no label — the same
// step that names nothing a caller who gave no root is refused. A record step is not
// drawn from a title nobody gave.
func TestAnUpdatePageWithNoTitleDrawsNoStepThatNamesNothing(t *testing.T) {
	t.Parallel()

	trail := build(t, input(view.KindForm, []map[string]any{
		{"id": "t1", "title": "Write", "status": "open", "revision": json.Number("3")},
	}, func(s *view.State) {
		s.Home = "Workspace"
		s.Form = view.FormState{Title: ""}
	})).Breadcrumbs

	if len(trail) != 2 {
		t.Fatalf("an update page with no form title named draws %d crumbs %v, want its root and its collection and no step for a title the caller never gave", len(trail), trail)
	}
	for i, crumb := range trail {
		if crumb.Label == "" {
			t.Errorf("crumb %d of an update page with no form title named is %q, want no step that names nothing", i, crumb.Label)
		}
	}
	if trail[1].Ref != (view.Ref{Resource: "m/task"}) {
		t.Errorf("the deepest crumb of an update page with no form title named is at %+v, want the collection itself, with no id to stand in for the title", trail[1].Ref)
	}
}
