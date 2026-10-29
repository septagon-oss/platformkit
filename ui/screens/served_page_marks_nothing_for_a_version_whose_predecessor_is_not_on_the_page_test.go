package screens

// One of the panel's two comparisons is "none": a version whose predecessor is not
// on this page is shown as one unchanged stretch, because retention releases old
// versions and the beginning of a series holds nothing earlier, and both read the
// same to whoever is looking. No case here reads that off a served page, because
// every fixture beside that rule numbers its versions in step, and a stepped series
// cannot tell "the version whose number is one lower" from "the row next in the
// list": both meet the same two documents and print the same page. A series with a
// hole can, so this asks the view Mount renders for versions 10, 9 and 7, where 8
// never arrived. Version 9's own row must show its words with no mark at all, and
// must show nothing of the words only version 7 held — a panel that compared a
// version with the row sitting next to it would put those words, marked, under a
// version that never moved them, which is the mistake this can see and no stepped
// fixture can.

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

func TestTheServedPageMarksNothingForAVersionWhosePredecessorIsNotOnThePage(t *testing.T) {
	const at = "/app/note/notes"
	id := uuid.MustParse("33333333-3333-3333-3333-333333333333")
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
					// Version 8 is not on the page: released by retention, or the series
					// simply began at 7. Either way nothing stands before version 9.
					{Version: 7, At: time.Unix(1600000000, 0).UTC(), Changed: []string{"title"},
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
	// row's own form, so a mark can be asked where it sits.
	anchors := regexp.MustCompile(`<p[^>]*>v(\d+) · `).FindAllStringSubmatchIndex(page, -1)
	if len(anchors) != 3 {
		t.Fatalf("the served page drew %d version rows, want one per version:\n%s", len(anchors), page)
	}
	byVersion := map[string]string{}
	for _, at := range anchors {
		from, end := at[0], strings.Index(page[at[0]:], "</form>")
		if end < 0 {
			t.Fatalf("the row of version %s holds no restore form:\n%s", page[at[2]:at[3]], page[at[0]:])
		}
		byVersion[page[at[2]:at[3]]] = page[from : at[0]+end+len("</form>")]
	}
	// The version whose predecessor stands on the page is marked against it; the two
	// whose predecessors never arrived are shown as they read, unmarked.
	if row := byVersion["10"]; !strings.Contains(row, "<del>milk </del>") || !strings.Contains(row, "<ins>bread </ins>") {
		t.Errorf("version 10's row does not compare itself with version 9, which is on this page:\n%s", row)
	}
	for _, number := range []string{"9", "7"} {
		if row := byVersion[number]; strings.Contains(row, "<del") || strings.Contains(row, "<ins") {
			t.Errorf("version %s's row is marked against a version this page never received, so it compares a state that is not there:\n%s", number, row)
		}
	}
	if row := byVersion["9"]; strings.Contains(row, "Sell") {
		t.Errorf("version 9's row holds %q, the words only version 7 ever held: this row was compared with the row next to it in the list, not with the version before it:\n%s", "Sell", row)
	}
	if row := byVersion["9"]; !strings.Contains(row, "Buy milk today") {
		t.Errorf("version 9's row does not show the words it holds, unmarked:\n%s", row)
	}
}
