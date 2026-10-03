package ui_test

// Review round 13 of T-0108. Rounds 11 and 12 were the same question asked at two
// boundaries: the gate refuses a consumer rule that names the kernel's own
// vocabulary, and each round found a name the vocabulary omitted — a class reached
// as the value of the class attribute (round 12), a class carried into
// @layer components by a module's Extra.Lists (round 12's deferred MEDIUM). Both
// were found by reading a declaration list that was narrower than the sheet.
//
// The delivery's own completeness pins still read lists: ui/hooks_test.go's
// TestEveryClassTheKernelEmitsIsInRefusedVocabulary walks rules(ClassLists()),
// componentState() and base() — the sheets Compose merges — and
// TestAttrMatchesReadsWhetherASelectorComparesAValue walks hand-written
// selectors. A class can enter @layer components by a road no declaration list
// names (that is what round 12's MEDIUM was), and a refusal can fail for a
// spelling no hand-written case names (that is what round 11's and round 12's
// HIGHs were). So this file reads the other artifact: the emitted sheet itself.
// For every class and every attribute the served sheet's own rules address, the
// gate must refuse a consumer rule that addresses the same thing — in the `.name`
// spelling, in the [class~="name"] spelling, and in the spellings of a name CSS
// allows an author who means the same thing.
//
// Nothing below decides anything from the absence of a refusal: the premise is the
// served sheet, which Compose emits today, and the assertion is the refusal the
// brief demands, reached through the refusal's own words.

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/ui"
	"github.com/septagon-oss/platformkit/ui/css"
)

// reviewRound13Served is the sheet every page of a composed application loads.
func reviewRound13Served() string {
	return string(ui.Compose(design.Default()).Body)
}

// reviewRound13Block returns the text of one @layer block of a sheet. A consumer
// cannot add one (UsesLayers refuses it), so what a block holds is what the
// browser cascades from that layer.
func reviewRound13Block(t *testing.T, sheet, layer string) string {
	t.Helper()
	open := "@layer " + layer + " {"
	start := strings.Index(sheet, open)
	if start < 0 {
		t.Fatalf("the served sheet carries no %q block", open)
	}
	rest := sheet[start+len(open):]
	depth, out := 1, strings.Builder{}
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return out.String()
			}
		}
		out.WriteByte(rest[i])
	}
	t.Fatalf("the @layer %s block is never closed", layer)
	return ""
}

// reviewRound13Subjects returns the rule heads of a block: the text the emitter
// writes ahead of each `{`. That text is the selector a browser then resolves.
func reviewRound13Subjects(t *testing.T, block, where string) []string {
	t.Helper()
	var out []string
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimRight(line, " \t")
		if !strings.HasSuffix(line, "{") {
			continue
		}
		head := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		if head == "" || strings.HasPrefix(head, "@") {
			continue
		}
		out = append(out, head)
	}
	if len(out) == 0 {
		t.Fatalf("%s carries no rule head: this case would check nothing", where)
	}
	sort.Strings(out)
	return out
}

// reviewRound13Refusal composes one consumer rule into the served sheet and
// returns the refusal, or the accepted sheet's length.
func reviewRound13Refusal(t *testing.T, selector string) (refusal string, body int) {
	t.Helper()
	rule := css.NewSheet()
	rule.Select(selector, css.Decl("color", css.VarRef("pk-color-accent-default", "")))
	func() {
		defer func() {
			if r := recover(); r != nil {
				refusal = fmt.Sprint(r)
			}
		}()
		body = len(ui.Compose(design.Default(), ui.Extra{Sheets: []*css.Sheet{rule}}).Body)
	}()
	return refusal, body
}

