package page_test

import (
	"strings"
	"testing"

	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/ui/page"
)

func localeMessages(t *testing.T, name string) page.Messages {
	t.Helper()
	messages := catalog.NewBuilder(catalog.Fallback(language.English))
	for _, entry := range []struct {
		tag       language.Tag
		one, many string
	}{
		{language.English, name + ": %d open task", name + ": %d open tasks"},
		{language.EuropeanPortuguese, name + ": %d tarefa aberta", name + ": %d tarefas abertas"},
	} {
		if err := messages.Set(entry.tag, "task.open", plural.Selectf(1, "%d", plural.One, entry.one, plural.Other, entry.many)); err != nil {
			t.Fatal(err)
		}
	}
	if err := messages.SetString(language.Portuguese, "task.owner", "Responsável: %s"); err != nil {
		t.Fatal(err)
	}
	return page.FromCatalog(messages)
}

func TestLocaleNegotiationUsesExplicitChoiceThenBrowserThenCatalog(t *testing.T) {
	t.Parallel()
	messages := localeMessages(t, "Fleet")
	for _, tc := range []struct {
		name        string
		preferences []string
		want        string
	}{
		{"catalog default", nil, "en"},
		{"weighted browser", []string{"en;q=0.5,pt-PT;q=0.9"}, "pt-PT"},
		{"explicit preference", []string{"en", "pt-PT"}, "en"},
		{"regional preference", []string{"en-GB"}, "en"},
		{"malformed explicit preference", []string{"[invalid]", "pt-PT"}, "pt-PT"},
		{"unsupported explicit preference", []string{"ja", "pt-PT"}, "pt-PT"},
		{"unsupported browser", []string{"ja"}, "en"},
		{"excluded language", []string{"pt-PT;q=0,en;q=1"}, "en"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := page.SelectLocale(messages, tc.preferences...).Language; got != tc.want {
				t.Errorf("content language = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestLocaleComposesPluralParentFallbackAndEscapedInterpolation(t *testing.T) {
	t.Parallel()
	locale := page.SelectLocale(localeMessages(t, "Fleet"), "pt-PT")
	for count, want := range map[int]string{1: "Fleet: 1 tarefa aberta", 2: "Fleet: 2 tarefas abertas"} {
		if got := locale.Text("task.open", "%d open tasks", count); got != want {
			t.Errorf("task count = %q, want %q", got, want)
		}
	}
	if got := locale.Text("task.owner", "Owner: %s", "João"); got != "Responsável: João" {
		t.Errorf("parent translation = %q", got)
	}
	if got := locale.Text("task.missing", "Assigned to %s", "João"); got != "Assigned to João" {
		t.Errorf("missing message exposed a key or lost interpolation: %q", got)
	}
	text := locale.Text("task.owner", "Owner: %s", `<img src=x onerror="alert(1)">`)
	output := render(t, g.Text(text))
	if strings.Contains(output, "<img") || !strings.Contains(output, "&lt;img") {
		t.Errorf("localized interpolation was interpreted as HTML: %s", output)
	}
}

func TestLocalesNeverBorrowAnotherCompositionsMessages(t *testing.T) {
	t.Parallel()
	first, second := localeMessages(t, "Fleet"), localeMessages(t, "Stocks")
	for _, tc := range []struct {
		name, language, want string
		messages             page.Messages
	}{
		{"Fleet English", "en", "Fleet: 2 open tasks", first},
		{"Fleet Portuguese", "pt-PT", "Fleet: 2 tarefas abertas", first},
		{"Stocks English", "en", "Stocks: 2 open tasks", second},
		{"Stocks Portuguese", "pt-PT", "Stocks: 2 tarefas abertas", second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for range 50 {
				locale := page.SelectLocale(tc.messages, tc.language)
				if got := locale.Text("task.open", "%d open tasks", 2); got != tc.want {
					t.Fatalf("request used another language or catalog: %q", got)
				}
			}
		})
	}
}
