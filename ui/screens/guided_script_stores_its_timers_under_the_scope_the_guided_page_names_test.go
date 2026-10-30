package screens_test

// A guided page names, in its data-guided config, the scope a countdown is stored under, and
// ui/screens writes that scope from the tenant, the caller it answered and the resource. The
// case beside this one pins that half. Nothing pinned the other half: the countdown script
// decides on its own what to put a timer into sessionStorage under, and a page that names the
// caller perfectly is of no use to anyone if the script keys its store by the session alone.
// Two people of one tenant working one sequence in one browser tab would then leave the first
// one's running simmer where the second one's page reads it, and every case here would pass,
// because the markup, the numbers and the words are all correct either way.
// This case follows the script's own bindings — a declaration, or the argument its one call
// site passes for a parameter — from each sessionStorage key it composes, and asks that the
// trail arrive at the scope the page wrote.

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// countdownScript reads the script a guided page installs, from where the page serves it.
func countdownScript(t *testing.T) string {
	t.Helper()
	source, err := os.ReadFile("../assets/js/guided.js")
	if err != nil {
		t.Fatalf("no countdown script to read, so nothing joins its storage keys to the page that hands it a scope: %v", err)
	}
	return string(source)
}

// scriptKeyPrefix reads, out of the script, the identifier every sessionStorage key for a
// stored timer is concatenated onto: the calls that read and write a timer hold the address
// as `prefix + key`. A script this case reads no prefix in is one it cannot follow, so it
// says so rather than pass on two empty lists.
func scriptKeyPrefix(t *testing.T, source string) []string {
	t.Helper()
	var names []string
	for _, one := range regexp.MustCompile(`sessionStorage\s*\.\s*(?:setItem|getItem)\s*\(\s*([A-Za-z_$][\w$]*)\s*\+`).FindAllStringSubmatch(source, -1) {
		if !strings.Contains(strings.Join(names, " "), one[1]) {
			names = append(names, one[1])
		}
	}
	if len(names) == 0 {
		t.Fatal("the countdown script composes no sessionStorage key from a name this case can follow, so a timer's address and the page's scope are no longer joined by anything readable")
	}
	return names
}

// scriptValues maps each name the script binds to what it binds it to: the right-hand side of
// its const, let or var, and — because the store lives in a function the page's prefix is
// passed into — the argument the script's own call site passes for a parameter. A name with no
// binding here is one the trail stops at, which the caller reports as a refusal.
func scriptValues(source string) map[string][]string {
	values := map[string][]string{}
	for _, one := range regexp.MustCompile(`(?:const|let|var)\s+([A-Za-z_$][\w$]*)\s*=\s*([^;\n]*)`).FindAllStringSubmatch(source, -1) {
		values[one[1]] = append(values[one[1]], strings.TrimSpace(one[2]))
	}
	for _, decl := range regexp.MustCompile(`function\s+([A-Za-z_$][\w$]*)\s*\(([^)]*)\)\s*\{`).FindAllStringSubmatch(source, -1) {
		calls := regexp.MustCompile(`\b`+regexp.QuoteMeta(decl[1])+`\s*\(([^()]*)\)`).FindAllStringSubmatch(source, -1)
		for _, call := range calls {
			for i, param := range strings.Split(decl[2], ",") {
				args := strings.Split(call[1], ",")
				if i >= len(args) {
					continue
				}
				name := strings.TrimSpace(strings.Split(param, "=")[0])
				if name == "" {
					continue
				}
				values[name] = append(values[name], strings.TrimSpace(args[i]))
			}
		}
	}
	return values
}

// TestTheGuidedScriptStoresItsTimersUnderTheScopeTheGuidedPageNames follows the script's own
// bindings from each sessionStorage key it reads or writes to the scope field the page writes,
// and asks that the cross-scope sweep it makes on load — the entries it discards because they
// are not its own — is told apart by that same field. A stored timer whose address leaves the
// scope out is read by whoever opens the same sequence next.
func TestTheGuidedScriptStoresItsTimersUnderTheScopeTheGuidedPageNames(t *testing.T) {
	source := countdownScript(t)
	if !strings.Contains(source, "cfg.scope") {
		t.Fatal("the countdown script reads no scope out of the config the guided page writes, so a stored timer answers to no person at all")
	}
	values := scriptValues(source)
	for _, prefix := range scriptKeyPrefix(t, source) {
		if storesUnderTheScope(values, prefix) {
			continue
		}
		t.Errorf("the countdown script stores and reads its timers at keys under %q, which leads nowhere near the scope the page writes: two people of one tenant share one countdown, the timer one leaves running being the other's",
			prefix)
	}
	// What the script discards on load because an entry is not its own: a stored timer of some
	// other page. The sweep that empties the whole store on a finished session is the script
	// tidying up after itself and is not read here.
	swept := 0
	for _, one := range regexp.MustCompile(`!\s*[A-Za-z_$][\w$]*\.startsWith\s*\(([^)]*)\)`).FindAllStringSubmatch(source, -1) {
		swept++
		toldApart := strings.Contains(one[1], "cfg.scope")
		for _, name := range regexp.MustCompile(`[A-Za-z_$][\w$]*`).FindAllString(one[1], -1) {
			toldApart = toldApart || storesUnderTheScope(values, name)
		}
		if !toldApart {
			t.Errorf("the countdown script discards stored timers that fail %q, which tells one person's entries from another's by nothing the page wrote: the entries it drops are not chosen by the scope the page names", one[1])
		}
	}
	if swept == 0 {
		t.Error("the countdown script no longer discards the stored timers of any other scope, so a person reads back a countdown a previous person of this browser left beside their own")
	}
}

// storesUnderTheScope answers whether a prefix name reaches, in a few binding steps, the
// config field the page writes its scope into. Each step follows one binding the script
// itself wrote, so a rename of the script's own names keeps the trail.
func storesUnderTheScope(values map[string][]string, prefix string) bool {
	queue, seen := []string{prefix}, map[string]bool{}
	for depth := 0; depth < 6 && len(queue) > 0; depth++ {
		var next []string
		for _, name := range queue {
			if seen[name] {
				continue
			}
			seen[name] = true
			for _, bound := range values[name] {
				if strings.Contains(bound, "cfg.scope") {
					return true
				}
				next = append(next, regexp.MustCompile(`[A-Za-z_$][\w$]*`).FindAllString(bound, -1)...)
			}
		}
		queue = next
	}
	return false
}
