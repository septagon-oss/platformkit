package screen_test

// The cardinality a reference carries is part of what it serves: one record or a
// repeated row. A retained view that keeps its name, its owner, its kind and the
// operation serving it yet flips that cardinality is a different reference, and a
// client generated against the published one would keep rendering a list as a
// single record. Anchors cannot witness the flip, because a repeated row publishes
// none, so only the view's own identity can refuse it.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
)

func TestAPublishedViewRefusesFlippingItsCardinality(t *testing.T) {
	published := rowsContract(t, screen.One)
	if err := rowsContract(t, screen.Many).Compatible(published); err == nil {
		t.Error("a published view kept its identity while one record became a repeated row")
	}
	if err := rowsContract(t, screen.One).Compatible(published); err != nil {
		t.Errorf("the same contract published twice = %v, want compatible", err)
	}
}

// rowsContract publishes app/note/custom/rows at the given cardinality, served by note-list.
func rowsContract(t *testing.T, cardinality screen.Cardinality) screen.Inventory {
	t.Helper()
	inventory, err := screen.Publish("note", 1,
		[]screen.View{screen.MustView(screen.ViewSpec{Ref: "app/note/custom/rows", Cardinality: cardinality})},
		[]screen.Mount{{Owner: "note", Document: true,
			Operation: screen.Operation{ID: "note-list", Surface: "app", Method: "GET", Path: "/app/note/rows", Guard: "permission note:read"},
			Responses: []screen.Response{{View: "app/note/custom/rows", Kind: "input"}}}})
	if err != nil {
		t.Fatalf("publish app/note/custom/rows as %s served by GET /app/note/rows: %v", cardinality, err)
	}
	return inventory
}