// TestEveryClassTheServedSheetAddressesIsRefusedByBothSpellings is
// the brief's *Done when* ("a test proves a client rule cannot beat a component
// rule") read off the artifact rather than off a list: for every rule head in
// @layer components that addresses a class, a consumer rule at that same class is
// refused — as `.name`, and as [class~="name"], which is the same name addressed
// by value and the hole round 12 measured.
func TestEveryClassTheServedSheetAddressesIsRefusedByBothSpellings(t *testing.T) {
	t.Parallel()
	sheet := reviewRound13Served()
	heads := reviewRound13Subjects(t, reviewRound13Block(t, sheet, "components"), "@layer components")
	checked := 0
	for _, head := range heads {
		for _, written := range reviewRound13ClassTokens(t, head) {
			checked++
			selector := "." + written
			if refusal, _ := reviewRound13Refusal(t, selector); refusal == "" {
				t.Errorf("ui.Compose took a consumer rule at %s: @layer components of the served sheet addresses that class, and @layer client ranks last, so the consumer's rule wins the kernel's own element; the refusal reads no name for it", selector)
			}
			// The value inside the quotes is the same token written the same way:
			// a CSS string decodes \: and \. exactly as an identifier does, so
			// this addresses the class the browser already gave that element.
			byValue := `[class~="` + written + `"]`
			refusal, body := reviewRound13Refusal(t, byValue)
			if refusal == "" {
				t.Errorf("ui.Compose took a consumer rule at %s: the browser matches it against the elements @layer components styles at .%s and the client layer outranks that rule; the sheet carries it at %d bytes", byValue, written, body)
				continue
			}
			if body != 0 {
				t.Errorf("a refused composition (%s) still returned %d bytes of sheet", byValue, body)
			}
			if !strings.Contains(refusal, "class") {
				t.Errorf("the refusal of %s does not name what it refused: %q", byValue, refusal)
			}
		}
	}
	t.Logf("checked %d class selectors of the served sheet in both spellings", checked)
	if checked < 100 {
		t.Errorf("only %d class selectors of the served sheet were checked: the read has gone blind to something it used to see", checked)
	}
}

// reviewRound13ClassTokens returns each class a rule head addresses, spelled as the
// sheet spells it (escapes kept, so the browser decodes the same token it decodes
// here). `.focus\:z-\[1300\]:focus` is one class; `[data-component=modal]` is none.
func reviewRound13ClassTokens(t *testing.T, head string) []string {
	t.Helper()
	var out []string
	for i := 0; i < len(head); i++ {
		switch head[i] {
		case '[': // a bracketed value is data: . inside it names no class
			for i < len(head) && head[i] != ']' {
				if head[i] == '\\' {
					i++
				}
				i++
			}
		case '\\': // an escaped `.` is a class name's own character
			i++
		case '.':
			var token strings.Builder
			j := i + 1
			for ; j < len(head); j++ {
				c := head[j]
				if c == '\\' {
					token.WriteByte('\\')
					j++
					if j < len(head) {
						token.WriteByte(head[j])
					}
					continue
				}
				if strings.ContainsRune(".:>+,~*[", rune(c)) {
					break
				}
				token.WriteByte(c)
			}
			if token.Len() > 0 {
				out = append(out, token.String())
			}
			i = j - 1
		}
	}
	return out
}

// TestEveryHookTheServedSheetAddressesIsRefused is the same read on
// the other vocabulary: for every data-* attribute the kernel's own rules in
// @layer components address, the gate must refuse a consumer rule that addresses
// it — by presence and by each comparison. This is the promise kernelHooks makes,
// checked against the sheet rather than against the list kernelHooks is built
// from; a hook a renderer gains and ui/components forgets to declare is exactly
// what a list cannot see.
func TestEveryHookTheServedSheetAddressesIsRefused(t *testing.T) {
	t.Parallel()
	sheet := reviewRound13Served()
	heads := reviewRound13Subjects(t, reviewRound13Block(t, sheet, "components"), "@layer components")
	seen := map[string]bool{}
	var hooks []string
	for _, head := range heads {
		for _, name := range reviewRound13BracketNames(t, head) {
			if !seen[name] {
				seen[name] = true
				hooks = append(hooks, name)
			}
		}
	}
	t.Logf("checked %d attribute names the served sheet's own rules address", len(hooks))
	if len(hooks) == 0 {
		t.Fatal("@layer components addresses no bracketed attribute: this case would check nothing")
	}
	// What this case deliberately does not demand, reported rather than hidden:
	// the sheet's own rules also address HTML's universal attributes — [hidden] and
	// [open] are the kernel's rules in @layer components, and a consumer rule at
	// either composes, because the gate reads names and those are the platform's
	// names, not the kernel's. refuseClientSheet states that limit; a change to it
	// should show up here before it shows up on a page.
	var unread []string
	for _, head := range heads {
		for _, name := range reviewRound13BracketNamesAll(head, "") {
			if !seen[name] {
				seen[name] = true
				unread = append(unread, name)
			}
		}
	}
	if len(unread) > 0 {
		sort.Strings(unread)
		t.Logf("names the served sheet addresses that no vocabulary reads, so a consumer rule at them composes: %s", strings.Join(unread, ", "))
	}
	for _, name := range hooks {
		for _, selector := range []string{"[" + name + "]", "[" + name + `="x"]`, "[" + name + `~="x"]`, "[" + name + `*="x"]`} {
			refusal, body := reviewRound13Refusal(t, selector)
			if refusal == "" {
				t.Errorf("ui.Compose took a consumer rule at %s: @layer components of the served sheet addresses that attribute, so the consumer rule in the client layer wins the kernel's own element; the sheet carries it at %d bytes", selector, body)
				continue
			}
			if body != 0 {
				t.Errorf("a refused composition (%s) still returned %d bytes of sheet", selector, body)
			}
		}
	}
}

