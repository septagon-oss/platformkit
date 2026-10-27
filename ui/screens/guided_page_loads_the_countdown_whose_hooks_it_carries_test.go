package screens_test

// The countdown reaches a guided page by name at both ends: the page names one script
// module and the shell serves it from the same tree this package's cases read, and the
// script installs on the element the page marks. The case beside this one joins the
// script's hook names to the markup of the two pages a person counts on, reading the
// script out of ui/assets/js. Two links of that chain stay open. Nothing pins that a
// guided page names its countdown at all — ui/document pins the module tag for the rich
// text editor alone — so a page that stopped naming it would keep every element the
// script queries, load none of it, and leave a person with a pause that never counts and
// a timer that never sounds, while every case that reads the render stays green. And
// nothing renders the two pages a session ends on against the script's vocabulary, so a
// hook those pages write that the script queries nowhere would sit wired to nothing. This
// case walks all four modes: each names the countdown once, the name the shell serves is
// the bytes the vocabulary cases read, and the two pages no other case sets beside the
// script carry no hook that script reaches no more.

import (
	"bytes"
	"io/fs"
	"os"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	core "github.com/septagon-oss/platformkit/ui/resource"
)

// guidedCountdown is the module a guided page names and the file ui/assets/js holds it
// at, so one name joins what the page loads, what the shell serves and what the cases
// beside this one read.
const guidedCountdown = "js/guided.js"

// TestTheGuidedPagesLoadTheCountdownTheyRunAndWriteNoHookItNeverReaches walks the step,
// the pause and the two pages a session ends on, and asks each end of the join to name
// the same script: the page names it, the shell serves those bytes, and the finish and
// summary markup holds nothing the loaded script will not query.
func TestTheGuidedPagesLoadTheCountdownTheyRunAndWriteNoHookItNeverReaches(t *testing.T) {
	words, session := mountWords(t), oneSetRecorded()
	reached := hooksTheScriptReaches(t)
	served, err := fs.ReadFile(ui.Assets(ui.Compose(design.Default())), guidedCountdown)
	if err != nil {
		t.Fatalf("the shell serves no countdown at %s, so no guided page can run one: %v", guidedCountdown, err)
	}
	// The cases that join the script's words, lengths and hooks read the file on disk;
	// a person reads what the shell serves. Two names for one script are only evidence
	// while the two are the same bytes.
	compiled, err := os.ReadFile("../assets/" + guidedCountdown)
	if err != nil {
		t.Fatalf("no countdown to read where the cases read it: %v", err)
	}
	if !bytes.Equal(served, compiled) {
		t.Errorf("the shell serves %d bytes at %s and ui/assets/js holds %d, so the script a person runs is not the one these cases read",
			len(served), guidedCountdown, len(compiled))
	}
	for _, page := range []struct {
		mode       string
		step, set  int
		endsTheDay bool
	}{
		{"step", 1, 0, false},
		{"rest", 0, 0, false},
		// The two pages a person arrives at when the session is over: the script
		// installs on both, and no other case renders either.
		{"finish", 0, 0, true},
		{"summary", 0, 0, true},
	} {
		view := core.Guided(core.GuidedPage{Session: session, Policy: core.GuidedPolicy{
			Complete: core.GuidedCapability{Declared: true, Callable: true},
			Finish:   core.GuidedCapability{Declared: true, Callable: true}, Labels: words},
			Base: "/app/note/notes/" + session.ID.String(), Exit: "/app", Mode: page.mode,
			Step: page.step, Set: page.set, Extra: 15},
			core.Options{})
		if !slices.Contains(view.Modules, guidedCountdown) {
			t.Errorf("the %s page names %v and no %s, so the countdown, its notice and its keys are never loaded to bring its elements to life",
				page.mode, view.Modules, guidedCountdown)
		}
		if !page.endsTheDay {
			continue
		}
		for _, hook := range sortedHooks(hooksThePageCarries(t, session, words, page.mode, page.step, page.set)) {
			if !reached[hook] && !hooksForTheSheet[hook] {
				t.Errorf("the %s page writes %s for the countdown to reach and the script reaches it no more, so that element sits on the page and never moves",
					page.mode, hook)
			}
		}
	}
}
