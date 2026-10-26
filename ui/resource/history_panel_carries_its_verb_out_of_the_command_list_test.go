package resource_test

// The history panel is the restore command's control: one form per version, with that
// version and the write count this page read inside it. A resource that says the panel
// carries the verb must not also draw that command as a generic form above the panel,
// where no version is known and the write is refused for want of one. A panel that
// carries one verb is no reason to hide the caller's other commands either, so both
// directions of the mistake are judged here.

import (
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// carriedAction is where the restore below posts: the record's own address plus the
// verb of the command the panel carries, as the adapter composes it. carriedCount is
// the write count this page read, unlike every version number below.
const (
	carriedItem   = "/app/note/notes/11111111-1111-1111-1111-111111111111"
	carriedAction = carriedItem + "/restore"
	carriedCount  = "14"
)

// TestTheCommandTheHistoryPanelCarriesIsNotOfferedAgainAboveIt: the carried command has
// its one control per version inside the panel, and the caller's other command keeps
// its one control beside it.
func TestTheCommandTheHistoryPanelCarriesIsNotOfferedAgainAboveIt(t *testing.T) {
	r := note()
	r.Commands = []resource.Command{
		{Verb: "restore", Label: "Restore this version", Description: "Puts a version back.",
			Base: "/app/note/notes", Fields: []entity.Field{{Name: "version", Type: entity.TypeInt}}},
		assignCommand,
	}
	r.History = &resource.HistoryPanel{
		Title: "History", Restore: "Restore", Carries: "restore", Action: carriedAction,
		Condition: carriedCount,
		Rows: []resource.VersionRow{
			{Number: 3, When: "08:15", Who: "ana"},
			{Number: 9, When: "16:40", Who: "ben"},
		},
	}
	page := render(t, resource.Detail(r, opts, map[string]any{
		"id": "11111111-1111-1111-1111-111111111111", "title": "Buy milk",
	}, true).Body)
	forms := regexp.MustCompile(`(?s)<form[^>]*action="`+regexp.QuoteMeta(carriedAction)+`"[^>]*>.*?</form>`).FindAllString(page, -1)
	if len(forms) != len(r.History.Rows) {
		t.Fatalf("the page drew %d forms posting to %s, want one per version and no second door for the command the panel carries:\n%s",
			len(forms), carriedAction, around(page, carriedAction))
	}
	for i, want := range []string{"3", "9"} {
		if drawn := regexp.MustCompile(`name="version"[^>]*value="([^"]*)"`).FindStringSubmatch(forms[i]); len(drawn) != 2 || drawn[1] != want {
			t.Errorf("form %d beside the panel posts version %v, want %s: %s", i+1, drawn, want, forms[i])
		}
		if drawn := regexp.MustCompile(`name="expectedRevision"[^>]*value="([^"]*)"`).FindStringSubmatch(forms[i]); len(drawn) != 2 || drawn[1] != carriedCount {
			t.Errorf("form %d beside the panel posts %v as its write count, want the %q this page was drawn from: %s", i+1, drawn, carriedCount, forms[i])
		}
	}
	if n := strings.Count(page, `action="`+assignCommand.Action(carriedItem)+`"`); n != 1 {
		t.Errorf("the caller's other command was drawn %d times on a page whose panel carries one verb, want its one control", n)
	}
}
