package screens_test

// A guided page speaks in three places at once. The list of running timers is a region
// a person can jump to, so it needs a name; the notice the countdown reports when a
// timer or a rest reaches zero is written into one element after the page was served,
// so that element has to be a live region or the report arrives silently; and the
// countdown itself counts on the page, which is why it keeps its own announcements off
// — a timer that reads out every second of ten minutes is unreadable. None of this is
// copy, so no gate that reads the page's words or its placeholder slots sees it: the
// three elements are named by the attributes the browser and the screen reader ask for.
// This case renders the pages a session walks a person through and requires each region
// to carry the name, the role or the silence that makes it work.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
	core "github.com/septagon-oss/platformkit/ui/resource"
	"github.com/septagon-oss/platformkit/ui/screens"
	"golang.org/x/net/html"
)

// reportedRegion is one of the elements a guided page reports through, named by the
// attribute the countdown script finds it by.
var reportedRegion = map[string]bool{
	"data-guided-timers": true, "data-guided-status": true, "data-guided-seconds": true,
}

type guidedRegion struct {
	name  string            // the attribute that names the region
	attrs map[string]string // every attribute that element carries
}

func (r guidedRegion) get(key string) string { return r.attrs[key] }

// regionWords are the words of a mount that declares a guided read and both its
// commands — the map the request adapter writes into the page, so a name this case asks
// for is a name the mount actually answers rather than one kept beside it.
func regionWords(t *testing.T) map[string]string {
	t.Helper()
	r := resource()
	r.Guided = func(context.Context, uuid.UUID) (entity.GuidedSession, error) { return entity.GuidedSession{}, nil }
	r.GuidedCommands = [2]bool{true, true}
	r.Present = entity.EntryHints{Session: true}
	resolved := screens.Describe1(r, true).Resolved
	if resolved == nil || resolved.Session == nil || len(resolved.Session.Labels) == 0 {
		t.Fatal("a session read answers no words, so a guided page would name its regions with nothing")
	}
	return resolved.Session.Labels
}

// onePairRecorded is a session whose person recorded the first set of the first step, so
// the pause that pair earned exists, and answers both commands, so the finish page and
// the summary a finish returns are both rendered below.
func onePairRecorded(finished bool) entity.GuidedSession {
	id := uuid.New()
	sets := [][]uuid.UUID{{uuid.New(), uuid.New()}, {uuid.New()}}
	step := func(title string, of ...uuid.UUID) entity.GuidedStep {
		return entity.GuidedStep{ID: uuid.New(), Title: title, Text: "Follow the instruction.",
			Items: []entity.GuidedItem{{Label: "Kettlebell", Quantity: "1"}}, Sets: of, RestBetweenSeconds: 45}
	}
	steps := []entity.GuidedStep{step("Warm up", sets[0]...), step("Stretch", sets[1]...)}
	session := entity.GuidedSession{ID: id, Title: "Practice sequence", Steps: steps,
		Progress: &entity.GuidedProgress{Revision: 3, Completed: []entity.GuidedCompletion{
			{SessionID: id, StepID: steps[0].ID, SetID: sets[0][0]}}}}
	if finished {
		session.Progress.Finished = &entity.GuidedSummary{SessionID: id, Revision: 4,
			Title: "Session ended", Text: "You recorded one set.", CompletedSets: 1, TotalSets: 3}
	}
	return session
}

// regionsOf answers the reporting elements of one rendered page, as the browser and the
// assistive technology are handed them.
func regionsOf(t *testing.T, s entity.GuidedSession, words map[string]string, mode string, step, set int) []guidedRegion {
	t.Helper()
	page := core.Guided(core.GuidedPage{Session: s, Policy: core.GuidedPolicy{
		Complete: core.GuidedCapability{Declared: true, Callable: true},
		Finish:   core.GuidedCapability{Declared: true, Callable: true}, Labels: words},
		Base: "/app/note/notes/" + s.ID.String(), Exit: "/app", Mode: mode, Step: step, Set: set, Extra: 15},
		core.Options{})
	var out strings.Builder
	for _, node := range page.Body {
		if err := node.Render(&out); err != nil {
			t.Fatalf("the %s page cannot be rendered: %v", mode, err)
		}
	}
	doc, err := html.Parse(strings.NewReader(out.String()))
	if err != nil {
		t.Fatalf("the %s page is no document: %v", mode, err)
	}
	var found []guidedRegion
	for d := range doc.Descendants() {
		if d.Type != html.ElementNode {
			continue
		}
		attrs := map[string]string{}
		named := ""
		for _, attr := range d.Attr {
			key := strings.ToLower(attr.Key)
			attrs[key] = attr.Val
			if reportedRegion[key] {
				named = key
			}
		}
		if named != "" {
			found = append(found, guidedRegion{name: named, attrs: attrs})
		}
	}
	return found
}

// only answers the one element named `want`, and refuses a page that carries none or
// carries it twice: a second live region would speak the same report twice.
func only(t *testing.T, mode, want string, regions []guidedRegion) guidedRegion {
	t.Helper()
	var hits []guidedRegion
	for _, r := range regions {
		if r.name == want {
			hits = append(hits, r)
		}
	}
	if len(hits) != 1 {
		t.Fatalf("the %s page carries %d %s elements, want exactly one", mode, len(hits), want)
	}
	return hits[0]
}

// TestAGuidedPageNamesTheRegionsItsCountdownReportsIn walks the pages of a session and
// requires the timers list to be named with the mount's own word, the notice the timer
// reports at zero to sit in a live region, and the countdown itself to keep silent.
func TestAGuidedPageNamesTheRegionsItsCountdownReportsIn(t *testing.T) {
	words := regionWords(t)
	timers := words["timers"]
	if string(strings.TrimSpace(timers)) == "" {
		t.Fatal("the mount answers no word for the timers list, so no guided page can name it")
	}
	running, ended := onePairRecorded(false), onePairRecorded(true)
	for _, page := range []struct {
		mode      string
		step, set int
		session   entity.GuidedSession
	}{
		{"step", 0, 0, running}, {"rest", 0, 0, running}, {"finish", 0, 0, running}, {"summary", 0, 0, ended},
	} {
		regions := regionsOf(t, page.session, words, page.mode, page.step, page.set)
		if named := only(t, page.mode, "data-guided-timers", regions); named.get("aria-label") != timers {
			t.Errorf("the %s page's timers list is named %q, want the mount's own word %q, so a person searching the page by landmark finds nothing",
				page.mode, named.get("aria-label"), timers)
		}
		if said := only(t, page.mode, "data-guided-status", regions); said.get("role") != "status" {
			t.Errorf("the %s page writes its countdown's report into an element whose role is %q, want \"status\", so the report at zero changes words on the page and is never announced",
				page.mode, said.get("role"))
		}
	}
	regions := regionsOf(t, running, words, "rest", 0, 0)
	if tick := only(t, "rest", "data-guided-seconds", regions); tick.get("aria-live") != "off" {
		t.Errorf("the pause countdown carries aria-live %q, want \"off\": a person hearing every second of a pause counted out cannot read it",
			tick.get("aria-live"))
	}
}
