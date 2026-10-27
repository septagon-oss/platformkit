package screens

// The panel's rows are composed here as well as its door: ui/screens decodes every
// version's field set and compares each one with the version before it, and
// ui/resource draws whatever comparison arrives. Every case beside the panel hands
// the renderer rows it wrote itself, so the layer that decides which two versions
// meet had no reader. This asks the served record page — the view Mount renders —
// for its own markup and reads three things off it: the restore door this record's
// own command opens, one form per version quoting that version and the row's write
// count, and each write marked against the version before it rather than against
// itself. The oldest version on the page compares against nothing, because no
// earlier state arrived with the series.

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestTheServedRecordPageComparesEachVersionAgainstTheOneBeforeIt(t *testing.T) {
	const at = "/app/note/notes"
	id := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	item, versioned := at+"/"+id.String(), httpx.Resource{
		Module: "note", Entity: "note", Path: "/notes", Screen: at,
		Schema: entity.Schema{Module: "note", Entity: "note", Path: "/notes",
			Fields: []entity.Field{{Name: "title", Type: entity.TypeString}}},
		ReadBy:   httpx.SignedIn(),
		Commands: []httpx.Command{panelRestoreCommand("restore")},
		History: &httpx.History{RestoreVerb: "restore", Asks: "This puts that version back.",
			Versions: func(context.Context, uuid.UUID) ([]httpx.Version, error) {
				// The series arrives newest first, and each document holds the words
				// that version left: only the pair the adapter picks decides what a
				// reader sees marked.
				return []httpx.Version{
					{Version: 9, At: time.Unix(1700000000, 0).UTC(),
						Fields: json.RawMessage(`{"title":"Buy bread today"}`), Changed: []string{"title"}},
					{Version: 8, At: time.Unix(1600000000, 0).UTC(),
						Fields: json.RawMessage(`{"title":"Buy milk today"}`), Changed: []string{"title"}},
				}, nil
			}},
	}
	row := map[string]any{"id": id.String(), "title": "Buy bread today", entity.RevisionField: float64(14)}

	mounted, ctx := historyServedTo(t, versioned)
	served, err := detailView(mounted, ctx,
		Options{Workspace: "/app", Home: "Workspace"}, at, row, true)
	if err != nil {
		t.Fatalf("the record page of a resource whose History answers would not render: %v", err)
	}
	var b strings.Builder
	for _, n := range served.Body {
		if err := n.Render(&b); err != nil {
			t.Fatalf("render the record page: %v", err)
		}
	}
	page := b.String()

	forms := regexp.MustCompile(`(?s)<form[^>]*action="`+
		regexp.QuoteMeta(resource.Command{Verb: "restore", Base: at}.Action(item))+
		`"[^>]*>.*?</form>`).FindAllString(page, -1)
	if len(forms) != 2 {
		t.Fatalf("the served page drew %d restore forms, one per version, want 2:\n%s", len(forms), page)
	}
	for i, want := range []string{"9", "8"} {
		if drawn := regexp.MustCompile(`name="version"[^>]*value="([^"]*)"`).FindStringSubmatch(forms[i]); len(drawn) != 2 || drawn[1] != want {
			t.Errorf("form %d posts version %v, want %s", i+1, drawn, want)
		}
		if drawn := regexp.MustCompile(`name="expectedRevision"[^>]*value="([^"]*)"`).FindStringSubmatch(forms[i]); len(drawn) != 2 || drawn[1] != "14" {
			t.Errorf("form %d posts the write count %v, want the 14 this row carries", i+1, drawn)
		}
	}
	// One marking each: the word version 9 dropped and the word it added, against
	// version 8. A second marking would be the oldest version on the page compared
	// with a state that never arrived, and none at all is the panel showing a write
	// that moved nothing.
	for _, marked := range []string{"<del>milk </del>", "<ins>bread </ins>"} {
		if n := strings.Count(page, marked); n != 1 {
			t.Errorf("the served page draws %s %d times, want it once — the mark version 9 left against version 8, and nothing else:\n%s", marked, n, page)
		}
	}
}
