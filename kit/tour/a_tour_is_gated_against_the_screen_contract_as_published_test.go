package tour_test

// A step that waits for a completion is held to the write the control it points at
// posts, and the gate reads that fact off the composition's screen contract. The
// contract a tour is really played against is the frozen one — published, encoded,
// read back — so this asks the gate of that value rather than of a double: the
// operation a control's binding names has to reach the gate unchanged, or a tour
// that waits on a save is refused over a name nobody wrote.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

const (
	publishedCreate    = "app/feature/custom/create"
	publishedDone      = "app/feature/custom/done"
	publishedCreateOp  = "screen-feature-note-create"
	publishedPublishOp = "screen-feature-note-publish"
)

// frozenContract is the view of the screen contract kit/app hands the gate: the
// frozen inventory answers views and anchors, and only the operations its snapshot
// carries are ones a step may wait a completion on.
type frozenContract struct{ inventory screen.Inventory }

func (p frozenContract) View(ref screen.ViewRef) (screen.View, error) {
	return p.inventory.View(ref)
}

func (p frozenContract) Anchor(ref screen.AnchorRef) (screen.Anchor, error) {
	return p.inventory.Anchor(ref)
}

func (p frozenContract) Operation(id string) (screen.Operation, bool) {
	for _, op := range p.inventory.Snapshot().Operations {
		if op.ID == id {
			return op, true
		}
	}
	return screen.Operation{}, false
}

// frozenScreenContract is a composition's own screens: a create form whose Save posts one
// write and whose title field carries that same write, a second mounted write the
// form never starts, and the page a save lands on.
func frozenScreenContract(t *testing.T) screen.Inventory {
	t.Helper()
	create := screen.MustView(screen.ViewSpec{Ref: publishedCreate, Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "action.submit", Control: "button", Binding: screen.Submission(publishedCreateOp),
			Interactions: []screen.Interaction{screen.Activated, screen.Submitted}},
		{Name: "field.title", Control: "text", Binding: screen.Value(publishedCreateOp, []string{"title"}),
			Interactions: []screen.Interaction{screen.Committed}},
	}})
	inventory, err := screen.Publish("feature", 1, []screen.View{
		create,
		screen.MustView(screen.ViewSpec{Ref: publishedDone, Cardinality: screen.One}),
	}, []screen.Mount{
		{Operation: screen.Operation{ID: "feature-editor", Surface: "app", Method: "GET", Path: "/note/new", Guard: "feature:read"},
			Owner: "feature", Document: true, Responses: []screen.Response{{View: publishedCreate, Kind: "input"}}},
		{Operation: screen.Operation{ID: publishedCreateOp, Surface: "app", Method: "POST", Path: "/note", Guard: "feature:write"},
			Owner: "feature", Document: true, Responses: []screen.Response{{View: publishedDone, Kind: "redirect"}}},
		{Operation: screen.Operation{ID: publishedPublishOp, Surface: "app", Method: "POST", Path: "/note/publish", Guard: "feature:write"},
			Owner: "feature", Document: true, Responses: []screen.Response{{View: publishedDone, Kind: "result"}}},
	})
	if err != nil {
		t.Fatalf("publish the screen contract: %v", err)
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

func oneStepTour(id string, anchor screen.AnchorRef, advance tour.Advance) tour.Tour {
	return tour.Tour{
		Ref:   tour.Ref{ID: "feature.first", Edition: 1},
		Title: text("feature.first", "Add your first note"),
		Steps: []tour.Step{tour.Spotlight(tour.StepID(id), anchor, tour.Content{
			Title: text("feature.first.step."+id, "Save it"),
			Body:  text("feature.first.step."+id+".body", "Saving writes the note."),
		}, advance, tour.AnchoredOptions{})},
	}
}

func TestATourIsGatedAgainstTheScreenContractAsPublished(t *testing.T) {
	frozen := frozenContract{inventory: frozenScreenContract(t)}
	for _, tc := range []struct {
		name      string
		tour      tour.Tour
		wantFirst string
	}{
		{"a step waits on the write its own button posts",
			oneStepTour("save", publishedCreate+"#action.submit", tour.Completes(publishedCreateOp)), ""},
		{"a step waits on the write the field it points at carries",
			oneStepTour("title", publishedCreate+"#field.title", tour.Completes(publishedCreateOp)), ""},
		{"a step waits on a write its control never starts",
			oneStepTour("save", publishedCreate+"#action.submit", tour.Completes(publishedPublishOp)),
			`tour "feature.first", step "save": ` + publishedCreate + `#action.submit starts "` + publishedCreateOp +
				`"; a step waits for the outcome of the write its own control makes, not for "` + publishedPublishOp + `"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if problems := tc.tour.DeclarationProblems("feature"); len(problems) != 0 {
				t.Fatalf("the declaration refused the tour: %v", problems)
			}
			problems := tc.tour.CompositionProblems(frozen)
			if tc.wantFirst == "" {
				if len(problems) != 0 {
					t.Fatalf("the published contract refused the tour: %v", problems)
				}
				return
			}
			if len(problems) == 0 || problems[0] != tc.wantFirst {
				t.Fatalf("refusal = %q, want first %q", problems, tc.wantFirst)
			}
		})
	}
}
