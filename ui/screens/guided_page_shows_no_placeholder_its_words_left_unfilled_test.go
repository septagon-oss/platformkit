package screens_test

// A guided page fills its copy twice over: the renderer replaces the placeholders of
// the line the policy resolved with the numbers of the pair under the person, and the
// countdown script does the same in the browser for the lines it redraws. A
// placeholder no caller replaces reaches a person whole — "Completed 1 of {total}
// sets" where the count belongs. The catalogue gate asks which keys a screen raises,
// never what each call fills, so this case renders the pages a session of every shape
// produces with the words the mount itself answers, and asks each page to show its
// numbers and to keep no brace-word.

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	core "github.com/septagon-oss/platformkit/ui/resource"
	"github.com/septagon-oss/platformkit/ui/screens"
	"golang.org/x/net/html"
)

// wholeSlot is a placeholder a caller left where a person reads.
var wholeSlot = regexp.MustCompile(`\{[a-z_0-9]+\}`)

// mountWords are the words of a mount that declares a guided read and both its
// commands: the map the request adapter writes into the page, so only copy that mount
// answers can reach the page this case reads.
func mountWords(t *testing.T) map[string]string {
	t.Helper()
	r := resource()
	r.Guided = func(context.Context, uuid.UUID) (entity.GuidedSession, error) { return entity.GuidedSession{}, nil }
	r.GuidedCommands = [2]bool{true, true}
	r.Present = entity.EntryHints{Session: true}
	resolved := screens.Describe1(r, true).Resolved
	if resolved == nil || resolved.Session == nil || len(resolved.Session.Labels) == 0 {
		t.Fatal("a session read answers no words, so every guided page here would render nothing to read")
	}
	return resolved.Session.Labels
}

// oneSetRecorded is a session whose person recorded exactly one set, on the first
// step, so the step under them is the first set of the next one. Its three steps carry
// four sets, a named timer, a repetitions target, a duration target, items and both
// rests, so every line a guided page says is said by something below.
func oneSetRecorded() entity.GuidedSession {
	id := uuid.New()
	sets := [][]uuid.UUID{{uuid.New()}, {uuid.New(), uuid.New()}, {uuid.New()}}
	step := func(title string, of ...uuid.UUID) entity.GuidedStep {
		return entity.GuidedStep{ID: uuid.New(), Title: title, Text: "Follow the instruction.",
			Items: []entity.GuidedItem{{Label: "Kettlebell", Quantity: "1"}}, Sets: of,
			RestBetweenSeconds: 45, RestAfterSeconds: 60}
	}
	steps := []entity.GuidedStep{step("Warm up", sets[0]...), step("Work", sets[1]...), step("Stretch", sets[2]...)}
	steps[1].Timer = &entity.GuidedTimer{Name: "Simmer", Seconds: 600}
	steps[1].Target = &entity.GuidedTarget{Kind: "repetitions", Value: 12}
	steps[2].Target = &entity.GuidedTarget{Kind: "seconds", Value: 90}
	return entity.GuidedSession{ID: id, Title: "Practice sequence", Steps: steps,
		Progress: &entity.GuidedProgress{Revision: 4, Completed: []entity.GuidedCompletion{
			{SessionID: id, StepID: steps[0].ID, SetID: sets[0][0]}}}}
}

// readAloud renders one page of a session and answers only what a person reads: the
// text of the served document, with the config the countdown script reads left in the
// attribute it is written to. Each want names copy the page must show with its numbers
// in place, which is how this case knows it reached a filled line rather than an empty
// page; a placeholder any caller missed is what it reports.
func readAloud(t *testing.T, s entity.GuidedSession, words map[string]string, mode string, step, set int, wants ...string) {
	t.Helper()
	page := core.Guided(core.GuidedPage{Session: s, Policy: core.GuidedPolicy{
		Complete: core.GuidedCapability{Declared: true, Callable: true},
		Finish:   core.GuidedCapability{Declared: true, Callable: true}, Labels: words},
		Base: "/app/note/notes/" + s.ID.String(), Exit: "/app", Mode: mode, Step: step, Set: set, Extra: 15},
		core.Options{})
	var out strings.Builder
	for _, node := range page.Body {
		if err := node.Render(&out); err != nil {
			t.Fatal(err)
		}
	}
	doc, err := html.Parse(strings.NewReader(out.String()))
	if err != nil {
		t.Fatal(err)
	}
	var read strings.Builder
	for d := range doc.Descendants() {
		if d.Type == html.TextNode {
			read.WriteString(d.Data + " ")
		}
	}
	text := strings.Join(strings.Fields(read.String()), " ")
	shown := text
	if len(shown) > 300 {
		shown = shown[:300] + "…"
	}
	for _, want := range wants {
		if !strings.Contains(text, want) {
			t.Errorf("the %s page does not show %q, so this is not the copy the case came to read: %s", mode, want, shown)
		}
	}
	if left := wholeSlot.FindAllString(text, -1); len(left) > 0 {
		t.Errorf("the %s page shows %v with its placeholder whole, which a person reads instead of a number", mode, left)
	}
}

// TestAGuidedPageShowsNoPlaceholderItsWordsLeftUnfilled walks the five pages a session
// of every shape produces — the pair the person is recording, the step ahead of them,
// the pause between sets, the finish they may stop at, and the summary a finished
// session returns — and requires each to say its numbers rather than the names of the
// slots they fill.
func TestAGuidedPageShowsNoPlaceholderItsWordsLeftUnfilled(t *testing.T) {
	words, session := mountWords(t), oneSetRecorded()
	readAloud(t, session, words, "step", 1, 0, "Start \u201cSimmer\u201d 10:00", "Set 1 of 2", "Repetitions", "Kettlebell 1")
	readAloud(t, session, words, "step", 2, 0, "1:30", "Duration", "Next does not record progress.")
	readAloud(t, session, words, "rest", 0, 0, "1:15", "+15 s", "Up next", "Work")
	readAloud(t, session, words, "finish", 0, 0, "Completed 1 of 4 sets", "Finish with 1 of 4 sets")
	finished := oneSetRecorded()
	finished.Progress.Finished = &entity.GuidedSummary{SessionID: finished.ID, Revision: 5, Title: "Session ended",
		Text: "You recorded one set.", CompletedSets: 1, TotalSets: 4,
		Facts: []entity.GuidedFact{{Label: "Longest streak", Value: "1 set"}}}
	readAloud(t, finished, words, "summary", 0, 0, "Session ended", "Longest streak: 1 set", "Completed 1 of 4 sets")
}
