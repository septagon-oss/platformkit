package resource_test

// A restore names one version, and the panel has to say it once. The sentence
// carrying it comes from a catalogue and the number from a stored row, so a change
// that keeps the message and adds back the `" v" + n` the message replaced leaves a
// record reading "Restored from v3 v3" — and a page that contains the right sentence
// still contains it however many times the number follows it. What a person reads as
// one version or two is the whole line, so this reads the whole line: every line the
// history panel drew, in each language it draws, and what the one naming the version
// it brought back says from its first word to its last.

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

// versionLine is the paragraph naming the version this write read in order to be
// written: the only line of the series carrying the number 3 while the entry itself
// is v4.
type versionLine struct {
	what string
	want string
}

// TestThePanelNamesTheVersionItRestoredFromOnce: each language's own sentence is the
// whole of that line — its words, its number, nothing else — so the version a restore
// read is stated exactly as the copy of the language the person reads wrote it.
func TestThePanelNamesTheVersionItRestoredFromOnce(t *testing.T) {
	for _, want := range []versionLine{
		{what: "en", want: "Restored from v3"},
		{what: "pt-PT", want: "Restaurada desde a versão 3"},
	} {
		var found []string
		for _, line := range historyLines(t, want.what) {
			if strings.Contains(line, "3") {
				found = append(found, line)
			}
		}
		if len(found) != 1 {
			t.Fatalf("the %s panel names the version it restored from in %d lines, want one:\n%v",
				want.what, len(found), found)
		}
		if found[0] != want.want {
			t.Errorf("the %s panel's line about the version it restored from reads %q, want exactly %q",
				want.what, found[0], want.want)
		}
	}
}

// historyLines draws one record whose version series carries a restore of version 3
// written as version 4, and returns every line its history panel drew, with no
// markup on either side. The copy is the catalogue this package ships.
func historyLines(t *testing.T, preference string) []string {
	t.Helper()
	record := resource.Resource{
		Schema: entity.Schema{Module: "line", Entity: "line", Path: "/api/v1/line/lines",
			Fields: []entity.Field{{Name: "title", Type: entity.TypeText, Doc: "The line."}},
		},
		Screen: "/app/line/lines",
		History: &resource.HistoryPanel{
			Title: "History", Restore: "Restore", Action: "/app/line/lines/one/restore",
			Rows: []resource.VersionRow{{Number: 4, RestoredFrom: 3}},
		},
	}
	options := resource.Options{Workspace: "/app", Home: "Dashboard"}
	if preference != "" {
		selected := locale.SelectLocale(
			xtext.Load("en", xtext.Source{FS: resource.Catalogues(), Name: "ui/resource"}), preference)
		options.Locale = &selected
	}
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
	if len(lines) == 0 {
		t.Fatalf("the record drew no history line at all:\n%s", page.String())
	}
	return lines
}
