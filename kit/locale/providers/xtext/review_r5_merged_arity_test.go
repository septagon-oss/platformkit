package xtext_test

// A reviewer's case, T-0111 review round 5 (2026-09-29).
//
// `Load` merges sources in argument order — a later source answers for a key an
// earlier one carried — and it refuses a copy that renumbers a sentence's
// arguments. Both statements are true, and they are true about different things:
// the merge is over the whole catalogue, the refusal is over one source's own
// files. `checkParity` is handed `entries`, the map `read` built from one
// `Source.FS`, and never sees what another source says about the same key.
//
// So the two halves compose into a hole exactly where they meet. A module ships
// `screens.new` as `Novo %s` in its `pt-PT.json` and `New %s` in the Go text its
// call site passes; a later, product source re-words the same key in an `en.json`
// of its own — `New %s for %s`, say, which is the ordinary way a product adds the
// context its own screen has. Both sources pass their own parity check: the module
// has one non-source file and nothing to disagree with, and the product's only file
// *is* the source language, which `checkParity` exempts from presence and compares
// against nothing. The merge then keeps the product's English and the module's
// Portuguese, and the two no longer ask for the same arguments.
//
// What a person reading the Portuguese page gets is Go's own complaint, printed in
// the sentence:
//
//	pref=pt-PT language=pt-PT "Novo Widget%!(EXTRA string=Acme)"
//
// which is the failure the loader's own comment says it exists to refuse — "must
// not renumber a sentence's arguments: both conditions are refused here, at
// composition, rather than as a page that quietly answers in English".
//
// The case therefore asserts the outcome rather than one cure: either `Load`
// refuses the composition, naming the key, or every language it composes renders
// the key without Go's mismatch marker. Both branches are reachable, and the second
// file below holds the line the cure must not cross: an override that leaves the
// arguments alone is what a product is *for*, and a refusal of every shared key
// would break the brief's own §1 requirement and `apps/platformkit/catalog.go`,
// whose comment says "re-labelling \"Delete\" is a product's ordinary decision".

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
)

// oneLocale is a source that ships only a translation, the shape every kernel
// layer in this repository actually has: no en.json, because the English of a key
// is the text its call site passes.
func oneLocale(pt string) xtext.Source {
	return xtext.Source{Name: "the module", FS: fstest.MapFS{
		"pt-PT.json": &fstest.MapFile{Data: []byte(`{"screens.new": {"translation": "` + pt + `"}}`)},
	}}
}

// sourceWords is a source that re-words a key it inherited, in the source language
// alone. Nothing in `Load` objects: the source language needs no file, and this
// source has no other file to be checked against.
func sourceWords(en string) xtext.Source {
	return xtext.Source{Name: "the product", FS: fstest.MapFS{
		"en.json": &fstest.MapFile{Data: []byte(`{"screens.new": {"translation": "` + en + `"}}`)},
	}}
}

// TestALaterSourceThatReWordsAKeyKeepsTheArgumentsItsOtherLocalesCarry is the
// merged catalogue's own rule, which is the one a reader of `Load`'s doc comment
// would expect: the copy a catalogue holds for one key asks for the same arguments
// in every language it is held in.
func TestALaterSourceThatReWordsAKeyKeepsTheArgumentsItsOtherLocalesCarry(t *testing.T) {
	args := []any{"Widget", "Acme"}
	messages, refusal := loadOrRefuse(t, oneLocale("Novo %s"), sourceWords("New %s for %s"))
	if refusal != "" {
		// Refused at composition. That is a cure, and it has to name the key, or a
		// person reading the boot failure cannot tell this from any other.
		if !strings.Contains(refusal, "screens.new") {
			t.Errorf("Load refused the catalogues for %q without naming the key it cannot reconcile", refusal)
		}
		return
	}
	for _, language := range messages.Languages() {
		loc := messages.Select(language)
		// The copy is rendered with the arguments the *English* copy asks for, which
		// is what every call site passes: it was written against the product's sentence.
		got := loc.Text("screens.new", "New %s for %s", args...)
		if strings.Contains(got, "%!") {
			t.Errorf("%s renders %q from the merged catalogues: the sentence is short of an "+
				"argument its own source-language copy asks for, and %q is what a person reads. "+
				"Load refuses a source that renumbers a sentence's arguments; it composes the "+
				"same fault across two of them", loc.Language, got, got)
		}
	}
}

// TestALaterSourceMayReWordsAKeyThatKeepsItsArguments is the line the cure must not
// cross. A product re-wording a module's label is the reason Load merges at all
// (the brief's own §1: "a test that a client key overrides a module key"), and as
// long as the sentence interpolates what the original interpolated, merging it
// cannot leave another locale short. A cure that refused every key two sources
// shared would go green on the case above and break this one, and the reference
// application's catalogues() with it.
func TestALaterSourceMayReWordsAKeyThatKeepsItsArguments(t *testing.T) {
	messages, refusal := loadOrRefuse(t, oneLocale("Novo %s"), sourceWords("Add %s"))
	if refusal != "" {
		t.Fatalf("Load refused a product re-wording that kept the arguments: %s", refusal)
	}
	for _, c := range []struct{ language, want string }{
		{"en", "Add Widget"},
		{"pt-PT", "Novo Widget"},
	} {
		if got := messages.Select(c.language).Text("screens.new", "Add %s", "Widget"); got != c.want {
			t.Errorf("a product re-wording reached %q as %q, want %q", c.language, got, c.want)
		}
	}
}

// loadOrRefuse calls Load, which is documented to panic on a catalogue it cannot
// answer for, and returns either the catalogue or the panic's text.
func loadOrRefuse(t *testing.T, sources ...xtext.Source) (messages xtext.Catalog, refusal string) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			messages, refusal = nil, sprint(recovered)
		}
	}()
	return xtext.Load("en", sources...), ""
}

// sprint names a recovered value: Load panics with a formatted string, and a
// recovered panic is whatever it was given.
func sprint(recovered any) string {
	if err, ok := recovered.(error); ok {
		return err.Error()
	}
	return fmt.Sprint(recovered)
}
