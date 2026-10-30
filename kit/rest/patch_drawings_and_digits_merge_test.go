package rest

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// mergedDrawingRow holds a nullable drawing beside an exact count, which is how a
// product asks for a board that may hold nothing and money in minor units.
type mergedDrawingRow struct {
	crud.Base
	Drawing *entity.Sketch `json:"drawing" required:"false" gorm:"type:jsonb" ui:"label:Drawing"`
	Cents   int64          `json:"cents"`
	Counts  []int64        `json:"counts"`
}

func (mergedDrawingRow) TableName() string { return "rest_merged_drawings" }

// What a patch body writes is decided twice: field by field as it is decoded, and
// field by field as it is merged into the row. A body that carries both halves —
// one drawing and one number past what a float64 writes exactly — has to reach the
// row with the strokes it sent, at the milliseconds it sent, and with the number it
// sent, whatever order the two fields were written in, since the body decodes into a
// Go map. A null drawing has to stay unset next to that number too: an empty board
// and no board are different answers, and a row that cannot tell them apart cannot
// be written back without inventing strokes.
func TestAMergedPatchBodyWritesItsDrawingAndItsDigitsIntoTheRow(t *testing.T) {
	const drawing = `[[[0,0,0],[500,250,17],[1000,1000,9007199254740991]]]`
	want := entity.Sketch{{{0, 0, 0}, {500, 250, 17}, {1000, 1000, 9007199254740991}}}
	for _, body := range []string{
		`{"drawing":` + drawing + `,"cents":9007199254740993}`,
		`{"cents":9007199254740993,"drawing":` + drawing + `}`,
	} {
		var values patchValues[mergedDrawingRow]
		if err := json.Unmarshal([]byte(body), &values); err != nil {
			t.Fatalf("body refused: %v", err)
		}
		row := &mergedDrawingRow{}
		columns, err := merge(row, crud.Fields[*mergedDrawingRow](), nil, values)
		if err != nil {
			t.Fatalf("body %s refused at the merge: %v", body, err)
		}
		got := append([]string(nil), columns...)
		slices.Sort(got)
		if !slices.Equal(got, []string{"cents", "drawing"}) {
			t.Fatalf("changed columns = %v, want the two the body named", columns)
		}
		if row.Drawing == nil {
			t.Fatalf("drawing stayed unset over %s", body)
		}
		if !reflect.DeepEqual(*row.Drawing, want) {
			t.Fatalf("drawing = %#v, want the strokes and milliseconds the body wrote", *row.Drawing)
		}
		if row.Cents != 9007199254740993 {
			t.Fatalf("cents = %d, want the exact digits 9007199254740993 the body wrote", row.Cents)
		}
	}

	// The digits a list of counts carries are the ones the body wrote too: a
	// number below the top of the field is the same number as one beside it, and
	// a list is the one nested shape a declared record field can take.
	const list = `[9007199254740995,9007199254740993,9223372036854775807]`
	for _, body := range []string{`{"drawing":` + drawing + `,"counts":` + list + `}`, `{"counts":` + list + `,"drawing":` + drawing + `}`} {
		var values patchValues[mergedDrawingRow]
		if err := json.Unmarshal([]byte(body), &values); err != nil {
			t.Fatalf("body refused: %v", err)
		}
		row := &mergedDrawingRow{}
		if _, err := merge(row, crud.Fields[*mergedDrawingRow](), nil, values); err != nil {
			t.Fatalf("body %s refused at the merge: %v", body, err)
		}
		wantCounts := []int64{9007199254740995, 9007199254740993, 9223372036854775807}
		if !slices.Equal(row.Counts, wantCounts) {
			t.Fatalf("counts = %v, want the exact digits %v the body wrote", row.Counts, wantCounts)
		}
		if row.Drawing == nil || !reflect.DeepEqual(*row.Drawing, want) {
			t.Fatalf("drawing = %#v, want the strokes and milliseconds the body wrote", row.Drawing)
		}
	}

	// A null drawing beside a number stays unset rather than becoming an empty
	// board, in either order, and the number keeps its digits either way.
	for _, body := range []string{`{"drawing":null,"cents":9007199254740995}`, `{"cents":9007199254740995,"drawing":null}`} {
		var values patchValues[mergedDrawingRow]
		if err := json.Unmarshal([]byte(body), &values); err != nil {
			t.Fatalf("body refused: %v", err)
		}
		row := &mergedDrawingRow{Drawing: &entity.Sketch{}}
		if _, err := merge(row, crud.Fields[*mergedDrawingRow](), nil, values); err != nil {
			t.Fatalf("body %s refused at the merge: %v", body, err)
		}
		if row.Drawing != nil {
			t.Fatalf("drawing = %#v over %s, want unset: null is not an empty board", *row.Drawing, body)
		}
		if row.Cents != 9007199254740995 {
			t.Fatalf("cents = %d, want the exact digits 9007199254740995 the body wrote", row.Cents)
		}
	}
}
