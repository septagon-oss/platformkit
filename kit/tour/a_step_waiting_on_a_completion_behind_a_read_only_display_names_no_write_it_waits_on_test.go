package tour_test

// A display names one read-only element's semantic source, and the screen rule
// validates that target as a name alone — never against the operations a
// composition mounted. So a display's key can spell the ID of a mounted write
// exactly, and the step behind such an anchor still has to be refused for what its
// control is: a read-only display starts no write whose outcome a person could
// wait on. Read the refusal as the reason, not as a key that happened not to match.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/kit/tour"
)

func TestAStepWaitingOnACompletionBehindAReadOnlyDisplayNamesNoWriteItWaitsOn(t *testing.T) {
	editor := screen.MustView(screen.ViewSpec{Ref: editorView, Cardinality: screen.One, Anchors: []screen.AnchorSpec{
		{Name: "field.total", Control: "display", Binding: screen.Display(writeOp)},
		{Name: "field.count", Control: "display", Binding: screen.Display("note.total")},
	}})
	gate := publishedScreens{views: map[screen.ViewRef]screen.View{editorView: editor},
		ops: map[string]screen.Operation{writeOp: {ID: writeOp, Surface: "app", Method: "POST", Path: "/note", Guard: "feature:write"}}}
	for _, anchor := range []string{"field.total", "field.count"} {
		want := `tour "feature.first", step "total": ` + editorView + `#` + anchor +
			` is a read-only display and starts no write; a step that waits for a completion has to point at the control that makes it`
		if problems := oneStepTour("total", screen.AnchorRef(editorView+"#"+anchor), tour.Completes(writeOp)).CompositionProblems(gate); len(problems) != 1 || problems[0] != want {
			t.Errorf("a step behind the display %s = %q, want %q", anchor, problems, want)
		}
	}
}
