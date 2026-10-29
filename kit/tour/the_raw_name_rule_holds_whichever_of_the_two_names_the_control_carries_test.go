package tour_test

// A step is held to the write its control names whichever of the two spellings the
// composition mounted behind that control: "note/create" and "note%2Fcreate" name
// two writes, so behind a control bound to the encoded one a step that waits on it
// is played and a step that waits on the raw one is refused. A gate that undid the
// encoding on one side of the comparison only — the binding it reads off the screen
// contract, or the name the step declares — would mount a tour whose step never
// leaves, silently, in the direction it left unchecked.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

const (
	rawSpelling = "note/create"
	encodedName = "note%2Fcreate"
)

func TestTheRawNameRuleHoldsWhicheverOfTheTwoNamesTheControlCarries(t *testing.T) {
	for _, tc := range []struct{ control, waited, want string }{
		{control: encodedName, waited: encodedName, want: ""},
		{control: encodedName, waited: rawSpelling, want: `tour "feature.first", step "save": ` + anchoredEditor +
			`#action.submit starts "` + encodedName + `"; a step waits for the outcome of the write its own control makes, not for "` +
			rawSpelling + `"`},
	} {
		gate := frozenContract{inventory: contractBoundAndBothMounted(t, tc.control)}
		step := oneStepTour("save", anchoredEditor+"#action.submit", tour.Completes(tour.OperationRef(tc.waited)))
		if problems := step.DeclarationProblems("feature"); len(problems) != 0 {
			t.Fatalf("the declaration refuses the mounted write %q: %v", tc.waited, problems)
		}
		if _, ok := gate.Operation(tc.control); !ok {
			t.Fatalf("the published contract carries no operation %q, so this asks nothing", tc.control)
		}
		problems := step.CompositionProblems(gate)
		if tc.want == "" {
			if len(problems) != 0 {
				t.Errorf("the composition refuses a step waiting on %q, the write its control %q posts: %v", tc.waited, tc.control, problems)
			}
		} else if len(problems) != 1 || problems[0] != tc.want {
			t.Errorf("refusal of a step waiting on %q behind the control that starts %q = %q, want %q", tc.waited, tc.control, problems, tc.want)
		}
	}
}

// contractBoundAndBothMounted is the two writes whose names differ only in spelling,
// with the editor's one control bound to the write named by control, read back
// through the encoding the published contract carries.
func contractBoundAndBothMounted(t *testing.T, control string) screen.Inventory {
	t.Helper()
	editor := screen.MustView(screen.ViewSpec{Ref: anchoredEditor, Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "action.submit", Control: "button", Binding: screen.Submission(control),
			Interactions: []screen.Interaction{screen.Activated, screen.Submitted}},
	}})
	inventory, err := screen.Publish("feature", 1, []screen.View{editor, screen.MustView(screen.ViewSpec{Ref: publishedDone, Cardinality: screen.One})},
		[]screen.Mount{
			{Operation: screen.Operation{ID: rawSpelling, Surface: "app", Method: "POST", Path: "/note", Guard: "feature:write"},
				Owner: "feature", Document: true, Responses: []screen.Response{{View: anchoredEditor, Kind: "result"}}},
			{Operation: screen.Operation{ID: encodedName, Surface: "app", Method: "POST", Path: "/note/publish", Guard: "feature:write"},
				Owner: "feature", Document: true, Responses: []screen.Response{{View: publishedDone, Kind: "result"}}},
		})
	if err != nil {
		t.Fatalf("mount the write %q behind the editor's control: %v", control, err)
	}
	encoded, err := inventory.Bytes()
	if err != nil {
		t.Fatalf("encode the screen contract: %v", err)
	}
	frozen, err := screen.ParseSnapshot(encoded)
	if err != nil {
		t.Fatalf("read the frozen screen contract back: %v", err)
	}
	return frozen
}
