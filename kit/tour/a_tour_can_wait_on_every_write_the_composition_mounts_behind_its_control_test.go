package tour_test

// A step may wait a completion on a write only if the composition mounts it and
// the control the step points at makes it. Those are two reads of one name that
// share no source: validateOperation for the mount, validateBinding for the
// control, and the frozen contract's encoding between them. A name the
// composition can declare must leave the gate silent, and one it cannot declare
// must leave the declaration's refusal; a name the two answer differently is a
// tour an author can write and cannot boot. screen's binding constructors panic
// for a refused name, so the composition's answer is read from the panic as well
// as from the error.
import (
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

// anchoredEditor is the view whose one control the step in waitingOn points at.
const anchoredEditor = "app/feature/custom/editor"

func TestATourCanWaitOnEveryWriteTheCompositionMountsBehindItsControl(t *testing.T) {
	for _, name := range wholeOperationNames() {
		problems := waitingOn(tour.OperationRef(name)).DeclarationProblems("feature")
		contract, err := contractBinding(t, name)
		if (err == nil) != (len(problems) == 0) {
			t.Errorf("the composition binds a control to %q (len %d) = %v, the tour's declaration refuses the same name = %v (%v): the control and the tour answer a whole name differently",
				name, len(name), err == nil, len(problems) > 0, problems)
			continue
		}
		if err != nil {
			continue
		}
		if refused := waitingOn(tour.OperationRef(name)).CompositionProblems(frozenContract{inventory: contract}); len(refused) != 0 {
			t.Errorf("the composition gate refuses a step that waits on %q behind the control that makes it: %v", name, refused)
		}
	}
}

// contractBinding is a published contract whose one control is bound to the write
// named name, encoded and read back the way the composition hands it to the gate.
// A name screen cannot carry is refused — by the binding constructor or by the
// publish — and reaches the caller as an error either way.
func contractBinding(t *testing.T, name string) (contract screen.Inventory, err error) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			contract, err = screen.Inventory{}, fmt.Errorf("screen: %v", p)
		}
	}()
	view := screen.MustView(screen.ViewSpec{Ref: anchoredEditor, Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "action.submit", Control: "button", Binding: screen.Submission(name),
			Interactions: []screen.Interaction{screen.Activated, screen.Submitted}},
	}})
	inventory, err := screen.Publish("feature", 1, []screen.View{view},
		[]screen.Mount{{Operation: screen.Operation{ID: name, Surface: "app", Method: "POST", Path: "/note", Guard: "feature:write"},
			Owner: "feature", Document: true, Responses: []screen.Response{{View: anchoredEditor, Kind: "result"}}}})
	if err != nil {
		return screen.Inventory{}, err
	}
	encoded, err := inventory.Bytes()
	if err != nil {
		return screen.Inventory{}, err
	}
	return screen.ParseSnapshot(encoded)
}
