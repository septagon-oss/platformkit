package crud

import (
	"reflect"

	"github.com/septagon-oss/platformkit/kit/entity"
)

// Schema describes the fields and resource identity of an entity.
type Schema = entity.Schema

// FieldType is the shape a field exposes to queries and presentation.
type FieldType = entity.FieldType

const (
	TypeString = entity.TypeString
	TypeText   = entity.TypeText
	TypeInt    = entity.TypeInt
	TypeFloat  = entity.TypeFloat
	TypeBool   = entity.TypeBool
	TypeTime   = entity.TypeTime
	TypeUUID   = entity.TypeUUID
	TypeList   = entity.TypeList
)

// Field is the existing entity metadata, shared with kit/entity consumers.
type Field = entity.Field

// Fields derives caller-owned metadata for the pointer entity type.
func Fields[T Entity]() []Field { return entity.Fields[T]() }

// FieldsOf derives caller-owned metadata for a struct or pointer to one.
func FieldsOf(t reflect.Type) []Field { return entity.FieldsOf(t) }

// FieldNamed finds a field by its JSON name in the caller's metadata.
func FieldNamed(fields []Field, name string) (Field, bool) {
	return entity.FieldNamed(fields, name)
}
