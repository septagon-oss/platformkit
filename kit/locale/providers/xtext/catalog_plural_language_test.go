package xtext_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"golang.org/x/text/feature/plural"
	"golang.org/x/text/language"
	"golang.org/x/text/message/catalog"
)

func TestPluralUsesTheExplicitCatalogAndItsSelectedLanguage(t *testing.T) {
	messages := catalog.NewBuilder(catalog.Fallback(language.English))
	for _, entry := range []struct {
		tag        language.Tag
		one, other string
	}{
		{language.English, "%d setting", "%d settings"},
		{language.EuropeanPortuguese, "%d definição", "%d definições"},
	} {
		if err := messages.Set(entry.tag, "settings.count", plural.Selectf(1, "%d", plural.One, entry.one, plural.Other, entry.other)); err != nil {
			t.Fatal(err)
		}
	}
	selected := locale.SelectLocale(xtext.FromCatalog(messages), "pt-PT")
	if selected.Language != "pt-PT" {
		t.Fatalf("selected language = %q, want pt-PT", selected.Language)
	}
	for _, tc := range []struct {
		count int64
		want  string
	}{
		{0, "0 definições"}, {1, "1 definição"}, {2, "2 definições"}, {5, "5 definições"},
	} {
		// The same composed catalog must answer through Text and Plural. Text
		// also proves that the chosen locale actually contains this message.
		if got := selected.Text("settings.count", "%d settings", tc.count); got != tc.want {
			t.Fatalf("catalog Text at %d = %q, want %q", tc.count, got, tc.want)
		}
		if got := selected.Plural("settings.count", tc.count, "%d setting", "%d settings"); got != tc.want {
			t.Errorf("catalog Plural at %d = %q, want %q", tc.count, got, tc.want)
		}
	}
}
