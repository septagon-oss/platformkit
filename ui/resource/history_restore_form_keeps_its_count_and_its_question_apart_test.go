package resource_test

// A restore form carries a number and a sentence, and the pair reads as one control to the
// person who presses it: the write count this page was drawn from goes inside the form where
// nobody can see it, and the consequence the command declared goes on the button where they
// can. Drawn from the same panel with the same value in both places, those two are one
// string, and a form that put the sentence where the count belongs would still answer every
// assertion that only reads the shape. So this page is drawn with a count and a question that
// differ, and each attribute is read as the one value it may hold.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// apartAction is where the panel below posts, and apartCount and apartQuestion are the two
// values a restore carries — deliberately unlike each other, so that which one reached which
// attribute is a fact the page answers.
const (
	apartAction   = "/app/line/lines/one/restore"
	apartCount    = "17"
	apartQuestion = "This puts that version back over what you are reading now."
)

// apartPanel is one record page whose past holds two versions, the write count named
// apartCount and the consequence named apartQuestion.
func apartPanel() resource.Resource {
	r := lines()
	r.History = &resource.HistoryPanel{
		Title: "History", Restore: "Restore", Action: apartAction,
		Condition: apartCount, Asking: apartQuestion,
		Rows: []resource.VersionRow{
			{Number: 3, When: "08:15", Who: "ana"},
			{Number: 9, When: "16:40", Who: "ben"},
		},
	}
	return r
}

// TestTheRestoreFormPostsTheWriteCountAndNotTheQuestionItAsks: each version's own form posts
// the write count this page read as its condition, and shows the declared consequence on its
// button — never the one in the other's place. A form that posted the question where the
// count belongs is a restore the command refuses every time it is asked, and one that showed
// the count as its warning is a page that warns of a number.
func TestTheRestoreFormPostsTheWriteCountAndNotTheQuestionItAsks(t *testing.T) {
	page := spokenRecord(t, "", apartPanel(), nil)
	forms := regexp.MustCompile(`(?s)<form[^>]*action="`+regexp.QuoteMeta(apartAction)+`"[^>]*>.*?</form>`).FindAllString(page, -1)
	if len(forms) != 2 {
		t.Fatalf("the page drew %d restore forms, want one per version:\n%s", len(forms), page)
	}
	for i, want := range []string{"3", "9"} {
		form := forms[i]
		got := regexp.MustCompile(`name="expectedRevision"[^>]*value="([^"]*)"`).FindStringSubmatch(form)
		if len(got) != 2 {
			t.Fatalf("this restore form carries no hidden write count:\n%s", form)
		}
		if got[1] != apartCount {
			t.Errorf("the form restoring version %s posts the condition %q, want the write count %q this page was drawn from",
				want, got[1], apartCount)
		}
		if strings.Contains(got[1], apartQuestion) {
			t.Errorf("the form restoring version %s posts the declared consequence as its write count, so the command can never accept the restore", want)
		}
		if drawn := regexp.MustCompile(`name="version"[^>]*value="([^"]*)"`).FindStringSubmatch(form); len(drawn) != 2 || drawn[1] != want {
			t.Errorf("the form posted version %v, want %s", drawn, want)
		}
		if !strings.Contains(form, `data-confirm="`+apartQuestion+`"`) {
			t.Errorf("version %s's restore button shows no consequence the command declared, or shows something else in its place:\n%s", want, form)
		}
	}
}
