package entity_test

import (
	"encoding/json"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/entity"
)

type record struct {
	entity.Base
	Title    string      `json:"title,omitempty" validate:"required" ui:"widget:text" gorm:"column:display_title"`
	State    string      `json:"state" enum:"open,done" default:"open" doc:"Lifecycle state" ui:"widget:select;hide:list"`
	Notes    string      `json:"notes" gorm:"type:text" ui:"widget:textarea,hide:list"`
	DueAt    *time.Time  `json:"dueAt"`
	Watchers []uuid.UUID `json:"watchers"`
	Hidden   string      `json:"-"`
	Blob     []byte
	Object   map[string]string
}

func (record) TableName() string { return "records" }

func TestFieldsPreserveTheStructContract(t *testing.T) {
	want := []entity.Field{
		{Name: "id", Column: "id", Type: entity.TypeUUID, ReadOnly: true, Index: []int{0, 0}},
		{Name: "createdAt", Column: "created_at", Type: entity.TypeTime, ReadOnly: true, Index: []int{0, 2}},
		{Name: "updatedAt", Column: "updated_at", Type: entity.TypeTime, ReadOnly: true, Index: []int{0, 3}},
		{Name: "title", Column: "display_title", Type: entity.TypeString, Required: true, Widget: "text", Index: []int{1}},
		{Name: "state", Column: "state", Type: entity.TypeString, Enum: []string{"open", "done"}, Default: "open", Doc: "Lifecycle state", Widget: "select", HideList: true, Index: []int{2}},
		{Name: "notes", Column: "notes", Type: entity.TypeText, Widget: "textarea", HideList: true, Index: []int{3}},
		{Name: "dueAt", Column: "due_at", Type: entity.TypeTime, Index: []int{4}},
		{Name: "watchers", Column: "watchers", Type: entity.TypeList, Elem: entity.TypeUUID, Index: []int{5}},
	}
	if got := entity.Fields[*record](); !reflect.DeepEqual(got, want) {
		t.Fatalf("fields = %+v, want %+v", got, want)
	}
	r := &record{Title: "draft"}
	field, ok := entity.FieldNamed(entity.Fields[*record](), "title")
	if !ok {
		t.Fatal("title is missing")
	}
	reflect.ValueOf(r).Elem().FieldByIndex(field.Index).SetString("edited")
	if r.Title != "edited" {
		t.Fatal("field index does not address the declared value")
	}
	if _, ok := entity.FieldNamed(want, "tenant_id"); ok {
		t.Fatal("lookup accepted a storage column instead of a public name")
	}
	field, _ = entity.FieldNamed(want, "state")
	data, err := json.Marshal(field)
	if err != nil {
		t.Fatal(err)
	}
	const encoded = `{"name":"state","type":"string","widget":"select","enum":["open","done"],"hideList":true,"default":"open","doc":"Lifecycle state"}`
	if string(data) != encoded {
		t.Errorf("field JSON = %s, want %s", data, encoded)
	}
}

func TestFieldsOfNeedsNoEntityOrStorageIdentity(t *testing.T) {
	type input struct {
		Count  int      `json:"count"`
		Ratio  float32  `json:"ratio"`
		Active bool     `json:"active"`
		Tags   []string `json:"tags"`
	}
	want := []entity.Field{
		{Name: "count", Column: "count", Type: entity.TypeInt, Index: []int{0}},
		{Name: "ratio", Column: "ratio", Type: entity.TypeFloat, Index: []int{1}},
		{Name: "active", Column: "active", Type: entity.TypeBool, Index: []int{2}},
		{Name: "tags", Column: "tags", Type: entity.TypeList, Elem: entity.TypeString, Index: []int{3}},
	}
	for _, typ := range []reflect.Type{reflect.TypeFor[input](), reflect.TypeFor[*input](), reflect.TypeFor[**input]()} {
		if got := entity.FieldsOf(typ); !reflect.DeepEqual(got, want) {
			t.Errorf("FieldsOf(%v) = %+v, want %+v", typ, got, want)
		}
	}
	for _, typ := range []reflect.Type{nil, reflect.TypeFor[int](), reflect.TypeFor[[]input](), reflect.TypeFor[any](), reflect.TypeFor[struct{}]()} {
		if got := entity.FieldsOf(typ); got != nil {
			t.Errorf("FieldsOf(%v) = %+v, want no fields", typ, got)
		}
	}
}

func TestFieldsDoNotShareMutableMetadata(t *testing.T) {
	want := entity.Fields[*record]()
	for name, fields := range map[string]func() []entity.Field{
		"entity": entity.Fields[*record],
		"plain type": func() []entity.Field {
			return entity.FieldsOf(reflect.TypeFor[record]())
		},
	} {
		t.Run(name, func(t *testing.T) {
			got := fields()
			got[0].Name = "forged"
			got[0].ReadOnly = false
			got[0].Index[0] = 99
			state, _ := entity.FieldNamed(got, "state")
			state.Enum[0] = "forged"
			if next := fields(); !reflect.DeepEqual(next, want) {
				t.Errorf("one reader changed another reader's metadata: %+v", next)
			}
		})
	}
}

func TestConcurrentReadersOwnTheirFieldMetadata(t *testing.T) {
	type fresh struct {
		State string `json:"state" enum:"open,done"`
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			fields := entity.FieldsOf(reflect.TypeFor[fresh]())
			if fields[0].Name != "state" || fields[0].Enum[0] != "open" || fields[0].Index[0] != 0 {
				t.Error("metadata arrived modified by another caller")
			}
			fields[0].Name = "changed"
			fields[0].Enum[0] = "changed"
			fields[0].Index[0] = 42
		})
	}
	wg.Wait()
}

func TestBaseOfReturnsTheEmbeddedValue(t *testing.T) {
	r := &record{Title: "untouched"}
	base := entity.BaseOf(r)
	if base != &r.Base {
		t.Fatal("BaseOf copied the embedded value")
	}
	base.ID = uuid.New()
	base.TenantID = uuid.New()
	if r.ID != base.ID || r.TenantID != base.TenantID || r.Title != "untouched" {
		t.Fatal("BaseOf does not address only the embedded metadata")
	}
}

type unrelated struct{}

func (unrelated) TableName() string  { return "unrelated" }
func (unrelated) base() *entity.Base { return &entity.Base{} }

func TestAnUnrelatedBaseMethodDoesNotSatisfyEntity(t *testing.T) {
	if _, ok := any(unrelated{}).(entity.Entity); ok {
		t.Fatal("an unrelated private method opened the Entity constraint")
	}
}
