package app

// The composition gate asks a tour of the screen contract the kernel froze, not of
// a list a test wrote: what the snapshot holds is what a published view or a
// published control reaches, so a mounted route no screen points at is a fact a
// step cannot wait on. That is the one thing a double could not be trusted to
// state, so it is asked of Publish and of the gate that reads its result.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

const (
	tourEditorView = "app/feature/custom/editor"
	tourDoneView   = "app/feature/custom/done"
	tourWriteOp    = "feature-note-create"
	tourHiddenOp   = "feature-note-hide"
)

func tourScreens(t *testing.T) screen.Inventory {
	t.Helper()
	editor := screen.MustView(screen.ViewSpec{Ref: tourEditorView, Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "action.submit", Control: "button", Binding: screen.Submission(tourWriteOp), Interactions: []screen.Interaction{screen.Submitted}},
	}})
	done := screen.MustView(screen.ViewSpec{Ref: tourDoneView, Cardinality: screen.One})
	mounts := []screen.Mount{
		{Operation: screen.Operation{ID: "feature-editor", Surface: "app", Method: "GET", Path: "/editor", Guard: "feature:read"},
			Owner: "feature", Document: true, Responses: []screen.Response{{View: tourEditorView, Kind: "input"}}},
		{Operation: screen.Operation{ID: tourWriteOp, Surface: "app", Method: "POST", Path: "/note", Guard: "feature:write"},
			Owner: "feature", Document: true, Responses: []screen.Response{{View: tourDoneView, Kind: "redirect"}}},
		{Operation: screen.Operation{ID: "feature-done", Surface: "app", Method: "GET", Path: "/done", Guard: "feature:read"},
			Owner: "feature", Document: true, Responses: []screen.Response{{View: tourDoneView, Kind: "input"}}},
		// The route exists and is guarded, and no view or control of this
		// composition leads a person to it: nothing reaches it, so the frozen
		// contract carries no such operation.
		{Operation: screen.Operation{ID: tourHiddenOp, Surface: "app", Method: "POST", Path: "/hide", Guard: "feature:write"},
			Owner: "feature", Document: true, Exception: "redirect", Reason: "a command's own answer, never a screen a tour walks a person to"},
	}
	inventory, err := screen.Publish("collect", 1, []screen.View{editor, done}, mounts)
	if err != nil {
		t.Fatalf("publish the screen contract: %v", err)
	}
	return inventory
}

func completingTour(operation tour.OperationRef) module.Module {
	return module.Module{
		Name: "feature",
		Tours: []tour.Tour{{
			Ref:   tour.Ref{ID: "feature.first", Edition: 1},
			Title: tour.Message{Key: "feature.first", Fallback: "Add your first note"},
			Steps: []tour.Step{tour.Spotlight("save", tourEditorView+"#action.submit",
				tour.Content{
					Title: tour.Message{Key: "feature.first.step.save", Fallback: "Save it"},
					Body:  tour.Message{Key: "feature.first.step.save.body", Fallback: "Saving writes the note."}},
				tour.Completes(operation), tour.AnchoredOptions{})},
		}},
	}
}

func gate(t *testing.T, mod module.Module) error {
	t.Helper()
	a := &App{mods: []module.Module{mod}, screenInventory: tourScreens(t)}
	return a.validateTours()
}

func TestACompletionOnAnOperationNoScreenReachesIsRefused(t *testing.T) {
	if err := gate(t, completingTour(tourWriteOp)); err != nil {
		t.Fatalf("the composition refused a step completing on the write its own button starts: %v", err)
	}
	err := gate(t, completingTour(tourHiddenOp))
	if err == nil {
		t.Fatal("a step waits on a route no screen of this composition reaches")
	}
	want := `tour "feature.first", step "save": operation "feature-note-hide" is not an operation of this composition`
	if got := err.Error(); !strings.Contains(got, want) {
		t.Fatalf("refusal = %q, want it to hold %q", got, want)
	}
}
