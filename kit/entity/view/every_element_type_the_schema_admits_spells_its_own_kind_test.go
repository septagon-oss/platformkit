package view_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// listAdmits is what a list may hold: kit/entity refuses a Field whose Elem is outside
// this set, so these seven element types are the whole domain a shell decodes a list of.
var listAdmits = []struct {
	elem   entity.FieldType
	kind   view.ValueKind
	stored any
	spells string
}{
	{entity.TypeString, view.ValueString, "a", `["a"]`},
	{entity.TypeText, view.ValueString, "long text", `["long text"]`},
	{entity.TypeUUID, view.ValueString, "9f1b1f0c-2f6a-4d6d-9f2f-6f1a2b3c4d5e", `["9f1b1f0c-2f6a-4d6d-9f2f-6f1a2b3c4d5e"]`},
	{entity.TypeBool, view.ValueBoolean, true, `[true]`},
	{entity.TypeInt, view.ValueInteger, json.Number("3"), `[3]`},
	{entity.TypeFloat, view.ValueDecimal, json.Number("1.50"), `["1.5"]`},
	// The instant element carries the fixed layout the domain fixes, so a shell that
	// decodes it needs no zone and no guess about a shorter spelling.
	{entity.TypeTime, view.ValueInstant, "2026-07-01T14:12:00.000000Z", `["2026-07-01T14:12:00.000000Z"]`},
}

// TestEveryElementTypeTheSchemaAdmitsSpellsItsOwnKind reads the `elem` and the `value`
// of a list of each element type a schema may declare. A Page names one kind for the
// whole field and none per element, so `elem` is all a phone or a terminal has to decode
// by: a list whose declared type spells one kind and whose elements arrive as another is
// read wrongly by whichever client trusts it, and the trust is what ValueKind exists for.
func TestEveryElementTypeTheSchemaAdmitsSpellsItsOwnKind(t *testing.T) {
	t.Parallel()

	for _, tc := range listAdmits {
		name := "values"
		s := taskSchema()
		s.Fields = append(s.Fields, entity.Field{Name: name, Type: entity.TypeList, Elem: tc.elem})
		in := input(view.KindRecord, []map[string]any{{"id": "t1", "title": "n", name: []any{tc.stored}}}, nil)
		in.Resource.Schema = s
		in.Policy = taskPolicy(s, []present.CommandInput{commandInput("archive")}, nil, false)
		in.State.Zone = time.UTC

		got, ok := recordCells(t, in)[name]
		if !ok {
			t.Fatalf("a list of %s is not on the page, so this case would inspect nothing", tc.elem)
		}
		if got.ValueKind != view.ValueList || got.Elem != tc.kind {
			t.Errorf("a list of %s is kind=%s elem=%s, want a list of %s",
				tc.elem, got.ValueKind, got.Elem, tc.kind)
		}
		if spelling := string(bytes(t, got.Value)); spelling != tc.spells {
			t.Errorf("a list of %s spells its element as %s, want %s: %s is what elem=%q promises",
				tc.elem, spelling, tc.spells, tc.elem, tc.kind)
		}
	}
}
