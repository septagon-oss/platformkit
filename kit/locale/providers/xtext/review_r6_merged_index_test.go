package xtext

// A reviewer's case, T-0111 review round 6 (2026-09-30).
//
// Round 5's Finding 2 was that `checkParity` reads one source while `Load` merges across
// them, so one merged key could ask for two arguments in one language and one in another
// and the reader got Go's own mismatch marker. `f473a5b` added `checkMergedVerbs` and
// `a018e85` closed its zero-argument reading. Both are right, and this file keeps both:
// an arity that changes across the merge is refused, and a re-wording that keeps the
// arguments still merges.
//
// What the guard cannot read is an argument named by its position. `verbPattern` is
// `%[-+# 0]*[0-9.]*[a-zA-Z]`, which stops at the `[` of `%[1]s`, so a copy that indexes
// reads as a sentence asking for nothing. Two consequences, both measured at this head
// (the two first cases below, red, are the same composition turned around):
//
//   - refused for nothing: a module ships `Novo %s para %s` in pt-PT and a product
//     answers it in its own `en.json` as `New %[1]s for %[2]s` — the same two arguments,
//     in the order the language wants. Boot panics:
//     `xtext: product/en.json and module/pt-PT.json disagree about the arguments of
//     "screens.new": one copy asks for "", the other for "%s %s"`.
//     Re-ordering is the commonest thing a Portuguese sentence has to do to an English
//     one and indexing is how Go says to do it; `TestOneSourceMayIndexItsArguments`
//     shows the renderer speaks these verbs (`Novo Widget para Acme`), so what is
//     refused is a composition that did nothing wrong, at boot, for the whole deployment;
//   - accepted when it should not be: both sources index and the argument lists really
//     do differ — module `Novo %[1]s para %[2]s`, product `New %[1]s` — the merge loads
//     and the Portuguese page prints `Novo Acme para %!s(BADINDEX)`, which is exactly the
//     sentence `checkMergedVerbs` exists to make impossible, in the reader's language.
//
// One fix answers both red cases and no other: read `%[n]s` as the verb it is (an
// indexed `%[2]s` is still one `%s`), and compare what the two copies ask for.
// `TestAMergedCopyThatReOrdersTheSameArgumentsLoads` then goes green because the
// argument *set* matches, and `TestAMergedCopyThatAddsAnIndexedArgumentIsRefused` stays
// green because the sets differ.
//
// Nothing in this repository reaches either case today — no shipped catalogue indexes a
// verb (`grep -R '%\[' ui/*/messages modules/*/messages` answers nothing) and no key is
// carried by two of the three sources in `apps/platformkit/catalog.go` — so no page is
// wrong now. It is the merge refusing, or wrongly accepting, the first product that
// re-orders a module's sentence, which is the case the merge order exists for. Same
// shape and distance as round 5's Finding 2, one guard later.

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"
)

// catalogOf builds one source's files from locale/copy pairs, over one key.
func catalogOf(pairs ...[2]string) fstest.MapFS {
	out := fstest.MapFS{}
	for _, p := range pairs {
		out[p[0]] = &fstest.MapFile{Data: []byte(`{"screens.new":{"translation":"` + p[1] + `"}}`)}
	}
	return out
}

// loaded answers what Load did with these sources: the catalogue, or the refusal it
// panicked with. A panic is a result here, not a crash of the run.
func loaded(t *testing.T, sources ...Source) (catalog Catalog, refusal string) {
	t.Helper()
	defer func() {
		if p := recover(); p != nil {
			refusal = fmt.Sprintf("%v", p)
		}
	}()
	return Load("en", sources...), ""
}

// TestAMergedCopyThatReordersTheSameArgumentsLoads — the first red case: two arguments
// in each language, the product's English naming them by position. The refusal is the
// defect; loading and rendering both ways is the assertion.
func TestAMergedCopyThatReordersTheSameArgumentsLoads(t *testing.T) {
	t.Parallel()
	module := Source{Name: "module", FS: catalogOf([2]string{"pt-PT.json", "Novo %s para %s"})}
	product := Source{Name: "product", FS: catalogOf([2]string{"en.json", "New %[1]s for %[2]s"})}

	c, refusal := loaded(t, module, product)
	if refusal != "" {
		t.Errorf("a product that re-orders a module key's arguments — the same verbs, the order pt-PT wants "+
			"— refuses to boot: %s\nverbPattern stops at the bracket, so an indexed verb reads as no argument "+
			"at all and the guard compares an empty list against the module's real one; "+
			"TestOneSourceMayIndexItsArguments shows the renderer speaks these verbs, so the refusal falls on "+
			"a composition that changed the words and not the arguments", refusal)
	} else {
		if got := c.Select("pt-PT").Text("screens.new", "fallback", "Acme", "Widget"); !strings.Contains(got, "Novo Acme para Widget") {
			t.Errorf("the merged pt-PT copy did not answer with the two arguments it was given: %q", got)
		}
		if got := c.Select("en").Text("screens.new", "fallback", "Acme", "Widget"); !strings.Contains(got, "New Acme for Widget") {
			t.Errorf("the merged en copy did not answer with the two arguments it was given: %q", got)
		}
	}
}

