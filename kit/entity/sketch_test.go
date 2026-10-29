package entity_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

func TestSketchRoundTripAndLimits(t *testing.T) {
	stroke := func(n int) entity.SketchStroke { return make(entity.SketchStroke, n) }
	strokes := make(entity.Sketch, 400)
	for i := range strokes {
		strokes[i] = stroke(50)
	}
	cases := []struct {
		name   string
		value  entity.Sketch
		reason string
	}{
		{"unset", nil, ""}, {"empty", entity.Sketch{}, ""},
		{"drawing", entity.Sketch{{{0, 0, 0}, {500, 250, 17}, {1000, 1000, 42}}, {{900, 100, 0}}}, ""},
		{"both caps", strokes, ""}, {"point cap", entity.Sketch{stroke(20000)}, ""},
		{"too many points", entity.Sketch{stroke(20001)}, "20,000"},
		{"too many strokes", append(strokes, stroke(1)), "400"},
		{"empty stroke", entity.Sketch{{}}, "at least one"},
		{"coordinate", entity.Sketch{{{1001, 0, 0}}}, "coordinates"},
		{"first time", entity.Sketch{{{0, 0, 1}}}, "times in order"},
		{"decreasing time", entity.Sketch{{{0, 0, 0}, {0, 0, 17}, {0, 0, 16}}}, "times in order"},
		{"exact timestamp", entity.Sketch{{{0, 0, 0}, {0, 0, 9007199254740991}}}, ""},
		{"large timestamp", entity.Sketch{{{0, 0, 0}, {0, 0, 9007199254740992}}}, "elapsed milliseconds"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.value.Validate()
			raw, marshalErr := json.Marshal(c.value)
			sqlValue, sqlErr := c.value.Value()
			tuples, tuplesErr := c.value.Strokes()
			if c.reason != "" {
				for _, got := range []error{err, marshalErr, sqlErr, tuplesErr} {
					if !errors.Is(got, entity.ErrInvalidSketch) || !strings.Contains(got.Error(), c.reason) {
						t.Fatalf("want %q: %v", c.reason, got)
					}
				}
				if tuples != nil {
					t.Fatal("invalid drawing returned partial strokes")
				}
				return
			}
			if err != nil || marshalErr != nil || sqlErr != nil || tuplesErr != nil {
				t.Fatalf("%v %v %v %v", err, marshalErr, sqlErr, tuplesErr)
			}
			if encoded, err := json.Marshal(tuples); err != nil || string(encoded) != string(raw) {
				t.Fatalf("strokes changed the drawing: %s, %v", encoded, err)
			}
			if len(tuples) > 0 {
				first := c.value[0][0]
				tuples[0][0][0] ^= 1
				if c.value[0][0] != first {
					t.Fatal("returned strokes alias the drawing")
				}
			}
			var decoded, scanned entity.Sketch
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if err := scanned.Scan(sqlValue); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(c.value, decoded) || !reflect.DeepEqual(c.value, scanned) {
				t.Fatal("round trip changed value")
			}
		})
	}
}

func TestSketchStrictJSONPreservesReceiver(t *testing.T) {
	for _, raw := range []string{`{}`, `[null]`, `[[]]`, `[[0,0,0]]`, `[[[1,2]]]`, `[[[1,2,0,4]]]`, `[[[null,2,0]]]`, `[[["1",2,0]]]`, `[[[true,2,0]]]`, `[[[1.0,2,0]]]`, `[[[1e0,2,0]]]`, `[[[1,2,0]]] []`, `[[[-1,2,0]]]`, `[[[1,2,-1]]]`} {
		t.Run(raw, func(t *testing.T) {
			saved := entity.Sketch{{{1, 2, 0}}}
			for _, decode := range []func() error{func() error { return saved.UnmarshalJSON([]byte(raw)) }, func() error { return saved.Scan(raw) }} {
				if err := decode(); !errors.Is(err, entity.ErrInvalidSketch) {
					t.Fatalf("accepted %s: %v", raw, err)
				}
				if !reflect.DeepEqual(saved, entity.Sketch{{{1, 2, 0}}}) {
					t.Fatal("refusal changed receiver")
				}
			}
		})
	}
	var s *entity.Sketch
	if err := s.Scan(nil); !errors.Is(err, entity.ErrInvalidSketch) {
		t.Fatal(err)
	}
}

func TestSketchReflection(t *testing.T) {
	type lookalike entity.Sketch
	type model struct {
		Drawing entity.Sketch
		Pointer *entity.Sketch
		Other   lookalike
		Nested  []entity.Sketch
	}
	fields := entity.FieldsOf(reflect.TypeFor[model]())
	if len(fields) != 2 {
		t.Fatalf("%+v", fields)
	}
	for _, f := range fields {
		if f.Type != entity.TypeSketch || f.Elem != "" {
			t.Fatalf("%+v", f)
		}
	}
}

func TestSketchRequiredFieldsIncludeNilEmbeddedValues(t *testing.T) {
	type drawing struct {
		Value *entity.Sketch `json:"value" validate:"required"`
	}
	type model struct{ *drawing }
	for _, value := range []any{model{}, model{&drawing{}}, model{&drawing{new(entity.Sketch{})}}} {
		if err := entity.ValidateSketchFields(value); !errors.Is(err, entity.ErrInvalidSketch) {
			t.Fatalf("required drawing: %v", err)
		}
	}
	if err := entity.ValidateSketchFields(model{&drawing{new(entity.Sketch{{{1, 2, 0}}})}}); err != nil {
		t.Fatal(err)
	}
}
