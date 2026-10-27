package screens_test

// The countdown script reaches the guided page by hook name and nothing else: ui/resource
// writes a data-guided attribute on every element the script may need, and
// ui/assets/js/guided.js addresses that element by the same name — in a selector, or
// through the element's dataset, which the browser resolves to the same attribute. The
// page's words are joined twice over: one gate joins the keys a screen raises to the
// catalogues that answer them, and the case beside this one joins the lines the script
// asks for to the words a mount answers. And ui/hooks.go attributes every hook the kernel
// renders to the file that writes it, checked against that markup. No gate joins the
// script's own hooks to the page it runs on. Rename a hook in ui/resource and update the
// vocabulary ui/hooks.go declares for it, and the script's read of [data-guided-value] —
// the one element whose text it rewrites every quarter second, reached with no `?.` —
// throws on the first tick, so a pause never counts down, never reports at zero and never
// turns its Skip into Continue; a hook the script stops reaching leaves its element on the
// page and its countdown silent. Every case that reads the render, the numbers or the
// words passes either way. So this case reads the hooks out of the script, renders the two
// pages it installs on, and asks the two vocabularies to be one.

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	core "github.com/septagon-oss/platformkit/ui/resource"
)

// hooksForTheSheet are the hooks a guided page carries for its stylesheet to address and
// the countdown script never reaches: [data-guided-target] holds the step's own number,
// which the guided sheet gives a rule of its own and the script leaves alone. A hook the
// page carries and the script does not reach is a defect unless it is named here.
var hooksForTheSheet = map[string]bool{"data-guided-target": true}

// hooksTheScriptReaches lists the hook names out of the countdown script, both spellings:
// the bracketed selector it queries and the camelCased dataset member it reads. A script
// that reaches no hook is not a script this case can compare, so it refuses rather than
// weigh two empty lists against each other.
func hooksTheScriptReaches(t *testing.T) map[string]bool {
	t.Helper()
	source, err := os.ReadFile("../assets/js/guided.js")
	if err != nil {
		t.Fatalf("no countdown script to read: %v", err)
	}
	reached := map[string]bool{}
	for _, one := range regexp.MustCompile(`\[\s*(data-guided[a-z-]*)\s*\]`).FindAllStringSubmatch(string(source), -1) {
		reached[one[1]] = true
	}
	for _, one := range regexp.MustCompile(`dataset\.guided([A-Z][A-Za-z]*)`).FindAllStringSubmatch(string(source), -1) {
		reached["data-guided-"+strings.ToLower(one[1])] = true
	}
	if len(reached) == 0 {
		t.Fatal("the countdown script reaches no data-guided hook this case can read, so it and the page are no longer one vocabulary")
	}
	return reached
}

// hooksThePageCarries renders one page of a session and lists the data-guided attributes
// it writes, each taken as the attribute name it spells by its own `=` so a line of copy
// or of JSON that merely names one does not count as markup.
func hooksThePageCarries(t *testing.T, s entity.GuidedSession, words map[string]string, mode string, step, set int) map[string]bool {
	t.Helper()
	view := core.Guided(core.GuidedPage{Session: s, Policy: core.GuidedPolicy{
		Complete: core.GuidedCapability{Declared: true, Callable: true},
		Finish:   core.GuidedCapability{Declared: true, Callable: true}, Labels: words},
		Base: "/app/note/notes/" + s.ID.String(), Exit: "/app", Mode: mode, Step: step, Set: set, Extra: 15},
		core.Options{})
	var out strings.Builder
	for _, node := range view.Body {
		if err := node.Render(&out); err != nil {
			t.Fatalf("the %s page cannot be rendered: %v", mode, err)
		}
	}
	carried := map[string]bool{}
	for _, one := range regexp.MustCompile(`(?i)\b(data-guided[a-z-]*)=`).FindAllStringSubmatch(out.String(), -1) {
		carried[strings.ToLower(one[1])] = true
	}
	if len(carried) == 0 {
		t.Fatalf("the %s page carries no data-guided hook, so the countdown script that runs on it reaches nothing: %s", mode, out.String())
	}
	return carried
}

// TestTheGuidedScriptReachesOnlyHooksTheGuidedPageCarries walks the two pages the
// countdown runs on — the step whose pair a named timer belongs to, and the pause that
// pair earned — and requires every hook the script reaches to be an attribute the page
// writes, and every hook the page writes to be one the script reaches besides the sheet's
// own. Each direction is asked by name, because a one-sided read passes on the very
// rename it exists to notice.
func TestTheGuidedScriptReachesOnlyHooksTheGuidedPageCarries(t *testing.T) {
	words, session := mountWords(t), oneSetRecorded()
	reached := hooksTheScriptReaches(t)
	carried := map[string]bool{}
	for _, page := range []struct {
		mode      string
		step, set int
		wants     []string
	}{
		// The step under a person whose pair has the named timer and a target, so the
		// script's start button, its target and its heading are all on the page.
		{"step", 1, 0, []string{"data-guided-start", "data-guided-heading"}},
		// The pause the pair the person recorded earned — the only pair whose screen may
		// show a pause — which is where the countdown, the +15 s offer and the Skip live.
		{"rest", 0, 0, []string{"data-guided-rest", "data-guided-value", "data-guided-add", "data-guided-skip"}},
	} {
		for hook := range hooksThePageCarries(t, session, words, page.mode, page.step, page.set) {
			carried[hook] = true
		}
		for _, want := range page.wants {
			if !reached[want] {
				t.Errorf("the countdown script reaches no %s, though the %s page writes it and only the script can bring that element to life", want, page.mode)
			}
		}
	}
	for _, hook := range sortedHooks(reached) {
		if !carried[hook] {
			t.Errorf("the countdown script reaches %s and no guided page writes it, so the read finds no element: the pause's countdown, its notice or its list is never reached", hook)
		}
	}
	for _, hook := range sortedHooks(carried) {
		if !reached[hook] && !hooksForTheSheet[hook] {
			t.Errorf("the guided page writes %s for the countdown to reach and the script reaches it no more, so that element sits on the page and never moves", hook)
		}
	}
}

// sortedHooks lists a hook set in one order, so a refusal names every hook at stake.
func sortedHooks(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for hook := range set {
		out = append(out, hook)
	}
	sort.Strings(out)
	return out
}
