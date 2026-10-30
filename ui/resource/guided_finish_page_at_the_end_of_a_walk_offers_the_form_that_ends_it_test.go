package resource_test

// The end of a guided walk is two moments, not one: recording the last set, and finishing,
// which is what actually ends it. So a walk whose every set is recorded has nothing left to
// record and everything left to end, and the screen its own address sends that person to owes
// them the form that ends the walk, in words that count no set as still owed. Guided screens
// are rendered here as steps and as pauses; the finish page has been asked only for the address
// it names the countdown script, never for what it offers the person standing at the end of the
// walk, and every rendering that reaches a finish page reaches it with sets still owed — where
// the screen is right to count them. This one asks the page a full walk gets.

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
	"golang.org/x/net/html"
)

// finishWords is the guided vocabulary a finish page reads, with its two endings kept apart:
// the word for ending a walk that is over, and the word for ending one with sets still owed,
// which counts them.
func finishWords() map[string]string {
	return map[string]string{
		"end": "End of session", "completed": "Completed {completed} of {total} sets",
		"finish": "Finish session", "finish_partial": "Finish with {completed} of {total} sets",
		"finish_unavailable": "Finish is unavailable.", "nothing_recorded": "Nothing recorded",
		"continue": "Continue", "exit": "Exit session", "timers": "Timers",
		"no_script": "Live countdown and sound need JavaScript.",
		"keyboard":  "Arrow keys change steps. Space starts a timer or skips rest.",
	}
}

// guidedFinishScreen renders the finish page of one walk, in the bytes its address answers
// with, as the session declares it and as this caller's grants resolve it.
func guidedFinishScreen(t *testing.T, session entity.GuidedSession) string {
	t.Helper()
	view := resource.Guided(resource.GuidedPage{
		Session: session,
		Policy: resource.GuidedPolicy{
			Complete: resource.GuidedCapability{Declared: true, Callable: true},
			Finish:   resource.GuidedCapability{Declared: true, Callable: true},
			Labels:   finishWords()},
		Base: "/app/practice/sessions/" + session.ID.String(),
		Exit: "/app",
		Mode: "finish",
	}, resource.Options{})
	var out strings.Builder
	for _, node := range view.Body {
		if err := node.Render(&out); err != nil {
			t.Fatalf("the finish page cannot be rendered: %v", err)
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

// spokenText is what a reader hears from one element: its text, whitespace collapsed.
func spokenText(t *testing.T, n *html.Node) string {
	t.Helper()
	var out strings.Builder
	for d := range n.Descendants() {
		if d.Type == html.TextNode {
			out.WriteString(d.Data)
		}
	}
	return strings.Join(strings.Fields(out.String()), " ")
}

// attr is one attribute of an element, or nothing where it carries none.
func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// TestTheFinishPageOfAWalkWhoseEverySetIsRecordedOffersTheFormThatEndsIt renders the finish page
// of a walk whose three sets are all recorded and asks three things of it: that it count the
// walk as the person left it, three of three; that it offer a form posted to that walk's own
// finish address, which is the way out for a person whose browser runs no script; and that the
// control of that form say the word a session gives for ending, rather than the word that counts
// sets still owed. A finish page that offers no form holds a person who recorded everything on a
// screen they cannot act on, and one that says "Finish with 3 of 3 sets" tells them they owe a
// set they recorded.
func TestTheFinishPageOfAWalkWhoseEverySetIsRecordedOffersTheFormThatEndsIt(t *testing.T) {
	id := uuid.New()
	step := func(name string, sets ...uuid.UUID) entity.GuidedStep {
		return entity.GuidedStep{ID: uuid.New(), Title: name, Text: "Follow the instruction.", Sets: sets}
	}
	prepare, again, tail := uuid.New(), uuid.New(), uuid.New()
	steps := []entity.GuidedStep{step("Prepare", prepare, again), step("Stretch", tail)}
	completed := make([]entity.GuidedCompletion, 0, 3)
	for i, pair := range [][2]uuid.UUID{{steps[0].ID, prepare}, {steps[0].ID, again}, {steps[1].ID, tail}} {
		completed = append(completed, entity.GuidedCompletion{
			SessionID: id, StepID: pair[0], SetID: pair[1], Revision: int64(i + 2),
			CompletedAt: time.Unix(1777000000, 0).UTC()})
	}
	session := entity.GuidedSession{ID: id, Title: "Practice sequence", Steps: steps,
		Progress: &entity.GuidedProgress{Revision: 4, Completed: completed}}
	if _, _, ok := session.Pair(len(completed)); ok {
		t.Fatal("this walk still names a pair to record, so the page asked below is no longer the end of it")
	}

	page := guidedFinishScreen(t, session)
	if !strings.Contains(page, "Completed 3 of 3 sets") {
		t.Errorf("the finish page of a walk with every set recorded does not count it as three of three, so the person "+
			"cannot see the walk as they left it: %s", page)
	}

	doc, err := html.Parse(strings.NewReader(page))
	if err != nil {
		t.Fatalf("the finish page is no document this case can read: %v", err)
	}
	action := "/app/practice/sessions/" + id.String() + "/finish"
	var form *html.Node
	for n := range doc.Descendants() {
		if n.Type != html.ElementNode || n.Data != "form" {
			continue
		}
		if attr(n, "action") == action {
			form = n
		}
	}
	if form == nil {
		t.Fatalf("the finish page of a walk whose every set is recorded offers no form posting to %s, so a person sent "+
			"here from the walk's own address has nothing to press: %s", action, page)
	}
	said := ""
	for n := range form.Descendants() {
		if n.Type == html.ElementNode && (n.Data == "button" || (n.Data == "input" && attr(n, "type") == "submit")) {
			said = spokenText(t, n)
		}
	}
	if said == "" {
		t.Fatalf("the form that ends this walk carries no control that says what it does, so the person cannot tell it "+
			"from one that throws the walk away: %s", page)
	}
	if said != finishWords()["finish"] {
		t.Errorf("the control that ends a walk whose every set is recorded says %q rather than %q: with nothing left to "+
			"record it counts no set as still owed, and a label that counts one tells the person they owe a set they "+
			"recorded", said, finishWords()["finish"])
	}
}
