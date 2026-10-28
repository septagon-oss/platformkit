package view_test

import (
	"encoding/json"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// TestEachCrumbCarriesTheReferenceItsPositionMeans reads the three-step trail a
// record page shows when the caller named a breadcrumb root, and asks that every
// step says where it goes the way its position means: the workspace root carries no
// reference at all because it names no resource, the collection carries its resource
// with a blank id, and the record carries both. A shell follows a crumb by its
// reference, so a step that names a place it cannot be followed to is a trail that
// leads nowhere.
func TestEachCrumbCarriesTheReferenceItsPositionMeans(t *testing.T) {
	t.Parallel()

	row := map[string]any{"id": "t1", "title": "Write", "status": "open",
		"dueAt": "2026-07-01T14:12:00Z", "revision": json.Number("3")}
	trail := build(t, input(view.KindRecord, []map[string]any{row}, func(s *view.State) {
		s.Home = "Workspace"
	})).Breadcrumbs

	if len(trail) != 3 {
		t.Fatalf("a record page whose caller named a root draws %d crumbs, want the root, the collection and the record", len(trail))
	}
	root, collection, record := trail[0], trail[1], trail[2]

	if root.Label != "Workspace" || root.Ref != (view.Ref{}) {
		t.Errorf("the root crumb is %q at %+v, want the caller's label and no reference, which is where no resource was named", root.Label, root.Ref)
	}
	if collection.Label != "tasks" || collection.Ref != (view.Ref{Resource: "m/task"}) {
		t.Errorf("the collection crumb is %q at %+v, want the policy's plural name and the collection with no id", collection.Label, collection.Ref)
	}
	if record.Label != "Write" || record.Ref != (view.Ref{Resource: "m/task", ID: "t1"}) {
		t.Errorf("the record crumb is %q at %+v, want the row's title and its own resource and id", record.Label, record.Ref)
	}
}
