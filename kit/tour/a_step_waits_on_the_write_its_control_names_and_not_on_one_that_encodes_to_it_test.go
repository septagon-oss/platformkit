package tour_test

// Two writes can carry names that encode to one segment: "note/create" and
// "note%2Fcreate" are both names screen lets a write be mounted under, and each
// names its own write. The encoding is how a name reaches a URL, not how a write
// is identified, so a step that waits on the second cannot be left by a control
// that makes the first. A gate that compared names after encoding them would stay
// silent here and mount a tour whose step never leaves. This asks the pair of the
// frozen contract with both mounted behind the one button the step points at.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

const (
	rawNameBound = "note/create"
	rawNameOther = "note%2Fcreate"
)

func TestAStepWaitsOnTheWriteItsControlNamesAndNotOnOneThatEncodesToIt(t *testing.T) {
	gate := frozenContract{inventory: mountedUnderRawNames(t)}
	waiting := func(operation string) tour.Tour {
		return oneStepTour("save", anchoredEditor+"#action.submit", tour.Completes(tour.OperationRef(operation)))
	}
	for _, operation := range []string{rawNameBound, rawNameOther} {
		if problems := waiting(operation).DeclarationProblems("feature"); len(problems) != 0 {
			t.Fatalf("the declaration refuses the mounted write %q: %v", operation, problems)
		}
		if _, ok := gate.Operation(operation); !ok {
			t.Fatalf("the published contract carries no operation %q, so this asks nothing", operation)
		}
	}
	if problems := waiting(rawNameBound).CompositionProblems(gate); len(problems) != 0 {
		t.Fatalf("the composition refuses a step waiting on %q, the write its own control starts: %v", rawNameBound, problems)
	}
	want := `tour "feature.first", step "save": ` + anchoredEditor + `#action.submit starts "` + rawNameBound +
		`"; a step waits for the outcome of the write its own control makes, not for "` + rawNameOther + `"`
	if problems := waiting(rawNameOther).CompositionProblems(gate); len(problems) != 1 || problems[0] != want {
		t.Errorf("refusal of a step waiting on %q behind the control that starts %q = %q, want %q",
			rawNameOther, rawNameBound, problems, want)
	}
}

// mountedUnderRawNames is one composition's two writes whose names differ only in
// spelling, read back through the encoding the published contract carries, with
// the editor's one control bound to the first of them.
func mountedUnderRawNames(t *testing.T) screen.Inventory {
	t.Helper()
	editor := screen.MustView(screen.ViewSpec{Ref: anchoredEditor, Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "action.submit", Control: "button", Binding: screen.Submission(rawNameBound),
			Interactions: []screen.Interaction{screen.Activated, screen.Submitted}},
	}})
	inventory, err := screen.Publish("feature", 1, []screen.View{editor, screen.MustView(screen.ViewSpec{Ref: publishedDone, Cardinality: screen.One})},
		[]screen.Mount{
			{Operation: screen.Operation{ID: rawNameBound, Surface: "app", Method: "POST", Path: "/note", Guard: "feature:write"},
				Owner: "feature", Document: true, Responses: []screen.Response{{View: anchoredEditor, Kind: "result"}}},
			{Operation: screen.Operation{ID: rawNameOther, Surface: "app", Method: "POST", Path: "/note/publish", Guard: "feature:write"},
				Owner: "feature", Document: true, Responses: []screen.Response{{View: publishedDone, Kind: "result"}}},
		})
	if err != nil {
		t.Fatalf("mount two writes whose names encode to one segment: %v", err)
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
