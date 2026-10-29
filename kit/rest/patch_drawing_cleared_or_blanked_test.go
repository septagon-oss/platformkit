package rest

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// clearedDrawingRow is a nullable drawing beside a title, which is how a product
// asks for a board that may hold strokes, hold nothing drawn, or hold nothing at
// all. The three answers are written to one jsonb column and have to stay apart.
type clearedDrawingRow struct {
	crud.Base
	Title   string         `json:"title"`
	Drawing *entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
}

func (clearedDrawingRow) TableName() string { return "rest_cleared_drawings" }

// mergeOverHeld merges one patch body into a row that already holds one stroke and
// hands back the row with the columns the merge says it changed, which is the list
// the update writes: those columns and no others.
func mergeOverHeld(t *testing.T, body string) (*clearedDrawingRow, []string) {
	t.Helper()
	var values patchValues[clearedDrawingRow]
	if err := json.Unmarshal([]byte(body), &values); err != nil {
		t.Fatalf("body refused: %v", err)
	}
	held := entity.Sketch{{{10, 20, 30}, {40, 50, 60}}}
	row := &clearedDrawingRow{Title: "Held", Drawing: &held}
	columns, err := merge(row, crud.Fields[*clearedDrawingRow](), nil, values)
	if err != nil {
		t.Fatalf("body %s refused at the merge: %v", body, err)
	}
	return row, append([]string(nil), columns...)
}

// TestAClearedDrawingNamesTheColumnTheWriteUses asks what the difference between
// SQL NULL and an empty board is worth at the write door. A nil pointer that names
// no column would leave the strokes in the row while the answer said there was no
// board, and the next read would disagree with the one before it.
func TestAClearedDrawingNamesTheColumnTheWriteUses(t *testing.T) {
	row, columns := mergeOverHeld(t, `{"drawing":null}`)
	if !slices.Equal(columns, []string{"drawing"}) {
		t.Fatalf("changed columns = %v, want the drawing alone: a clearing that names no column leaves the old strokes in the row", columns)
	}
	if row.Drawing != nil {
		t.Fatalf("drawing = %#v, want unset: null is not an empty board", *row.Drawing)
	}
	if row.Title != "Held" {
		t.Fatalf("title = %q, want the untouched %q", row.Title, "Held")
	}
	// The answer the write carries is the column value: an unset drawing is SQL NULL,
	// which is what tells this row apart from one whose board was blanked.
	value, err := entity.Sketch(nil).Value()
	if err != nil || value != nil {
		t.Fatalf("unset drawing writes %v (%v), want SQL NULL", value, err)
	}
}

// TestABlankedDrawingNamesTheColumnAndWritesAnEmptyBoard asks the other half: a
// body that draws nothing has to reach the row as a board with no strokes on it,
// not as no board, because a caller who clears the canvas and saves expects the
// frame back, and a row that stores NULL for both cannot answer that.
func TestABlankedDrawingNamesTheColumnAndWritesAnEmptyBoard(t *testing.T) {
	row, columns := mergeOverHeld(t, `{"drawing":[]}`)
	if !slices.Equal(columns, []string{"drawing"}) {
		t.Fatalf("changed columns = %v, want the drawing alone", columns)
	}
	if row.Drawing == nil {
		t.Fatal("drawing stayed unset: an empty board is not the same answer as no board")
	}
	if len(*row.Drawing) != 0 {
		t.Fatalf("drawing holds %d strokes, want a board with none on it", len(*row.Drawing))
	}
	value, err := row.Drawing.Value()
	raw, ok := value.([]byte)
	if err != nil || !ok || string(raw) != "[]" {
		t.Fatalf("empty board writes %v (%v), want the JSON of a board with nothing on it", value, err)
	}
}

// TestABodyThatNamesNoDrawingKeepsTheStrokesTheRowHeld is the write that finds
// none: a body that updates another field leaves the board alone, in the row and in
// the columns the write is given, so a rename cannot quietly redraw anybody.
func TestABodyThatNamesNoDrawingKeepsTheStrokesTheRowHeld(t *testing.T) {
	row, columns := mergeOverHeld(t, `{"title":"Renamed"}`)
	if !slices.Equal(columns, []string{"title"}) {
		t.Fatalf("changed columns = %v, want the title alone: a body that asks for no drawing writes none", columns)
	}
	want := entity.Sketch{{{10, 20, 30}, {40, 50, 60}}}
	if row.Drawing == nil || !reflect.DeepEqual(*row.Drawing, want) {
		t.Fatalf("drawing = %#v, want the strokes the row held", row.Drawing)
	}
}