// reviewRound13BracketNames returns the attribute names of a rule head, canonical
// as attrNames describes them. Universal HTML attributes (hidden, open) and any
// run that is not a name are left out: the gate's stated limit covers them, and
// this case checks the vocabulary it promises, not the limit it states.
func reviewRound13BracketNames(t *testing.T, head string) []string {
	t.Helper()
	return reviewRound13BracketNamesAll(head, "data-")
}

// reviewRound13BracketNamesAll is the same read with the filter it applies given
// as an argument, so the case can report what it left out as well as what it kept.
func reviewRound13BracketNamesAll(head string, prefix string) []string {
	var out []string
	for i := 0; i < len(head); i++ {
		if head[i] != '[' {
			continue
		}
		end := i + 1
		var raw strings.Builder
		for end < len(head) && head[end] != ']' {
			if head[end] == '\\' {
				raw.WriteByte(head[end+1])
				end += 2
				continue
			}
			raw.WriteByte(head[end])
			end++
		}
		name := raw.String()
		if k := strings.IndexAny(name, "=~^$|"); k >= 0 {
			name = name[:k]
		}
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" || !isASCIILetterStart(name) {
			continue // `\[1300\]` inside an escaped class name is not an attribute
		}
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		out = append(out, name)
		i = end
	}
	return out
}

// isASCIILetterStart reports whether text begins with a letter, the one shape a
// CSS attribute name can begin with.
func isASCIILetterStart(text string) bool {
	return text != "" && (('a' <= text[0] && text[0] <= 'z') || ('A' <= text[0] && text[0] <= 'Z'))
}

// TestAClassComparisonIsRefusedInEverySpellingANameHas pins the
// spelling half of the cure. Round 11's HIGH was an attribute name written another
// way; round 12's was a class name written another way. An attribute name is
// case-insensitive and escapable, and the comparison operators are five tokens
// plus `=`; a refusal that reads one of them reads the class namespace only
// sometimes.
func TestAClassComparisonIsRefusedInEverySpellingANameHas(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{
		`[CLASS~="sr-only"]`,
		`[ ClAsS ~= "sr-only" ]`,
		`[\43 LASS~="sr-only"]`,
		`[class~="sr-only" i]`,
		`[class~="sr-only" s]`,
		`[*|class~="sr-only"]`,
		`[|class~="sr-only"]`,
		`[ class = "sr-only" ]`,
		`:is([class~="sr-only"])`,
		`.store-hero [CLASS$="sr-only"]`,
		`[data-component][class~="flex"]`,
		`[class~="flex"][class~="sr-only"]`,
	} {
		refusal, body := reviewRound13Refusal(t, selector)
		if refusal == "" {
			t.Errorf("ui.Compose took the consumer rule %s: every element the kernel gives the class this selector compares is styled in @layer components and loses to the client layer; the sheet carries it at %d bytes", selector, body)
			continue
		}
		if !strings.Contains(refusal, "class attribute") {
			t.Errorf("the refusal of %s does not name the class attribute it refused: %q", selector, refusal)
		}
	}
}

// TestTheGateStillRefusesNothingAConsumerOwns holds the cure to its
// own edge, so that reading class values out of `class=` did not become refusing
// attribute selectors, and so that the limit the gate documents stays the limit it
// documents rather than a wider one someone trips over later.
func TestTheGateStillRefusesNothingAConsumerOwns(t *testing.T) {
	t.Parallel()
	for _, selector := range []string{
		`[class]`,
		`[data-email="a.b"]`,
		`.store-card[aria-label*=".flex"]`,
		`.store-hero[data-store-region="featured"]`,
		`.store-hero [data-store-panel]`,
		`.store-hero[data-store-thing~="sr-only-ish"]`,
	} {
		refusal, body := reviewRound13Refusal(t, selector)
		if refusal != "" {
			t.Errorf("ui.Compose refused a consumer rule that names no kernel class and no kernel attribute (%s): %s", selector, refusal)
		} else if body == 0 {
			t.Errorf("the composition that took %s returned no sheet at all", selector)
		}
	}
}
