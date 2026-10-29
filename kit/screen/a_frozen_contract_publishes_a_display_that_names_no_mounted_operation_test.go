package screen_test

// A display's binding target is a key this package never resolves: validateTargets
// asks the mounted operations about a binding only when it is neither navigation nor
// display, and seal gives a display its own key as its target identity. So a contract
// may publish a display keyed with a name no operation answers to, and the freeze has
// to carry that key unchanged for the consumer that reads it back rather than inferring
// it from what was mounted. The sibling case here holds a key that is mounted; this one
// holds the rule that no pass asks whether a display key exists.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
)

func TestAFrozenContractPublishesADisplayThatNamesNoMountedOperation(t *testing.T) {
	view := screen.MustView(screen.ViewSpec{Ref: "app/note/custom/editor", Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "field.state", Control: "display", Binding: screen.Display("note-export")},
	}})
	inventory, err := screen.Publish("note", 1, []screen.View{view}, exampleMounts())
	if err != nil {
		t.Fatalf("publish a contract whose display names no mounted operation: %v", err)
	}
	encoded, err := inventory.Bytes()
	if err != nil {
		t.Fatalf("encode the screen contract: %v", err)
	}
	frozen, err := screen.ParseSnapshot(encoded)
	if err != nil {
		t.Fatalf("read the frozen screen contract back: %v", err)
	}
	anchor, err := frozen.Anchor("app/note/custom/editor#field.state")
	if err != nil {
		t.Fatalf("read the display anchor back: %v", err)
	}
	if got := anchor.Binding(); got.Kind != "display" || got.Target != "note-export" {
		t.Errorf("the frozen contract carried the unmounted display anchor = %+v, want kind \"display\" and target \"note-export\"", got)
	}
}
