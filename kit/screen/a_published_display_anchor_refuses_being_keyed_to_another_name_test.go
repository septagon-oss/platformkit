package screen_test

// A display's key names no operation, so the only thing holding a published
// display to what it was published as is the anchor identity hash, which seals
// the whole binding. That is what the composition's screen compatibility check
// (apps/platformkit/screens.go) asks, and what makes a read-only element's source
// a declared change rather than a silent one. The cases beside this one hold what
// the freeze carries; this one holds that a carried key cannot change underneath
// a published identity.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
)

func TestAPublishedDisplayAnchorRefusesBeingKeyedToAnotherName(t *testing.T) {
	published := displayContract(t, "note-export")
	same := displayContract(t, "note-export")
	if err := same.Compatible(published); err != nil {
		t.Errorf("the same display key published twice = %v, want compatible", err)
	}
	err := displayContract(t, "note-import").Compatible(published)
	if err == nil {
		t.Fatal("a published display anchor accepted a second key")
	}
	if !strings.Contains(err.Error(), "app/note/custom/editor#field.state") {
		t.Errorf("the refusal for a display retarget = %q, want it to name the anchor it refuses", err)
	}
}

func displayContract(t *testing.T, key string) screen.Inventory {
	t.Helper()
	view := screen.MustView(screen.ViewSpec{Ref: "app/note/custom/editor", Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "field.state", Control: "display", Binding: screen.Display(key)},
	}})
	inventory, err := screen.Publish("note", 1, []screen.View{view}, exampleMounts())
	if err != nil {
		t.Fatalf("publish a contract whose display is keyed %q: %v", key, err)
	}
	return inventory
}
