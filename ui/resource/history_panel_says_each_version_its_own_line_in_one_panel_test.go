package resource_test

// A record's past is a series: one panel holding every version, each version's lines
// under its own number. A case that draws one version per panel asks a panel nobody
// opens, and it cannot see a line that read a different row of the series than the one
// it was drawn beside — every row is row zero of its own panel there. This draws the
// series as the record page holds it and asks each version's lines to be that
// version's own sentences, in the copy of the language the request asked for. A
// version's own number, when it was written and by whom stay outside the ask: Go
// assembles that line from the row's fields and no catalogue answers it.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// versionFormRe finds the hidden input each version's restore form carries, whose value
// is that version's own number. One form per version, drawn after that version's lines,
// is the panel's structure and not its copy: it separates one version's lines from the
// next while leaving every sentence free to be re-worded.
var versionFormRe = regexp.MustCompile(`name="version" value="(\d+)"`)

// panelLineRe is one line the panel drew as a sentence of its own.
var panelLineRe = regexp.MustCompile(`(?s)<p[^>]*>([^<]*)</p>`)

// sentencesUnder returns the sentences the panel drew ahead of the given version's form
// — that version's whole story, its number, when and by whom apart, since the panel
// writes that first line out of the row's fields.
func sentencesUnder(block string) []string {
	var lines []string
	for _, m := range panelLineRe.FindAllStringSubmatch(block, -1) {
		lines = append(lines, strings.TrimSpace(m[1]))
	}
	if len(lines) == 0 {
		return nil
	}
	return lines[1:]
}

// drawnVersion reads a version number out of the panel's own markup, from the two
// offsets one form reported for the number it carries.
func drawnVersion(from, to int, page string) int64 {
	var n int64
	for _, r := range page[from:to] {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}

// TestThePanelSaysEachVersionItsOwnLineWhenTheSeriesDrawsTogether: a first writing, a
// save that moved no field and a restore of a two-digit version, drawn in one panel,
// must say each version's own fact under that version. A line that read its neighbour's
// row would name a version the record never restored, in a sentence whose number
// belongs to another write.
func TestThePanelSaysEachVersionItsOwnLineWhenTheSeriesDrawsTogether(t *testing.T) {
	rows := []resource.VersionRow{
		{Number: 1},                            // first written: no earlier state to compare with
		{Number: 3, When: "09:12", Who: "ana"}, // a save that moved no field
		{Number: 12, RestoredFrom: 7},          // a restore of a version of two digits
	}
	series := lines()
	series.History = &resource.HistoryPanel{
		Title: "History", Restore: "Restore", Action: "/app/line/lines/one/restore",
		Rows: rows,
	}
	messages := locale.SelectLocale(
		xtext.Load("en", xtext.Source{FS: resource.Catalogues(), Name: "ui/resource"}), "pt-PT")
	if messages.Language != "pt-PT" {
		t.Fatalf("a Portuguese request was answered in %q", messages.Language)
	}
	page := spokenRecord(t, "pt-PT", series, nil)

	forms := versionFormRe.FindAllStringSubmatchIndex(page, -1)
	if len(forms) != len(rows) {
		t.Fatalf("the panel drew %d restore forms for a series of %d versions:\n%s", len(forms), len(rows), page)
	}
	from := 0
	for i, form := range forms {
		if drawn := drawnVersion(form[2], form[3], page); drawn != rows[i].Number {
			t.Fatalf("version %d's lines end at the form of version %d:\n%s", rows[i].Number, drawn, page)
		}
		got := sentencesUnder(page[from:form[1]])
		from = form[1]
		var want []string
		if rows[i].RestoredFrom > 0 {
			want = append(want, messages.Text("screens.history.restored", "screens.history.restored", rows[i].RestoredFrom))
		}
		if rows[i].Number <= 1 {
			want = append(want, messages.Text("screens.history.first", "screens.history.first"))
		} else {
			want = append(want, messages.Text("screens.history.unchanged", "screens.history.unchanged"))
		}
		for _, sentence := range want {
			if strings.HasPrefix(sentence, "screens.history.") {
				t.Fatalf("version %d: its copy answers nothing in Portuguese, so Go's words would reach a person reading Portuguese", rows[i].Number)
			}
		}
		if len(got) != len(want) {
			t.Errorf("version %d drew %d sentences %q, want %q", rows[i].Number, len(got), got, want)
			continue
		}
		for j := range want {
			if got[j] != want[j] {
				t.Errorf("version %d: its line %q is not the sentence this language wrote %q",
					rows[i].Number, got[j], want[j])
			}
		}
	}
}
