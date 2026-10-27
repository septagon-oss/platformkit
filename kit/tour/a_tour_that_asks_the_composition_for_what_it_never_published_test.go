package tour_test

// What a composition published is the second pass's whole world, so the double
// here is built out of the same screen values a composition would freeze: the
// anchor descriptors are screen's own, which decides which interactions a binding
// may report and that a repeated row publishes none. The gate reads both answers
// off the published descriptor, so no row below can pass on a hand-written story.

import (
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

const (
	editorView = "app/feature/custom/editor"
	doneView   = "app/feature/custom/done"
	rowView    = "app/task/resource/task/list"
	writeOp    = "feature-note-create"
	anotherOp  = "feature-note-publish"
	readOp     = "feature-editor"
)

type publishedScreens struct {
	views map[screen.ViewRef]screen.View
	ops   map[string]screen.Operation
}

func (p publishedScreens) View(ref screen.ViewRef) (screen.View, error) {
	view, ok := p.views[ref]
	if !ok {
		return screen.View{}, fmt.Errorf("screen: missing view %s", ref)
	}
	return view, nil
}

func (p publishedScreens) Anchor(ref screen.AnchorRef) (screen.Anchor, error) {
	view, _ := cutAnchor(ref)
	v, err := p.View(view)
	if err != nil {
		return screen.Anchor{}, err
	}
	return v.Anchor(ref)
}

func (p publishedScreens) Operation(id string) (screen.Operation, bool) {
	op, ok := p.ops[id]
	return op, ok
}

func cutAnchor(ref screen.AnchorRef) (screen.ViewRef, string) {
	value := string(ref)
	for i := range len(value) {
		if value[i] == '#' {
			return screen.ViewRef(value[:i]), value[i+1:]
		}
	}
	return screen.ViewRef(value), ""
}

// composition is the fixture's own screens: an editor with one field and two
// controls, the page a save lands on, one list that renders repeated rows, and the
// two operations the editor's controls reach — the write and a read.
func composition() tour.Composition {
	editor := screen.MustView(screen.ViewSpec{Ref: editorView, Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "field.title", Control: "text", Binding: screen.Value(writeOp, []string{"title"}), Interactions: []screen.Interaction{screen.Committed}},
		{Name: "action.submit", Control: "button", Binding: screen.Submission(writeOp), Interactions: []screen.Interaction{screen.Activated, screen.Submitted}},
		{Name: "action.new", Control: "link", Binding: screen.Navigation(doneView), Interactions: []screen.Interaction{screen.Activated}},
	}})
	views := map[screen.ViewRef]screen.View{
		editorView: editor,
		doneView:   screen.MustView(screen.ViewSpec{Ref: doneView, Cardinality: screen.One}),
		rowView:    screen.MustView(screen.ViewSpec{Ref: rowView, Cardinality: screen.Many}),
	}
	return publishedScreens{views: views, ops: map[string]screen.Operation{
		writeOp:   {ID: writeOp, Surface: "app", Method: "POST", Path: "/note", Guard: "feature:write"},
		anotherOp: {ID: anotherOp, Surface: "app", Method: "POST", Path: "/note/publish", Guard: "feature:write"},
		readOp:    {ID: readOp, Surface: "app", Method: "GET", Path: "/editor", Guard: "feature:read"},
	}}
}

func anchoredStep(id tour.StepID, anchor screen.AnchorRef, advance tour.Advance) tour.Step {
	return tour.Spotlight(id, anchor, tour.Content{
		Title: text("feature.first.step."+string(id), "A step"),
		Body:  text("feature.first.step."+string(id)+".body", "What to do here."),
	}, advance, tour.AnchoredOptions{})
}

func TestAValidTourAsksNothingItsCompositionNeverPublished(t *testing.T) {
	if problems := fixtureTour().CompositionProblems(composition()); len(problems) != 0 {
		t.Fatalf("the fixture tour is refused by the composition: %v", problems)
	}
}