// TestAMergedCopyThatAddsAnIndexedArgumentIsRefused — the second red case, and the one
// that reaches a page: with both sides indexing, the guard sees two empty lists and the
// merge ships a sentence short of an argument.
func TestAMergedCopyThatAddsAnIndexedArgumentIsRefused(t *testing.T) {
	t.Parallel()
	module := Source{Name: "module", FS: catalogOf([2]string{"pt-PT.json", "Novo %[1]s para %[2]s"})}
	product := Source{Name: "product", FS: catalogOf([2]string{"en.json", "New %[1]s"})}

	c, refusal := loaded(t, module, product)
	if refusal == "" {
		broken := c.Select("pt-PT").Text("screens.new", "fallback", "Acme")
		t.Errorf("a merged copy carrying one argument while its pt-PT sentence interpolates two loaded, and "+
			"the page prints %q: %s\nboth copies read as empty verb lists, so the check round 5 asked for "+
			"compares nothing — this is the same sentence the guard exists to make impossible", broken, "it did not refuse")
	}
}

// TestOneSourceMayIndexItsArguments is the premise of the case above and is green at
// this head: the renderer speaks indexed verbs, so a guard that cannot is the thing at
// fault, not the translation.
func TestOneSourceMayIndexItsArguments(t *testing.T) {
	t.Parallel()
	one := Source{Name: "one", FS: catalogOf(
		[2]string{"en.json", "New %[1]s for %[2]s"},
		[2]string{"pt-PT.json", "Novo %[2]s para %[1]s"})}

	c, refusal := loaded(t, one)
	if refusal != "" {
		t.Fatalf("one source cannot carry an indexed verb at all, so the premise above is wrong: %s", refusal)
	}
	if got := c.Select("pt-PT").Text("screens.new", "fallback", "Acme", "Widget"); !strings.Contains(got, "Widget para Acme") {
		t.Errorf("x/text did not render the reordered sentence it was given: %q", got)
	}
}

// TestAMergedCopyThatChangesTheArgumentListStaysRefused is the fault the guard exists
// for, spelled without indexing so it is unaffected by the fix: boot refuses, naming
// both sources and the key.
func TestAMergedCopyThatChangesTheArgumentListStaysRefused(t *testing.T) {
	t.Parallel()
	module := Source{Name: "module", FS: catalogOf([2]string{"pt-PT.json", "Novo %s para %s"})}
	product := Source{Name: "product", FS: catalogOf([2]string{"en.json", "New"})}

	_, refusal := loaded(t, module, product)
	if refusal == "" {
		t.Fatalf("a merged copy that drops an argument the other language interpolates loaded: the guard " +
			"round 5 asked for is gone")
	}
	for _, want := range []string{"module", "product", "screens.new"} {
		if !strings.Contains(refusal, want) {
			t.Errorf("the refusal omits %q: %s", want, refusal)
		}
	}
}

// TestAMergedCopyThatKeepsItsArgumentsStillMerges is round 5's own case, kept alive so a
// cure for the re-ordering case cannot become "refuse anything two sources share".
func TestAMergedCopyThatKeepsItsArgumentsStillMerges(t *testing.T) {
	t.Parallel()
	module := Source{Name: "module", FS: catalogOf([2]string{"pt-PT.json", "Novo %s para %s"})}
	product := Source{Name: "product", FS: catalogOf([2]string{"en.json", "Remove %s from %s"})}

	c, refusal := loaded(t, module, product)
	if refusal != "" {
		t.Fatalf("a product re-wording one key and keeping its two arguments was refused: %s", refusal)
	}
	if len(c.Languages()) != 2 {
		t.Errorf("Languages() = %v, want en and pt-PT", c.Languages())
	}
}
