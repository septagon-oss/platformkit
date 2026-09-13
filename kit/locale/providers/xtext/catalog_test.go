package xtext_test

import (
	"fmt"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"
)

func ExampleFromCatalog() {
	messages := catalog.NewBuilder(catalog.Fallback(language.English))
	if err := messages.SetString(language.English, "task.assigned", "Assigned to %s"); err != nil {
		panic(err)
	}
	if err := messages.SetString(language.EuropeanPortuguese, "task.assigned", "Atribuída a %s"); err != nil {
		panic(err)
	}
	// The worker supplies the recipient's preference; no HTTP request is needed.
	selected := locale.SelectLocale(xtext.FromCatalog(messages), "pt-PT")
	fmt.Println(selected.Language)
	fmt.Println(selected.Text("task.assigned", "Assigned to %s", "João"))
	// Output:
	// pt-PT
	// Atribuída a João
}

func TestIndependentWorkersKeepCatalogAndNumberFormatting(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"Fleet", "Stocks"} {
		messages := catalog.NewBuilder(catalog.Fallback(language.English))
		for _, entry := range []struct {
			language  language.Tag
			one, many string
		}{
			{language.English, name + ": %d task", name + ": %d tasks"},
			{language.EuropeanPortuguese, name + ": %d tarefa", name + ": %d tarefas"},
		} {
			if err := messages.Set(entry.language, "task.digest", plural.Selectf(1, "%d", plural.One, entry.one, plural.Other, entry.many)); err != nil {
				t.Fatal(err)
			}
		}
		provider := xtext.FromCatalog(messages)
		for _, tc := range []struct {
			preference, language, noun, amount string
		}{
			{"en", "en", "tasks", "12.50"},
			{"pt-PT", "pt-PT", "tarefas", "12,50"},
			{"ja", "en", "tasks", "12.50"},
		} {
			t.Run(name+"/"+tc.preference, func(t *testing.T) {
				t.Parallel()
				for range 50 {
					selected := locale.SelectLocale(provider, tc.preference)
					if got := selected.Text("task.digest", "%d tasks", 2); got != name+": 2 "+tc.noun || selected.Language != tc.language {
						t.Fatalf("worker received another language or catalog: %q (%s)", got, selected.Language)
					}
					if got := selected.Text("missing.amount", "%.2f", 12.5); got != tc.amount {
						t.Fatalf("fallback lost selected number formatting: %q, want %q", got, tc.amount)
					}
				}
			})
		}
	}
}

func TestProviderRefusesMissingCatalog(t *testing.T) {
	for _, messages := range []catalog.Catalog{nil, catalog.NewBuilder()} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("missing catalog accepted")
				}
			}()
			_ = xtext.FromCatalog(messages)
		}()
	}
}

func TestRegionalFormattingDoesNotInventAnotherContentLanguage(t *testing.T) {
	messages := catalog.NewBuilder(catalog.Fallback(language.English))
	if err := messages.SetString(language.English, "task.digest", "%d tasks"); err != nil {
		t.Fatal(err)
	}
	for preference, want := range map[string]string{"en-US": "100,000 tasks", "en-IN": "1,00,000 tasks"} {
		selected := locale.SelectLocale(xtext.FromCatalog(messages), preference)
		if got := selected.Text("task.digest", "%d tasks", 100000); got != want || selected.Language != "en" {
			t.Errorf("%s selected %q (%s), want %q (en)", preference, got, selected.Language, want)
		}
	}
}
