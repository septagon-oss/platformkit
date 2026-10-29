package screen_test

// A display's binding target is a key no pass resolves: validateTargets skips a
// display entirely and seal gives one its own key as its target identity, so a
// contract may carry a display keyed with a name that was never mounted. A
// consumer therefore reads the binding kind back instead of inferring it from the
// operations, and a tour is gated against the frozen contract rather than a view
// in memory, so the freeze has to carry the kind and the key.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
)

func TestAFrozenContractCarriesADisplayAnchorWithTheKeyItWasGiven(t *testing.T) {
	view := screen.MustView(screen.ViewSpec{Ref: "app/note/custom/editor", Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "field.total", Control: "display", Binding: screen.Display("note-save")},
	}})
	inventory, err := screen.Publish("note", 1, []screen.View{view}, exampleMounts())
	if err != nil {
		t.Fatalf("publish a contract whose display is keyed to the mounted write: %v", err)
	}
	encoded, err := inventory.Bytes()
	if err != nil {
		t.Fatalf("encode the screen contract: %v", err)
	}
	frozen, err := screen.ParseSnapshot(encoded)
	if err != nil {
		t.Fatalf("read the frozen screen contract back: %v", err)
	}
	anchor, err := frozen.Anchor("app/note/custom/editor#field.total")
	if err != nil {
		t.Fatalf("read the display anchor back: %v", err)
	}
	if got := anchor.Binding(); got.Kind != "display" || got.Target != "note-save" {
		t.Errorf("the frozen contract carried the display anchor = %+v, want kind \"display\" and target \"note-save\"", got)
	}
}
