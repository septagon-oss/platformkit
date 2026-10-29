package tour_test

// A display's binding target is a name alone on both passes that could have
// checked it against the operations a composition mounted: validateTargets skips
// a display entirely and seal resolves an anchor's target through the mounted
// operations for every binding kind but a display, which keeps its own key. So
// the refusal a completion step behind a read-only display gets is kit/tour's
// alone, and the value a tour is really played against is the frozen contract
// rather than a descriptor in memory. Ask that value: a freeze that stopped
// carrying the binding kind would leave a step no person can ever leave mounted,
// with every in-memory case still green.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

func TestTheFrozenContractRefusesACompletionStepBehindAReadOnlyDisplay(t *testing.T) {
	view := screen.MustView(screen.ViewSpec{Ref: publishedCreate, Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "field.total", Control: "display", Binding: screen.Display(publishedCreateOp)},
	}})
	inventory, err := screen.Publish("feature", 1, []screen.View{view}, []screen.Mount{
		{Operation: screen.Operation{ID: publishedCreateOp, Surface: "app", Method: "POST", Path: "/note", Guard: "feature:write"},
			Owner: "feature", Document: true, Responses: []screen.Response{{View: publishedCreate, Kind: "result"}}},
	})
	if err != nil {
		t.Fatalf("publish a contract whose display key is the mounted write: %v", err)
	}
	encoded, err := inventory.Bytes()
	if err != nil {
		t.Fatalf("encode the screen contract: %v", err)
	}
	frozen, err := screen.ParseSnapshot(encoded)
	if err != nil {
		t.Fatalf("read the frozen screen contract back: %v", err)
	}
	want := `tour "feature.first", step "total": ` + publishedCreate + `#field.total is a read-only display and starts no write; a step that waits for a completion has to point at the control that makes it`
	problems := oneStepTour("total", screen.AnchorRef(publishedCreate+"#field.total"), tour.Completes(publishedCreateOp)).
		CompositionProblems(frozenContract{inventory: frozen})
	if len(problems) != 1 || problems[0] != want {
		t.Errorf("the frozen contract answered a step waiting on %s behind the display = %q, want %q", publishedCreateOp, problems, want)
	}
}
