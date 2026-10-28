package resource_test

// The comparison is what a person opens a record's history for: under each version,
// which field the write moved and which words it left standing, put in and took out.
// Every case over the panel draws versions that moved no field, so none of them runs
// the panel's comparison loop, and a line that drew another version's comparison
// answers each of them green. This draws two writes that each moved a field, in the
// one panel the record page holds, and asks each version to be told by its own words —
// with what a write added still marked as an addition and what it dropped still marked
// as a deletion. Which field a line names and how the sentence around it reads belong
// to the caller and to the reader's language, so this reads the words and their
// marking only, never the punctuation that joins them.

import (
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// movedSeries is one record's past as the record page holds it: two writes that each
// moved a field, each holding words no other version holds.
func movedSeries() resource.Resource {
	r := lines()
	r.History = &resource.HistoryPanel{
		Title: "History", Restore: "Restore", Action: "/app/line/lines/one/restore",
		Condition: "5",
		Rows: []resource.VersionRow{
			{Number: 2, When: "09:04", Who: "ana", Changes: []resource.VersionChange{{
				Label: "Title",
				Edits: []resource.VersionEdit{
					{Text: "Planning", Kind: resource.EditKept},
					{Text: " for autumn", Kind: resource.EditInserted},
				},
			}}},
			{Number: 5, When: "11:20", Who: "ana", Changes: []resource.VersionChange{{
				Label: "Body",
				Edits: []resource.VersionEdit{
					{Text: "Old draft", Kind: resource.EditRemoved},
					{Text: "Current draft", Kind: resource.EditKept},
				},
			}}},
		},
	}
	return r
}

// versionBlocks cuts the page at the form each version's restore control carries —
// one form per version, holding that version's own number, drawn after that version's
// lines. The form is the panel's structure and not its copy, so the windows it gives
// stay correct across any re-wording.
func versionBlocks(t *testing.T, page string, numbers []int64) []string {
	t.Helper()
	forms := regexp.MustCompile(`name="version" value="(\d+)"`).FindAllStringSubmatchIndex(page, -1)
	if len(forms) != len(numbers) {
		t.Fatalf("the panel drew %d restore forms for a series of %d versions:\n%s", len(forms), len(numbers), page)
	}
	blocks := make([]string, len(forms))
	from := 0
	for i, form := range forms {
		if drawn := page[form[2]:form[3]]; drawn != strconv.FormatInt(numbers[i], 10) {
			t.Fatalf("a form drawn after version %d's lines carries version %q", numbers[i], drawn)
		}
		blocks[i] = page[from:form[1]]
		from = form[1]
	}
	return blocks
}

// toldBy is one word a person reads under one version: the text itself, and the
// element that says what it is to the write being shown — none for a word both
// versions hold, <ins> for one this write added, <del> for one it dropped.
type toldBy struct {
	value   string
	marking string
}

// TestThePanelSaysWhatEachVersionMovedUnderThatVersion: each version's field and words
// are drawn under that version and nowhere else in the panel, and each word keeps the
// marking of the comparison it came from.
func TestThePanelSaysWhatEachVersionMovedUnderThatVersion(t *testing.T) {
	page := spokenRecord(t, "", movedSeries(), nil)
	rows := movedSeries().History.Rows
	blocks := versionBlocks(t, page, []int64{2, 5})
	wants := [][]toldBy{
		{{value: "Planning"}, {value: " for autumn", marking: "ins"}},
		{{value: "Old draft", marking: "del"}, {value: "Current draft"}},
	}
	for i, block := range blocks {
		if !strings.Contains(block, rows[i].Changes[0].Label) {
			t.Errorf("version %d's lines name no field for its comparison, want the field the caller called %q:\n%s",
				rows[i].Number, rows[i].Changes[0].Label, block)
		}
		for _, want := range wants[i] {
			if strings.Count(page, want.value) != 1 {
				t.Errorf("version %d's word %q is drawn %d times in the panel it belongs to", rows[i].Number, want.value, strings.Count(page, want.value))
			}
			if !strings.Contains(block, want.value) {
				t.Errorf("version %d's word %q is not drawn under version %d:\n%s", rows[i].Number, want.value, rows[i].Number, block)
			}
			if want.marking == "" {
				continue
			}
			marked := regexp.MustCompile(`(?s)<` + want.marking + `[^>]*>` + regexp.QuoteMeta(want.value) + `</` + want.marking + `>`)
			if !marked.MatchString(block) {
				t.Errorf("version %d's word %q is not drawn inside a <%s> element, so a reader cannot see what it is to the write:\n%s",
					rows[i].Number, want.value, want.marking, block)
			}
		}
	}
}
