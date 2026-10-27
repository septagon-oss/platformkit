package view_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/entity/view"
)

// spelledInput is one record whose schema carries a single field of the type under read,
// so the page names one list and this case reads that one list.
func spelledInput(field entity.Field, stored any) view.Input {
	s := taskSchema()
	s.Fields = append(s.Fields, field)
	in := input(view.KindRecord, []map[string]any{
		{"id": "t1", "title": "n", field.Name: stored},
	}, nil)
	in.Resource.Schema = s
	in.Policy = taskPolicy(s, []present.CommandInput{commandInput("archive")}, nil, false)
	in.State.Zone = time.UTC
	return in
}

// TestAListFieldSpellsEachElementInTheKindItsTypeDeclares reads every element type the
// value domain spells, as the Page writes it: a boolean as a JSON boolean, an integer as
// a JSON number, a decimal as a canonical string, a string as itself — and the same four
// whether the row arrived as decoded JSON or as the Go carrier a handler holds. A Page
// declares an element kind once for the field and names no kind per element, so a phone
// that decodes []int64 on `"elem":"integer"` and a terminal that prints what it is given
// agree only while each element keeps its declared type's spelling: an integer that
// reaches the document as "3" is a value the first cannot read, and a boolean that
// reaches it as "true" is a whole string to the second.
func TestAListFieldSpellsEachElementInTheKindItsTypeDeclares(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		field  entity.Field
		stored any
		elem   view.ValueKind
		spells string
	}{
		{entity.Field{Name: "flags", Type: entity.TypeList, Elem: entity.TypeBool},
			[]any{true, false}, view.ValueBoolean, `[true,false]`},
		{entity.Field{Name: "counts", Type: entity.TypeList, Elem: entity.TypeInt},
			[]any{json.Number("3"), int64(4)}, view.ValueInteger, `[3,4]`},
		{entity.Field{Name: "rates", Type: entity.TypeList, Elem: entity.TypeFloat},
			[]any{json.Number("1.5")}, view.ValueDecimal, `["1.5"]`},
		{entity.Field{Name: "labels", Type: entity.TypeList, Elem: entity.TypeString},
			[]string{"a", "b"}, view.ValueString, `["a","b"]`},
	} {
		fields := recordCells(t, spelledInput(tc.field, tc.stored))
		got, ok := fields[tc.field.Name]
		if !ok {
			t.Fatalf("%s is not on the page, so this case would inspect nothing", tc.field.Name)
		}
		if got.ValueKind != view.ValueList || got.Elem != tc.elem {
			t.Errorf("%s of %T is kind=%s elem=%s, want a list of %s",
				tc.field.Name, tc.stored, got.ValueKind, got.Elem, tc.elem)
		}
		if spelling := string(bytes(t, got.Value)); spelling != tc.spells {
			t.Errorf("%s of %T spells its elements as %s, want %s: the spelling %s declares",
				tc.field.Name, tc.stored, spelling, tc.spells, tc.elem)
		}
	}
}
