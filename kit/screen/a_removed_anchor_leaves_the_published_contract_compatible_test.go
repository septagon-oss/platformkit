package screen_test

// Compatible judges only the references a contract still publishes: a removed
// one is left to the generated-reference and consumer checks, which is what lets
// a module take an anchor out at all. The composition asks this door of every
// retained screen (apps/platformkit/screens.go), so a refusal over a removal
// would stop the application starting over a change that retargets nothing.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
)

func TestARemovedAnchorLeavesThePublishedContractCompatible(t *testing.T) {
	published := editorAnchors(t, true, []string{"title"})
	if err := editorAnchors(t, false, []string{"title"}).Compatible(published); err != nil {
		t.Errorf("a published anchor removed = %v, want compatible: a removal belongs to another check", err)
	}
	if err := editorAnchors(t, false, []string{"secret"}).Compatible(published); err == nil {
		t.Error("a contract that removed one anchor and re-bound the other to a different field was accepted")
	}
}

// editorAnchors publishes the editor view with its text field bound to the given
// path of the write, and with the save button included or left out.
func editorAnchors(t *testing.T, saveButton bool, fieldPath []string) screen.Inventory {
	t.Helper()
	anchors := []screen.AnchorSpec{{Name: "field.title", Control: "text", Binding: screen.Value("note-save", fieldPath), Interactions: []screen.Interaction{screen.Committed}}}
	if saveButton {
		anchors = append(anchors, screen.AnchorSpec{Name: "action.submit", Control: "button", Binding: screen.Submission("note-save"), Interactions: []screen.Interaction{screen.Activated, screen.Submitted}})
	}
	view := screen.MustView(screen.ViewSpec{Ref: "app/note/custom/editor", Cardinality: screen.One, Anchors: anchors})
	inventory, err := screen.Publish("note", 1, []screen.View{view}, exampleMounts())
	if err != nil {
		t.Fatalf("publish a contract with the save button %v and the field path %v: %v", saveButton, fieldPath, err)
	}
	return inventory
}