func TestATourAsksForWhatTheCompositionNeverPublished(t *testing.T) {
	for _, tc := range []struct {
		name     string
		wrong    func(t *tour.Tour)
		sentence string
	}{
		{"trigger's view is not published", func(t *tour.Tour) {
			start := tour.OnVisit("app/feature/custom/nowhere")
			t.Automatic = &start
		}, `tour "feature.first": its automatic trigger's view app/feature/custom/nowhere is not a view of this composition`},
		{"modal's view is not published", func(t *tour.Tour) {
			replace(t, "done", tour.Modal("done", "app/feature/custom/nowhere", tour.Content{
				Title: text("feature.first.step.done", "That is the shape of it"),
				Body:  text("feature.first.step.done.body", "Come back to the help button whenever you want it again.")}))
		}, `tour "feature.first", step "done": its view app/feature/custom/nowhere is not a view of this composition`},
		{"anchor is not published", func(t *tour.Tour) {
			replace(t, "save", anchoredStep("save", editorView+"#action.delete", tour.Completes(writeOp)))
		}, `tour "feature.first", step "save": ` + editorView + `#action.delete is not an anchor of this composition`},
		{"anchor is a repeated row", func(t *tour.Tour) {
			t.Steps = []tour.Step{anchoredStep("row", rowView+"#action.open", tour.On(screen.Activated))}
		}, `tour "feature.first", step "row": ` + rowView + `#action.open names no anchor; ` + rowView + ` renders repeated rows and a repeated row carries no anchor until the platform has a typed instance binding`},
		{"anchor does not report the interaction", func(t *tour.Tour) {
			replace(t, "save", anchoredStep("save", editorView+"#action.submit", tour.On(screen.Committed)))
		}, `tour "feature.first", step "save": anchor ` + editorView + `#action.submit does not report "committed"`},
		{"operation is not mounted", func(t *tour.Tour) {
			replace(t, "save", anchoredStep("save", editorView+"#action.submit", tour.Completes("feature-note-delete")))
		}, `tour "feature.first", step "save": operation "feature-note-delete" is not an operation of this composition`},
		{"operation is a read", func(t *tour.Tour) {
			replace(t, "save", anchoredStep("save", editorView+"#action.submit", tour.Completes(readOp)))
		}, `tour "feature.first", step "save": operation "feature-editor" answers GET and can never be observed succeeding; an outcome is admitted only for a write`},
		{"completion waits behind a link", func(t *tour.Tour) {
			t.Steps = []tour.Step{anchoredStep("open", editorView+"#action.new", tour.Completes(writeOp))}
		}, `tour "feature.first", step "open": ` + editorView + `#action.new is a link and starts no write; a step that waits for a completion has to point at the control that makes it`},
		{"completion waits on a write the control does not start", func(t *tour.Tour) {
			replace(t, "save", anchoredStep("save", editorView+"#action.submit", tour.Completes(anotherOp)))
		}, `tour "feature.first", step "save": ` + editorView + `#action.submit starts "feature-note-create"; a step waits for the outcome of the write its own control makes, not for "feature-note-publish"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			declared := fixtureTour()
			tc.wrong(&declared)
			problems := declared.CompositionProblems(composition())
			if len(problems) == 0 || problems[0] != tc.sentence {
				t.Fatalf("refusal = %q, want first %q", problems, tc.sentence)
			}
		})
	}
}

// The composition pass is asked of a tour whose declaration the manifest pass has
// already refused as well: it says nothing about copy or ids, because those are not
// the composition's to answer.
func TestTheCompositionPassAsksOnlyWhatTheCompositionKnows(t *testing.T) {
	declared := fixtureTour()
	declared.Title = tour.Message{Key: "not namespaced"}
	if problems := declared.CompositionProblems(composition()); len(problems) != 0 {
		t.Fatalf("the composition pass answered a copy fault: %v", problems)
	}
}
