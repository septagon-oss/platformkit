package resource_test

// Every line the history panel draws under a version — except the version's own
// number, when it was written and by whom, which Go assembles from the row's fields
// and no catalogue answers — is a sentence of the language the person reading asked
// for: which version a restore read in order to be written, and what the write
// moved. Each has to arrive whole, as that language's copy wrote it: a line that is
// half a catalogue and half something Go appended is one sentence to nobody, and a
// line left in Go's own words is English leaking onto a Portuguese page.

import (
	"regexp"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// panelCopy is one sentence the panel owes a version, with the key it came from, so
// a failure names the copy a person did not get.
type panelCopy struct {
	key      string
	sentence string
}

// askedFor reads one key of the copy this package ships for the reader's language,
// with its own key as the fallback: an answer equal to the key is a language that
// carries no copy of it, which is what a line in Go's words on the page means.
func askedFor(key string, messages locale.Locale, args ...any) panelCopy {
	return panelCopy{key: key, sentence: messages.Text(key, key, args...)}
}

// drawnPanelLines draws one record whose history holds exactly this version and
// returns every line the panel drew under it, markup stripped, in reading order.
func drawnPanelLines(t *testing.T, preference string, row resource.VersionRow) []string {
	t.Helper()
	record := resource.Resource{
		Schema: entity.Schema{Module: "line", Entity: "line", Path: "/api/v1/line/lines",
			Fields: []entity.Field{{Name: "title", Type: entity.TypeText, Doc: "The line."}},
		},
		Screen: "/app/line/lines",
		History: &resource.HistoryPanel{
			Title: "History", Restore: "Restore", Action: "/app/line/lines/one/restore",
			Rows: []resource.VersionRow{row},
		},
	}
	options := resource.Options{Workspace: "/app", Home: "Dashboard"}
	selected := locale.SelectLocale(
		xtext.Load("en", xtext.Source{FS: resource.Catalogues(), Name: "ui/resource"}), preference)
	options.Locale = &selected
	view := resource.DetailRichText(record, options,
		map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "A line"}, true, nil, nil)
	var page strings.Builder
	if err := g.Group(view.Body).Render(&page); err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, m := range regexp.MustCompile(`(?s)<p[^>]*>([^<]*)</p>`).FindAllStringSubmatch(page.String(), -1) {
		lines = append(lines, strings.TrimSpace(m[1]))
	}
	if len(lines) < 2 {
		t.Fatalf("the %s panel drew %d lines for version %d, want its version line and at least one sentence:\n%s",
			preference, len(lines), row.Number, page.String())
	}
	return lines
}

// TestThePanelDrawsEveryLineInTheReadersLanguage asks a version series — a first
// writing, a save that moved no field, a restore of a two-digit version — in the
// language this package ships a copy for, and requires the lines under each version
// to be exactly that language's own sentences with the row's own arguments: one
// where a line gained an argument bolted onto the copy, one where the panel left a
// sentence out, one where a copy went missing and Go's words showed through.
func TestThePanelDrawsEveryLineInTheReadersLanguage(t *testing.T) {
	messages := locale.SelectLocale(
		xtext.Load("en", xtext.Source{FS: resource.Catalogues(), Name: "ui/resource"}), "pt-PT")
	if messages.Language != "pt-PT" {
		t.Fatalf("a Portuguese request was answered in %q", messages.Language)
	}
	for _, row := range []resource.VersionRow{
		{Number: 1},                            // first written: there was no earlier state
		{Number: 3, When: "09:12", Who: "ana"}, // a save that moved no field
		{Number: 12, RestoredFrom: 7},          // a restore of a version of two digits
	} {
		var want []panelCopy
		if row.RestoredFrom > 0 {
			want = append(want, askedFor("screens.history.restored", messages, row.RestoredFrom))
		}
		if row.Number <= 1 {
			want = append(want, askedFor("screens.history.first", messages))
		} else {
			want = append(want, askedFor("screens.history.unchanged", messages))
		}
		for _, c := range want {
			if c.sentence == c.key {
				t.Fatalf("version %d: %s answers nothing in Portuguese, so the panel would draw Go's words to a person reading Portuguese",
					row.Number, c.key)
			}
		}
		got := drawnPanelLines(t, "pt-PT", row)[1:] // its own number, when and by whom
		if len(got) != len(want) {
			t.Fatalf("version %d: the panel drew %d sentence lines %q, want %d: %q",
				row.Number, len(got), got, len(want), want)
		}
		for i := range want {
			if got[i] != want[i].sentence {
				t.Errorf("version %d: its line %q (%s) is not the sentence this language wrote %q",
					row.Number, got[i], want[i].key, want[i].sentence)
			}
		}
	}
}
