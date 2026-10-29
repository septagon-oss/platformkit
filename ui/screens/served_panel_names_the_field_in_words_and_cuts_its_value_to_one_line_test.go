package screens

// The panel beside a record is a list of one-line entries, and each line is a
// sentence in the reader's language rather than a dump of the record: the field is
// named in the words the kernel makes of its stored name, and the value it held is
// cut to the panel's line. Neither reads off a served page today: the cases over
// this panel ask which two versions meet and what the marks mean, and the cut lives
// in the adapter that composes the panel, so a panel that grew to the whole document
// or named a field by what the column is called would still answer every one of them.
// The line matters most for the version no earlier version stands beside — the one
// shown as one unmarked stretch of everything it held — so this asks it there, over a
// series whose middle version never arrived.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// longBody is a value longer than any line the panel draws: the whole document of a
// version, which the versions route answers and the panel only quotes.
var longBody = strings.Repeat("a full paragraph of the record's own body ", 12)

func TestTheServedPanelNamesTheFieldInWordsAndCutsItsValueToOneLine(t *testing.T) {
	const at = "/app/note/notes"
	id := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	body := func(text string) json.RawMessage {
		out, err := json.Marshal(map[string]string{"body_copy": text})
		if err != nil {
			t.Fatalf("encode the document a version held: %v", err)
		}
		return out
	}
	versioned := httpx.Resource{
		Module: "note", Entity: "note", Path: "/notes", Screen: at,
		Schema: entity.Schema{Module: "note", Entity: "note", Path: "/notes",
			Fields: []entity.Field{{Name: "body_copy", Type: entity.TypeString}}},
		History: &httpx.History{RestoreVerb: "restore",
			Versions: func(context.Context, uuid.UUID) ([]httpx.Version, error) {
				// Version 4 never arrived on this page, so version 5 is shown as one
				// stretch of what it holds rather than against anything.
				return []httpx.Version{
					{Version: 5, At: time.Unix(1800000000, 0).UTC(), Changed: []string{"body_copy"},
						Fields: body(longBody + "and the words this version ended with")},
					{Version: 3, At: time.Unix(1600000000, 0).UTC(), Changed: []string{"body_copy"},
						Fields: body("An earlier paragraph")},
				}, nil
			}},
	}
	row := map[string]any{"id": id.String(), "body_copy": longBody, entity.RevisionField: float64(9)}

	served, err := detailView(versioned, context.Background(),
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

	// Version 5's own line: from where it names the version to the end of that entry,
	// so what a name, a mark or a length is said about is that version's entry alone,
	// whichever way round the panel drew the two of them.
	start := strings.Index(page, ">v5 · ")
	if start < 0 {
		t.Fatalf("the served page drew no line for version 5:\n%s", page)
	}
	entry := page[start:]
	if end := strings.Index(entry, "</div><div>"); end >= 0 {
		entry = entry[:end]
	} else if end = strings.Index(entry, "</details>"); end >= 0 {
		entry = entry[:end]
	}

	// The one sentence this version's entry holds about its field: the words the field
	// is called by, and the stretch of value the panel quotes for it.
	line := entry
	if i := strings.Index(entry, "Body copy"); i >= 0 {
		if end := strings.Index(entry[i:], "</p>"); end >= 0 {
			line = entry[i : i+end]
		}
	}
	if !strings.Contains(line, "Body copy") {
		t.Errorf("version 5's line names its field by none of the words a reader is given: %q", line)
	}
	if strings.Contains(line, "body_copy") {
		t.Errorf("version 5's line prints the name the column answers to instead of the words made from it: %q", line)
	}
	// The line is cut, so a whole document never lands beside the record: the panel
	// quotes the beginning and says the rest is longer.
	if !strings.Contains(line, "…") {
		t.Errorf("version 5's line carries none of the sign that its value goes past what the panel quotes:\n%s", line)
	}
	if strings.Contains(line, "and the words this version ended with") {
		t.Errorf("version 5's line carries the end of a document a whole document long, so the panel draws what the versions route answers:\n%s", line)
	}
	if runes := len([]rune(line)); runes > 240 {
		t.Errorf("version 5's line quotes %d runes of a value longer than any line the panel draws: %q", runes, line)
	}
}
