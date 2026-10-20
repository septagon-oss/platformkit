package rest

import (
	"reflect"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
)

func TestCommandDefaultsRefuseUndecodableTypedDeclarations(t *testing.T) {
	type small struct {
		Count int8 `json:"count" default:"128"`
	}
	type invalidUUID struct {
		ID string `json:"id" default:"x"`
	}
	type wrongList struct {
		Values []int8 `json:"values" default:"[128]"`
	}
	type enum struct {
		State string `json:"state" enum:"open,done" default:"other"`
	}
	type malformed struct {
		Values []string `json:"values" default:"["`
	}
	checkDefaultFailure[small](t)
	checkDefaultFailure[wrongList](t)
	checkDefaultFailure[enum](t)
	checkDefaultFailure[malformed](t)
	fields := entity.FieldsOf(reflect.TypeFor[invalidUUID]())
	fields[0].Type = entity.TypeUUID
	if _, _, err := commandDefaults[invalidUUID](fields); err == nil {
		t.Fatal("invalid UUID default accepted")
	}
}

func checkDefaultFailure[I any](t *testing.T) {
	t.Helper()
	if _, _, err := commandDefaults[I](entity.FieldsOf(reflect.TypeFor[I]())); err == nil {
		t.Fatalf("invalid default for %T accepted", *new(I))
	}
}

func TestCommandDefaultValuesAreIndependentAndSuppliedZerosWin(t *testing.T) {
	type input struct {
		Name    string  `json:"name" default:"pending"`
		Enabled bool    `json:"enabled" default:"true"`
		Count   int     `json:"count" default:"5"`
		Values  []int64 `json:"values" default:"[9007199254740993]"`
	}
	prepare, fault, err := commandDefaults[input](entity.FieldsOf(reflect.TypeFor[input]()))
	if err != nil || fault != "" {
		t.Fatalf("%s %v", fault, err)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			in, err := prepare(nil)
			if err != nil || in.Name != "pending" || len(in.Values) != 1 || in.Values[0] != 9007199254740993 {
				t.Errorf("defaults: %+v %v", in, err)
				return
			}
			in.Values[0] = 0
		})
	}
	wg.Wait()
	got, err := prepare(map[string]any{"name": "", "enabled": false, "count": 0, "values": nil})
	if err != nil || got.Name != "" || got.Enabled || got.Count != 0 || got.Values != nil {
		t.Fatalf("supplied zeros: %+v %v", got, err)
	}
}

func TestCommandInputAuditIncludesNamedEmbeddedAndUnsupportedFields(t *testing.T) {
	type Embedded struct {
		Why string `json:"why"`
	}
	type named struct {
		Embedded `json:"details"`
	}
	type hidden struct {
		Secret  struct{} `json:"-"`
		private string
	}
	type pointer struct {
		*Embedded `json:",omitempty"`
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[named]()} {
		if bad := inputSchemaFault(typ, entity.FieldsOf(typ)); bad == "" {
			t.Fatalf("unsupported wire field in %s escaped the audit", typ)
		}
	}
	if bad := inputSchemaFault(reflect.TypeFor[pointer](), entity.FieldsOf(reflect.TypeFor[pointer]())); bad != "" {
		t.Fatal(bad)
	}
	if bad := inputSchemaFault(reflect.TypeFor[hidden](), nil); bad != "" {
		t.Fatal(bad)
	}
}

func TestDefaultedEmbeddedInputsArePreparedWithJSONOptions(t *testing.T) {
	type Embedded struct {
		Count int64 `json:"count" default:"7"`
	}
	type input struct {
		*Embedded `json:",omitempty"`
	}
	prepare, fault, err := commandDefaults[input](entity.FieldsOf(reflect.TypeFor[input]()))
	if err != nil || fault != "" {
		t.Fatalf("%s %v", fault, err)
	}
	got, err := prepare(nil)
	if err != nil || got.Embedded == nil || got.Count != 7 {
		t.Fatalf("%+v %v", got, err)
	}
	got.Count = 9
	next, err := prepare(nil)
	if err != nil || next.Count != 7 {
		t.Fatalf("%+v %v", next, err)
	}
}
