package resource_test

// A restore carries two things a person never types: which version to bring back, and
// the write count this page was drawn from, which the command rechecks before it writes
// anything. It also carries the question the command's author declared for a write that
// overwrites what the person is reading. Each is drawn once per version, inside that
// version's own form. The write count belongs to the page rather than to a version, so a
// form that lost it is a form whose restore is refused every time, and a button that lost
// the question overwrites in silence. The cases over this panel read its sentences, and
// the write-count cases read the record's edit form, so nothing read any of the three off
// the restore control itself.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// restoreAction is the address the panel below posts to: the record's own address plus
// the verb of the command the Spec mounted.
const restoreAction = "/app/line/lines/one/restore"

// guardPanel is one record page whose past holds two versions, the write count this page
// read, and — when ask is not empty — the consequence the command declared for a restore.
func guardPanel(ask string) resource.Resource {
	r := lines()
	r.History = &resource.HistoryPanel{
		Title: "History", Restore: "Restore", Action: restoreAction,
		Condition: "5", Asking: ask,
		Rows: []resource.VersionRow{
			{Number: 2, When: "09:04", Who: "ana"},
			{Number: 5, When: "11:20", Who: "ana"},
		},
	}
	return r
}

// restoreForms returns one window per form posting to the panel's address, in the order
// the panel drew them. The form is the panel's own structure, so a window holds what a
// restore carries and nothing else the record page holds — not the delete control in the
// header, which asks a question of its own.
func restoreForms(t *testing.T, page string, want int) []string {
	t.Helper()
	forms := regexp.MustCompile(`(?s)<form[^>]*action="`+regexp.QuoteMeta(restoreAction)+`"[^>]*>.*?</form>`).FindAllString(page, -1)
	if len(forms) != want {
		t.Fatalf("the page drew %d forms posting to %s, want one per version (%d):\n%s", len(forms), restoreAction, want, page)
	}
	return forms
}

// posted reads the value one hidden input of one form carries, and how many forms of the
// page carry that input at all.
func posted(t *testing.T, form, name string) string {
	t.Helper()
	found := regexp.MustCompile(`name="` + name + `"[^>]*value="([^"]*)"`).FindStringSubmatch(form)
	if len(found) != 2 {
		t.Fatalf("this restore form carries no hidden %q input:\n%s", name, form)
	}
	return found[1]
}

// TestEveryRestoreFormPostsTheVersionAndTheWriteCountThisPageRead: each version's own
// number and the write count the page read travel together in that version's form, one
// pair per version, so the command can refuse a restore that quotes a revision the
// record has left behind instead of writing over the newer one.
func TestEveryRestoreFormPostsTheVersionAndTheWriteCountThisPageRead(t *testing.T) {
	const condition = "5"
	page := spokenRecord(t, "", guardPanel(condition), nil)
	forms := restoreForms(t, page, 2)
	if got := strings.Count(page, `name="expectedRevision"`); got != len(forms) {
		t.Errorf("the page names expectedRevision %d times for %d restore controls: each posts the write count, and no version posts it twice", got, len(forms))
	}
	for i, number := range []string{"2", "5"} {
		if drawn := posted(t, forms[i], "version"); drawn != number {
			t.Errorf("the restore form drawn beside version %s posts version %q", number, drawn)
		}
		if drawn := posted(t, forms[i], "expectedRevision"); drawn != condition {
			t.Errorf("version %s's restore posts expectedRevision %q, want the %q this page was drawn from", number, drawn, condition)
		}
	}
}

// TestEveryRestoreButtonAsksOnlyTheQuestionTheCommandDeclared: a command that names what
// a restore costs gets that question on its control, and one that names nothing gets no
// question at all — a panel that invented one would be the kernel answering for the
// command's author.
func TestEveryRestoreButtonAsksOnlyTheQuestionTheCommandDeclared(t *testing.T) {
	const ask = "This puts that version back over what you are reading now."
	for i, form := range restoreForms(t, spokenRecord(t, "", guardPanel(ask), nil), 2) {
		for _, want := range []string{`data-confirm="` + ask + `"`, `data-confirm-label="Restore"`} {
			if !strings.Contains(form, want) {
				t.Errorf("version %d's restore button carries no %s, so the write happens before the person reads what it costs:\n%s",
					i+1, want, form)
			}
		}
	}
	for _, form := range restoreForms(t, spokenRecord(t, "", guardPanel(""), nil), 2) {
		if strings.Contains(form, "data-confirm") {
			t.Errorf("a command that declared no consequence drew a confirmation anyway, so the page asks a question nobody wrote:\n%s", form)
		}
	}
}
