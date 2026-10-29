package screens

// Which two versions meet in a version's own line is decided here, in ui/screens,
// and no other case reads it off the served page: the cases beside the renderer hand
// it rows they wrote themselves, and the one panel case above it reads the door, the
// verb and the write count, not the comparison. Two versions of one document cannot
// tell "each against the one before it" from "each against the oldest", so this asks
// for a series of three, each leaving a different word, and reads every mark off the
// row it belongs to. A version marked against the state two writes back shows the
// reader words that moved in an earlier write, and a page count of the marks cannot
// see that: only the row they sit under can.

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
)

func TestTheServedPageMarksEachVersionAgainstTheVersionBeforeIt(t *testing.T) {
	const at = "/app/note/notes"
	id := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	versioned := httpx.Resource{
		Module: "note", Entity: "note", Path: "/notes", Screen: at,
		Schema: entity.Schema{Module: "note", Entity: "note", Path: "/notes",
			Fields: []entity.Field{{Name: "title", Type: entity.TypeString}}},
		ReadBy:   httpx.SignedIn(),
		Commands: []httpx.Command{panelRestoreCommand("restore")},
		History: &httpx.History{RestoreVerb: "restore",
			Versions: func(context.Context, uuid.UUID) ([]httpx.Version, error) {
				return []httpx.Version{
					{Version: 10, At: time.Unix(1800000000, 0).UTC(), Changed: []string{"title"},
						Fields: json.RawMessage(`{"title":"Buy bread today"}`)},
					{Version: 9, At: time.Unix(1700000000, 0).UTC(), Changed: []string{"title"},
						Fields: json.RawMessage(`{"title":"Buy milk today"}`)},
					{Version: 8, At: time.Unix(1600000000, 0).UTC(), Changed: []string{"title"},
						Fields: json.RawMessage(`{"title":"Sell milk today"}`)},
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

	// One row per version, cut from the line that says which version it is to that
	// row's own form, so a mark can be asked where it sits rather than only whether
	// the page holds it.
	anchors := regexp.MustCompile(`<p[^>]*>v(\d+) · `).FindAllStringSubmatchIndex(page, -1)
	if len(anchors) != 3 {
		t.Fatalf("the served page drew %d version rows, want one per version:\n%s", len(anchors), page)
	}
	byVersion := map[string]string{}
	for _, at := range anchors {
		from, row := at[0], strings.Index(page[at[0]:], "</form>")
		if row < 0 {
			t.Fatalf("the row of version %s holds no restore form:\n%s", page[at[2]:at[3]], page[at[0]:])
		}
		byVersion[page[at[2]:at[3]]] = page[from : at[0]+row+len("</form>")]
	}
	// Each version's line holds the words it moved against the version before it, and
	// nothing of what an earlier version moved: version 10 dropped "milk" for "bread",
	// version 9 dropped "Sell" for "Buy", and version 8 arrived with no earlier state.
	for _, want := range []struct{ version, moved, older string }{
		{"10", "<ins>bread </ins>", "<del>Sell </del>"},
		{"9", "<ins>Buy </ins>", "<del>milk </del>"},
	} {
		drawn := byVersion[want.version]
		if !strings.Contains(drawn, want.moved) {
			t.Errorf("version %s's row does not mark %s, the word this version moved:\n%s", want.version, want.moved, drawn)
		}
		if strings.Contains(drawn, want.older) {
			t.Errorf("version %s's row marks %s, which an earlier version moved, not this one:\n%s", want.version, want.older, drawn)
		}
	}
	if oldest := byVersion["8"]; strings.Contains(oldest, "<del") || strings.Contains(oldest, "<ins") {
		t.Errorf("the oldest version on the page is marked against a state that never arrived:\n%s", oldest)
	}
}
