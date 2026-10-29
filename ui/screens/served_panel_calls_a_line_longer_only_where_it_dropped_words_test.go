package screens

// The panel says in one sign whether a value went past the line it quotes, so the
// sign has to answer for itself: it belongs on a line only where words were dropped,
// and the words it did keep must arrive whole. A case over a long value shows the
// sign present; nothing showed that a value the panel quoted in full is left without
// it, nor that quoting a value is stable — that a value already the length of one
// quoted line is not itself treated as longer. A reader told the rest is missing when
// nothing is missing, or handed a quotation that loses its own ending on the second
// reading, is being told something about the record that the record does not say.

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

// quotedLine is the one line version 1 of a single-version series says about the
// given value: the words the field is called by and the stretch of value the panel
// quotes, with none of the markup around them. The version stands alone, so the line
// is a quotation rather than a comparison and holds no inserted or removed run.
func quotedLine(t *testing.T, value string) string {
	t.Helper()
	const at = "/app/note/notes"
	id := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	fields, err := json.Marshal(map[string]string{"body_copy": value})
	if err != nil {
		t.Fatalf("encode the document a version held: %v", err)
	}
	res := httpx.Resource{
		Module: "note", Entity: "note", Path: "/notes", Screen: at,
		Schema: entity.Schema{Module: "note", Entity: "note", Path: "/notes",
			Fields: []entity.Field{{Name: "body_copy", Type: entity.TypeString}}},
		History: &httpx.History{RestoreVerb: "restore",
			Versions: func(context.Context, uuid.UUID) ([]httpx.Version, error) {
				return []httpx.Version{{Version: 1, At: time.Unix(1700000000, 0).UTC(),
					Changed: []string{"body_copy"}, Fields: fields}}, nil
			}},
	}
	row := map[string]any{"id": id.String(), "body_copy": value, entity.RevisionField: float64(1)}
	page, err := detailView(res, context.Background(),
		Options{Workspace: "/app", Home: "Workspace"}, at, row, true)
	if err != nil {
		t.Fatalf("the record page of a resource with one version would not render: %v", err)
	}
	var b strings.Builder
	for _, n := range page.Body {
		if err := n.Render(&b); err != nil {
			t.Fatalf("render the record page: %v", err)
		}
	}
	// The version's own mark first, then the field's words inside it: the line is
	// found by which version it belongs to, never by how far down the page it landed.
	const mark, label = ">v1 · ", "Body copy: "
	source := b.String()
	markAt := strings.Index(source, mark)
	if markAt < 0 {
		t.Fatalf("the served page drew no line for the version it was shown:\n%s", source)
	}
	entry := source[markAt:]
	start := strings.Index(entry, label)
	if start < 0 {
		t.Fatalf("the version's line names no field in the words a reader is given:\n%s", entry)
	}
	line := entry[start+len(label):]
	if end := strings.Index(line, "</p>"); end >= 0 {
		line = line[:end]
	}
	return line
}

func TestTheServedPanelCallsALineLongerOnlyWhereItDroppedWords(t *testing.T) {
	// Long enough that no line of this panel holds it, and free of anything the
	// markup would escape, so what the page shows is the value it was given.
	long := strings.Repeat("alpha bravo charlie delta ", 12)
	quoted := quotedLine(t, long)
	if !strings.HasSuffix(quoted, "…") {
		t.Fatalf("a value %d runes long was quoted with no sign that it goes past the line: %q", len([]rune(long)), quoted)
	}
	kept := strings.TrimSuffix(quoted, "…")

	// The same words a second time, this time as the whole of what the version held.
	// Nothing was dropped to get here, so the panel owes this line no sign, and every
	// word it quoted the first time has to be there, ending where it ended.
	again := quotedLine(t, kept)
	if again != kept {
		t.Errorf("a value no longer than one quoted line was not quoted whole: first %q, second %q", kept, again)
	}
	if strings.Contains(again, "…") {
		t.Errorf("the panel says a value of %d runes goes past what it quotes, when it quoted every rune of it: %q", len([]rune(kept)), again)
	}
}
