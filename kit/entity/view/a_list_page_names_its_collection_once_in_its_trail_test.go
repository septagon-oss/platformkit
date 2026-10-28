package view_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestAListPageNamesItsCollectionOnceInItsTrail asks that the trail above a
// collection name each place it passes through once. A list page is about the
// collection and shows many of its records, so the deepest step it may draw is the
// collection itself: a page whose title is the policy's plural name must not repeat
// that name, and no step of it may name one of the rows the read returned. A shell
// renders a trail as "you are here" by walking it, so a step that names the place the
// page already stands on reads as a deeper level a person can navigate to and is not.
func TestAListPageNamesItsCollectionOnceInItsTrail(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{"id": "t1", "title": "Write", "status": "open", "dueAt": "2026-07-01T14:12:00Z",
			"revision": json.Number("3")},
		{"id": "t2", "title": "Ship", "status": "done", "revision": json.Number("1")},
	}
	trail := build(t, input(view.KindList, rows, func(s *view.State) {
		s.Home = "Workspace"
	})).Breadcrumbs

	if len(trail) != 2 {
		t.Fatalf("a list page whose caller named a root draws %d crumbs %v, want its root and its collection and no step deeper", len(trail), trail)
	}
	if trail[0].Label != "Workspace" || trail[0].Ref != (view.Ref{}) {
		t.Errorf("the root crumb is %q at %+v, want the caller's label and no reference", trail[0].Label, trail[0].Ref)
	}
	if trail[1].Label != "tasks" || trail[1].Ref != (view.Ref{Resource: "m/task"}) {
		t.Errorf("the deepest crumb is %q at %+v, want the collection named once, by its policy name and its own resource with no id", trail[1].Label, trail[1].Ref)
	}
	for i, crumb := range trail {
		if crumb.Ref.ID != "" {
			t.Errorf("list crumb %d is %q at %+v, want no id, which is one row of many rather than the page's own record", i, crumb.Label, crumb.Ref)
		}
	}
}
