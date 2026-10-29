package tour_test

// What a step may wait a completion on is decided twice: kit/tour refuses a name
// its own rule refuses, and the composition can only mount an operation named what
// kit/screen lets it mount. Two rules with no shared source, so either can move
// alone: a name the composition mounts that the declaration refuses is a write a
// tour can never wait on, and a name the frozen contract returns under anything but
// the bytes an author wrote is a completion no tour could name. The files beside
// this one ask the declaration and kit/screen about one name at a time; this asks
// the mount and its encoding, over the same whole-name space.
import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

func TestATourWaitsOnOnlyWhatTheCompositionCanMountUnderThatName(t *testing.T) {
	for _, name := range wholeOperationNames() {
		problems := waitingOn(tour.OperationRef(name)).DeclarationProblems("feature")
		contract, err := contractMounting(t, name)
		if (err == nil) != (len(problems) == 0) {
			t.Errorf("the composition mounts an operation named %q (len %d) = %v, the tour's declaration refuses it = %v (%v): the write and the tour answer a whole name differently",
				name, len(name), err == nil, len(problems) > 0, problems)
			continue
		}
		if err != nil {
			continue
		}
		if _, ok := (frozenContract{inventory: contract}).Operation(name); !ok {
			t.Errorf("the published contract answers no operation named %q: a step cannot wait on the write it names", name)
		}
	}
}

// contractMounting is a composition's screens with one mounted write named name and
// nothing else, so the answer is that name's own, read back from the frozen
// contract kit/app hands the gate. A name screen cannot carry returns the error.
func contractMounting(t *testing.T, name string) (screen.Inventory, error) {
	t.Helper()
	inventory, err := screen.Publish("feature", 1,
		[]screen.View{screen.MustView(screen.ViewSpec{Ref: publishedCreate, Cardinality: screen.One})},
		[]screen.Mount{{Operation: screen.Operation{ID: name, Surface: "app", Method: "POST", Path: "/note", Guard: "feature:write"},
			Owner: "feature", Document: true, Responses: []screen.Response{{View: publishedCreate, Kind: "result"}}}})
	if err != nil {
		return screen.Inventory{}, err
	}
	encoded, err := inventory.Bytes()
	if err != nil {
		return screen.Inventory{}, err
	}
	frozen, err := screen.ParseSnapshot(encoded)
	if err != nil {
		return screen.Inventory{}, err
	}
	return frozen, nil
}
