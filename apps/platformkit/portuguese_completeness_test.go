package main

// The Portuguese half of every declared word, gated.
//
// The loader's own parity check compares the locales *within one source*, and
// each of these modules ships exactly one translated file, so it has nothing to
// compare and a hint table shipped without its Portuguese half composes happily.
// `TestNoReferenceHintStringIsEverEmpty` cannot see that either: the missing key
// answers with the declared English, and English is not empty. The only question
// that distinguishes the two is "does the Portuguese catalog answer this key", so
// that is the question asked here — of every key the shipped resolver derives from
// the five declarations, and answered with the resolver rather than with a second
// spelling of the key grammar.

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// unfound is the sentence no catalog holds. A missing key answers with whatever
// fallback it is handed, so the case hands it this one and treats it coming back
// as the absence it is. Inequality with the English is never the test: some
// Portuguese words legitimately are the English word ("Site").
const unfound = "\u2022no-copy-for-this-key\u2022"

// TestEveryDeclaredHintStringIsAnsweredInPortuguese walks the five reference
// resources through `resource.Words` — the same resolver the catalogue and the
// generated screens read — with a seam that records every key it is asked for and
// answers from the composed Portuguese catalog, and refuses any key the catalog
// does not answer.
func TestEveryDeclaredHintStringIsAnsweredInPortuguese(t *testing.T) {
	selected := page.SelectLocale(page.Messages(catalogues()), "pt-PT")
	if selected.Language != "pt-PT" {
		t.Fatalf("the composition does not answer in pt-PT: it chose %q", selected.Language)
	}
	asked := 0
	silent := []string{}
	var text resource.Text = func(key, declared string) string {
		asked++
		if out := selected.Formatter.Text(key, unfound); out == unfound || strings.TrimSpace(out) == "" {
			silent = append(silent, fmt.Sprintf("%s (declared %q)", key, declared))
			return declared
		}
		return selected.Formatter.Text(key, declared)
	}
	for _, name := range []struct{ module, entity string }{
		{"task", "task"}, {"content", "content"}, {"user", "user"}, {"billing", "plan"}, {"site", "settings"},
	} {
		r := resourceAt(t, name.module, name.entity)
		words := resource.WordsFor(r.Schema, text)
		words.Entry(r.Present)
		words.Fields(r.Schema.Fields)
		for _, c := range r.Commands {
			words.Command(c.Verb, c.Present)
			words.CommandFields(c.Verb, c.Fields)
		}
	}
	slices.Sort(silent)
	if len(silent) > 0 {
		t.Errorf("%d declared hint strings have no Portuguese copy, so a pt-PT request is served the English beside Portuguese words:\n%s",
			len(silent), strings.Join(silent, "\n"))
	}
	if asked < 50 {
		t.Errorf("only %d keys were derived from the five declarations; the resolver stopped walking them", asked)
	}
}
