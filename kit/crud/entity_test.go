package crud_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/entity"
)

type portableRecord struct {
	entity.Base
	Title string `json:"title"`
}

func (portableRecord) TableName() string               { return "portable_records" }
func (*portableRecord) Validate(context.Context) error { return nil }

var (
	_ entity.Entity    = (*Task)(nil)
	_ crud.Entity      = (*portableRecord)(nil)
	_ crud.Validator   = (*portableRecord)(nil)
	_ entity.Validator = (*Task)(nil)
)

func TestEntityAliasesKeepExistingContractsUsable(t *testing.T) {
	var oldBase crud.Base = entity.Base{ID: uuid.New()}
	var newBase entity.Base = oldBase
	old := &Task{Base: oldBase, Title: "kept"}
	if entity.BaseOf(old) != &old.Base || newBase.ID != old.ID {
		t.Fatal("CRUD and entity disagree on the embedded Base")
	}
	var schema entity.Schema = crud.Schema{Fields: crud.Fields[*Task]()}
	var legacy crud.Schema = schema
	if got := entity.Fields[*Task](); !reflect.DeepEqual(got, legacy.Fields) {
		t.Fatalf("CRUD and entity schemas differ: %+v / %+v", legacy.Fields, got)
	}
	var field entity.Field
	field, _ = crud.FieldNamed(schema.Fields, "title")
	if field.Type != entity.TypeString {
		t.Fatalf("aliased field type = %q", field.Type)
	}
	portable := &portableRecord{Base: newBase, Title: "kept"}
	crud.Reset(portable)
	crud.Reset(old)
	if portable.Base != (entity.Base{}) || old.Base != (crud.Base{}) || portable.Title != "kept" || old.Title != "kept" {
		t.Fatal("Reset no longer clears either generation of embedded Base")
	}
}
