package resource_test

import (
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
	"golang.org/x/net/html"
)

// guidedStepScreen renders the step a person walked to but is not recording: the
// session declares a completion command, this caller may use it, and the set under
// them is not the next one, so the screen names the next step and says that walking
// records nothing. Labels are the words the resolved policy carries.
func guidedStepScreen(t *testing.T) string {
	t.Helper()
	step := func(name string, sets ...uuid.UUID) entity.GuidedStep {
		return entity.GuidedStep{ID: uuid.New(), Title: name, Text: "Follow the instruction.", Sets: sets}
	}
	first, second := uuid.New(), uuid.New()
	tail := uuid.New()
	id := uuid.New()
	session := entity.GuidedSession{
		ID:    id,
		Title: "Practice sequence",
		Steps: []entity.GuidedStep{step("Prepare", first), step("Repeat", second, tail)},
		Progress: &entity.GuidedProgress{Revision: 3, Completed: []entity.GuidedCompletion{
			{SessionID: id, StepID: first, SetID: first, Revision: 2},
		}},
	}
	view := resource.Guided(resource.GuidedPage{
		Session: session,
		Policy: resource.GuidedPolicy{
			Complete: resource.GuidedCapability{Declared: true, Callable: true},
			Finish:   resource.GuidedCapability{Declared: true, Callable: true},
			Labels: map[string]string{
				"step": "Step {current} of {total}", "set": "Set {current} of {total}",
				"completed_step": "Step {current} of {total}, completed",
				"exit":           "Exit session", "finish": "Finish session", "previous_step": "Previous step",
				"next_step": "Next step", "next_set": "Next set", "reload": "Reload",
				"navigation_only":        "Next does not record progress.",
				"completion_unavailable": "Set completion is unavailable.",
				"timers":                 "Timers", "no_script": "Live countdown and sound need JavaScript.",
				"keyboard": "Arrow keys change steps. Space starts a timer or skips rest.",
			},
		},
		Base: "/app/practice/sessions/" + session.ID.String(),
		Exit: "/app",
		Mode: "step",
		Step: 1,
		Set:  1,
	}, resource.Options{})
	var out strings.Builder
	for _, node := range view.Body {
		if err := node.Render(&out); err != nil {
			t.Fatal(err)
		}
	}
	return out.String()
}

// TestAGuidedStepScreenWritesNoBodyCopyBesideItsControls holds the session screen
// to the design floor's first rule — one content column and at most one rail —
// which it measures as the number of distinct left edges body text starts at. A
// paragraph placed beside the control it explains starts at a second, indented
// edge, because the row of controls lays its children out in a line. So the copy a
// screen writes about what its primary control does belongs in the body column, and
// the row of controls holds controls.
func TestAGuidedStepScreenWritesNoBodyCopyBesideItsControls(t *testing.T) {
	page := guidedStepScreen(t)
	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatal(err)
	}
	text := func(n *html.Node) string {
		var out strings.Builder
		for d := range n.Descendants() {
			if d.Type == html.TextNode {
				out.WriteString(d.Data)
			}
		}
		return strings.TrimSpace(strings.Join(strings.Fields(out.String()), " "))
	}
	if !strings.Contains(page, "Next does not record progress.") {
		t.Fatal("the step the person is reading says nothing about what its control does: " + page)
	}
	var row *html.Node
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode || (n.Data != "a" && n.Data != "button") {
			continue
		}
		if text(n) == "Next step" {
			row = n.Parent
		}
	}
	if row == nil {
		t.Fatal("the step the person is reading offers no Next step to walk on with: " + page)
	}
	for n := range row.Descendants() {
		if n.Type != html.ElementNode {
			continue
		}
		if n.Data == "p" || strings.HasPrefix(n.Data, "h") && len(n.Data) == 2 && n.Data[1] >= '1' && n.Data[1] <= '3' {
			t.Errorf("the row of the session screen's controls carries %q beside the control it explains, which starts a second left edge down the page: %s",
				strings.Join(strings.Fields(text(n)), " "), text(n))
		}
	}
}
