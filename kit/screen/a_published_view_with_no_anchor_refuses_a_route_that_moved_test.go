package screen_test

// A published view holds the routes that serve it in its own identity, so a
// retained screen is refused when the operation behind it moves even when the
// screen publishes no anchor. Only references that name an anchor are left to
// the generated-reference checks; a repeated row publishes none, which leaves
// the view's identity as the sole witness to what serves it. A guard on that
// mount is not part of the identity, so retargeting it stays compatible.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
)

func TestAPublishedViewWithNoAnchorRefusesARouteThatMoved(t *testing.T) {
	published := publishedRows(t, rowMount("permission note:read", "/app/note/rows"))
	if err := publishedRows(t, rowMount("permission note:manage", "/app/note/rows")).Compatible(published); err != nil {
		t.Errorf("the guard on the mount serving an anchorless view = %v, want compatible: a guard is no part of what the view is", err)
	}
	if err := publishedRows(t, rowMount("permission note:read", "/app/note/other")).Compatible(published); err == nil {
		t.Error("an anchorless view kept its published name while the operation serving it moved")
	}
}

// rowMount serves the anchorless row view over the given path under the given guard.
func rowMount(guard, path string) screen.Mount {
	return screen.Mount{Owner: "note", Document: true,
		Operation: screen.Operation{ID: "note-list", Surface: "app", Method: "GET", Path: path, Guard: guard},
		Responses: []screen.Response{{View: "app/note/custom/rows", Kind: "input"}}}
}

// publishedRows publishes the repeated row view, which carries no anchor, served by mount.
func publishedRows(t *testing.T, mount screen.Mount) screen.Inventory {
	t.Helper()
	row := screen.MustView(screen.ViewSpec{Ref: "app/note/custom/rows", Cardinality: screen.Many})
	inventory, err := screen.Publish("note", 1, []screen.View{row}, []screen.Mount{mount})
	if err != nil {
		t.Fatalf("publish the anchorless row view served by %s %s: %v", mount.Operation.Method, mount.Operation.Path, err)
	}
	return inventory
}
