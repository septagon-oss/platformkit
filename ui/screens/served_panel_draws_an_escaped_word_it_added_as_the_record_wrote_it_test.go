package screens

// The panel compares a version against the one before it and marks the words that write
// added and the words it dropped, so a person who put an "&" or a "<" into a field meets that
// character inside a mark. What the reader is handed has been asked of a quoted value, and the
// question stops at the quotation: every case that reads a comparison takes its values from
// words the markup never has to draw, so nothing asks it of a marked run — and a marked run is
// drawn by its own branch, one for an insertion and one for a deletion. A word drawn raw
// arrives as markup rather than as what the person typed, and one drawn twice arrives as the
// letters of its own drawing.

import (
	"context"
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// TestTheServedPanelDrawsAnEscapedWordItAddedAsTheRecordWroteIt compares two bodies whose
// dropped and added words hold characters the page draws as entities, and reads the served
// line of the version that moved them. Every character either body holds has to reach the
// line drawn once, inside the mark that says what became of the word holding it, and the
// line read back through one unescaping has to be the body the version left standing.
func TestTheServedPanelDrawsAnEscapedWordItAddedAsTheRecordWroteIt(t *testing.T) {
	// The dropped word holds "<3>" and the added one holds "&": one mark each, one character
	// the page has to draw in each. Far shorter than one line of this panel, so nothing is cut.
	const before, after = "Tom & Jerry <3> hearts", "Tom & Jerry & co"
	line := comparedLine(t, before, after)

	// Each character the two bodies hold reaches the reader drawn once: the kept ones from the
	// body both versions hold, the dropped ones from the mark that says they were dropped.
	for _, drawn := range []struct{ entity, char, from string }{
		{"&amp;", "&", after}, {"&lt;", "<", before}, {"&gt;", ">", before},
	} {
		if got, want := strings.Count(line, drawn.entity), strings.Count(drawn.from, drawn.char); got != want {
			t.Errorf("the compared line holds %d drawn %q for the %d the %q body wrote: %q",
				got, drawn.entity, want, drawn.from, line)
		}
	}
	// The sign of a second drawing is the entity for an entity's own first letters.
	for _, twice := range []string{"&amp;amp;", "&amp;lt;", "&amp;gt;"} {
		if strings.Contains(line, twice) {
			t.Errorf("the compared line drew %s, so the page escaped what was already drawn: %q", twice, line)
		}
	}
	// The mark holds the drawn characters: raw text would place the record's own characters in
	// the markup, where the page would read them as elements.
	if dropped := markedRun(line, "del"); !strings.Contains(dropped, "&lt;3&gt;") {
		t.Errorf("the mark of a dropped word holds %q rather than the drawn form of what the body held: %q", dropped, line)
	}
	if added := markedRun(line, "ins"); !strings.Contains(added, "&amp; co") {
		t.Errorf("the mark of an added word holds %q rather than the drawn form of what the body wrote: %q", added, line)
	}
	if held := readBack(line); held != after {
		t.Errorf("the compared line reads back as %q of a body that held %q: the reader is handed letters the "+
			"record does not hold, or loses letters it does", held, after)
	}
}

// markedRun is what the panel put inside the element that marks a run as added or dropped,
// and the empty string where it marked no such run.
func markedRun(line, mark string) string {
	openTag, closeTag := "<"+mark+">", "</"+mark+">"
	start, end := strings.Index(line, openTag), strings.Index(line, closeTag)
	if start < 0 || end < start {
		return ""
	}
	return line[start+len(openTag) : end]
}

// droppedMark matches the mark of a word this write dropped together with the word inside it.
var droppedMark = regexp.MustCompile(`(?s)<del>.*?</del>`)

// readBack is what a reader takes from one panel line as the body it shows. The mark of a
// dropped word and the word it holds say what used to be there rather than what is, so both
// stand as a space; the marks of an added word hold no word of their own; and the drawn
// entities become back the characters the record holds.
func readBack(line string) string {
	kept := droppedMark.ReplaceAllString(line, " ")
	kept = strings.NewReplacer("<ins>", " ", "</ins>", " ").Replace(kept)
	return strings.Join(strings.Fields(html.UnescapeString(kept)), " ")
}

// comparedLine is what version 2 of a two-version series says about a field on the page that
// shows the record: the words the field is called by and the marked runs the comparison
// produced, with the markup that marks them left standing.
func comparedLine(t *testing.T, before, after string) string {
	t.Helper()
	const at = "/app/note/notes"
	document := func(body string) []byte {
		fields, err := json.Marshal(map[string]string{"body_copy": body})
		if err != nil {
			t.Fatalf("encode the document a version held: %v", err)
		}
		return fields
	}
	res := httpx.Resource{
		Module: "note", Entity: "note", Path: "/notes", Screen: at,
		Schema: entity.Schema{Module: "note", Entity: "note", Path: "/notes",
			Fields: []entity.Field{{Name: "body_copy", Type: entity.TypeString}}},
		History: &httpx.History{RestoreVerb: "restore",
			Versions: func(context.Context, uuid.UUID) ([]httpx.Version, error) {
				return []httpx.Version{
					{Version: 2, At: time.Unix(1800000000, 0).UTC(), Changed: []string{"body_copy"},
						Fields: document(after)},
					{Version: 1, At: time.Unix(1700000000, 0).UTC(), Changed: []string{"body_copy"},
						Fields: document(before)},
				}, nil
			}},
	}
	id := uuid.MustParse("77777777-7777-7777-7777-777777777777")
	row := map[string]any{"id": id.String(), "body_copy": after, entity.RevisionField: float64(2)}
	page, err := detailView(res, context.Background(),
		Options{Workspace: "/app", Home: "Workspace"}, at, row, true)
	if err != nil {
		t.Fatalf("the record page of a resource with two versions would not render: %v", err)
	}
	var b strings.Builder
	for _, n := range page.Body {
		if err := n.Render(&b); err != nil {
			t.Fatalf("render the record page: %v", err)
		}
	}
	// The later version's own mark first, then the field's words inside it: the line is found
	// by which version it belongs to, never by how far down the page it landed.
	source, label := b.String(), "Body copy: "
	lineAt := strings.Index(source, ">v2 · ")
	if lineAt < 0 {
		t.Fatalf("the served page drew no line for the version it was shown:\n%s", source)
	}
	entry := source[lineAt:]
	nameAt := strings.Index(entry, label)
	if nameAt < 0 {
		t.Fatalf("the version's line names no field in the words a reader is given:\n%s", entry)
	}
	line := entry[nameAt+len(label):]
	if end := strings.Index(line, "</p>"); end >= 0 {
		line = line[:end]
	}
	return line
}
