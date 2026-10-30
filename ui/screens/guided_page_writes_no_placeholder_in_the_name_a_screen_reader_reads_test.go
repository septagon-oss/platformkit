package screens_test

// A guided page says some of its copy in an attribute rather than between tags: the
// timers list is named by an aria-label, a picture by its alt, and a control by its
// title. Those words come from the same map as the text beside them and are filled by
// the same call, so the case that reads the page aloud — the one beside this — sees
// none of them: it walks the text a person reads with their eyes. A line raised into
// an attribute with a slot its call never fills is invisible to that case and reaches
// a person through the screen reader instead: a timers list named "Timers {count}".
// The word templates the countdown script fills in the browser live in the config the
// page writes to its own data attribute, and stay there on purpose; every other
// attribute is copy a person is handed as it stands.

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

// namedByAttribute is the set of attributes that carry copy a person is told rather
// than shown: a name, a description, an alternative, a hover hint, or the grey word
// inside an empty field. `data-guided` is deliberately absent — it holds the word
// templates the script replaces at runtime, placeholders and all.
var namedByAttribute = map[string]bool{
	"aria-label": true, "aria-valuetext": true, "aria-roledescription": true,
	"title": true, "alt": true, "placeholder": true,
}

// slotName is a placeholder left whole. It is the shape the renderers replace with a
// number, a step title or a duration.
var slotName = regexp.MustCompile(`\{[^{}]*\}`)

// spokenNames answers every person-facing attribute of one rendered page, as
// `labelled` wrote them. A page that carries none is a failure of this case, not of
// the page, so the count is returned and asked for.
func spokenNames(t *testing.T, s entity.GuidedSession, words map[string]string, mode string, step, set int) []string {
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
	var named []string
	for d := range doc.Descendants() {
		if d.Type != html.ElementNode {
			continue
		}
		for _, attr := range d.Attr {
			if namedByAttribute[strings.ToLower(attr.Key)] {
				named = append(named, attr.Key+"="+attr.Val)
			}
		}
	}
	if len(named) == 0 {
		t.Errorf("the %s page names nothing in an attribute, so this case read a page it was not shown", mode)
	}
	return named
}

// readFromTheMount are the words a mount that declares a guided read and both its
// commands answers — the map the page is written with, not a list kept beside it.
func readFromTheMount(t *testing.T) map[string]string {
	t.Helper()
	r := resource()
	r.Guided = func(context.Context, uuid.UUID) (entity.GuidedSession, error) { return entity.GuidedSession{}, nil }
	r.GuidedCommands = [2]bool{true, true}
	r.Present = entity.EntryHints{Session: true}
	resolved := screens.Describe1(r, true).Resolved
	if resolved == nil || resolved.Session == nil || len(resolved.Session.Labels) == 0 {
		t.Fatal("a session read answers no words, so a guided page would name its parts with nothing")
	}
	return resolved.Session.Labels
}

// practiceSession is a session with one set recorded on the first step, a named timer
// and a target on the second, and both rests, so each page a person is walked through
// exists below: the pair being recorded, the step ahead, the pause, the finish, and the
// summary a finished session returns.
func practiceSession(finished bool) entity.GuidedSession {
	id := uuid.New()
	sets := [][]uuid.UUID{{uuid.New()}, {uuid.New(), uuid.New()}, {uuid.New()}}
	step := func(title string, of ...uuid.UUID) entity.GuidedStep {
		return entity.GuidedStep{ID: uuid.New(), Title: title, Text: "Follow the instruction.",
			Items: []entity.GuidedItem{{Label: "Kettlebell", Quantity: "1", Picture: "kettlebell"}}, Sets: of,
			RestBetweenSeconds: 45, RestAfterSeconds: 60}
	}
	steps := []entity.GuidedStep{step("Warm up", sets[0]...), step("Work", sets[1]...), step("Stretch", sets[2]...)}
	steps[1].Timer = &entity.GuidedTimer{Name: "Simmer", Seconds: 600}
	steps[1].Target = &entity.GuidedTarget{Kind: "repetitions", Value: 12}
	session := entity.GuidedSession{ID: id, Title: "Practice sequence", Steps: steps,
		Progress: &entity.GuidedProgress{Revision: 4, Completed: []entity.GuidedCompletion{
			{SessionID: id, StepID: steps[0].ID, SetID: sets[0][0]}}}}
	if finished {
		session.Progress.Finished = &entity.GuidedSummary{SessionID: id, Revision: 5, Title: "Session ended",
			Text: "You recorded one set.", CompletedSets: 1, TotalSets: 4}
	}
	return session
}

// TestAGuidedPageWritesNoPlaceholderInTheNameAScreenReaderReads walks the five pages of
// a session and requires every attribute that names a part of them — the timers list, a
// picture, a control's hint — to carry its numbers rather than the name of the slot they
// fill. It reads no text: what it guards is the half of the copy a person hears.
func TestAGuidedPageWritesNoPlaceholderInTheNameAScreenReaderReads(t *testing.T) {
	words := readFromTheMount(t)
	running, ended := practiceSession(false), practiceSession(true)
	for _, page := range []struct {
		mode      string
		step, set int
		session   entity.GuidedSession
	}{
		{"step", 0, 0, running},
		{"step", 1, 0, running},
		{"rest", 0, 0, running},
		{"finish", 0, 0, running},
		{"summary", 0, 0, ended},
	} {
		named := spokenNames(t, page.session, words, page.mode, page.step, page.set)
		for _, one := range named {
			key, value, _ := strings.Cut(one, "=")
			if left := slotName.FindAllString(value, -1); len(left) > 0 {
				t.Errorf("the %s page names %s %q with its placeholder whole, which a screen reader says instead of a number", page.mode, key, value)
			}
		}
	}
}
