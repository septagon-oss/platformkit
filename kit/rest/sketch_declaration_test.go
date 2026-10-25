package rest

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

func TestSketchDeclarationsRejectScalarHints(t *testing.T) {
	for _, tag := range []string{`json:"drawing,omitempty"`, `json:"drawing,omitzero"`, `json:"drawing" default:""`, `json:"drawing" maxItems:"1"`, `json:"drawing" validate:"min=1"`, `json:"drawing" ui:"widget:textarea"`, `json:"drawing" ui:"present:badge"`} {
		typ := reflect.StructOf([]reflect.StructField{{Name: "Drawing", Type: reflect.TypeFor[entity.Sketch](), Tag: reflect.StructTag(tag)}})
		if sketchDeclarationFault(typ, entity.FieldsOf(typ)) == "" {
			t.Fatal("accepted", tag)
		}
	}
	for _, f := range []entity.Field{
		{Name: "drawing", Type: entity.TypeString, Widget: "sketch"},
		{Name: "drawing", Type: entity.TypeSketch, Default: "[]"},
		{Name: "drawing", Type: entity.TypeSketch, Display: true},
	} {
		for _, r := range []httpx.Resource{{Schema: entity.Schema{Fields: []entity.Field{f}}}, {Commands: []httpx.Command{{Fields: []entity.Field{f}}}}} {
			if httpx.CheckRowHints([]httpx.Resource{r}) == "" {
				t.Fatal("manual schema accepted", f)
			}
		}
	}
}

func TestSketchPatchKeepsOrdinaryNumberHandling(t *testing.T) {
	type input struct {
		Count   int           `json:"count"`
		Drawing entity.Sketch `json:"drawing"`
	}
	var values patchValues[input]
	if err := json.Unmarshal([]byte(`{"count":1.0,"drawing":[[[1,2,0]]]}`), &values); err != nil {
		t.Fatal(err)
	}
	if count, ok := values["count"].(json.Number); !ok || count.String() != "1.0" {
		t.Fatalf("count = %#v, want the exact token 1.0 the body wrote", values["count"])
	}
	if _, ok := values["drawing"].(entity.Sketch); !ok {
		t.Fatal(values)
	}
	if err := json.Unmarshal([]byte(`{"drawing":[[[1.0,2,0]]]}`), &values); err == nil {
		t.Fatal("fractional token accepted")
	}
}
