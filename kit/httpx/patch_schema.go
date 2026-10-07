package httpx

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// SetPatchSchema documents a registered map-body PATCH with the writable fields
// its owner accepts. Call during composition, after Register and any entity media
// annotations. Replacing the document reference leaves Huma's captured input
// validator intact: the map decoder and the owner's merge still decide requests.
func SetPatchSchema[T any](router *Router, path string, fields []entity.Field) {
	doc := router.api.api.OpenAPI()
	registry := doc.Components.Schemas
	typ := reflect.TypeFor[T]()
	ref := registry.Schema(typ, true, "").Ref
	name := strings.TrimPrefix(ref, "#/components/schemas/") + "Patch"
	source := registry.SchemaFromRef(ref)
	if source == nil || source.Properties == nil {
		panic("rest: PATCH schema " + name + " has no entity properties")
	}
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	patch := &huma.Schema{Type: huma.TypeObject, Properties: map[string]*huma.Schema{}, AdditionalProperties: false}
	for _, field := range fields {
		property := source.Properties[field.Name]
		if property == nil || (property.Type == "" && property.Ref == "" && len(property.AnyOf) == 0) {
			panic("rest: PATCH schema " + name + " has no typed property for " + field.Name)
		}
		// Only this property's root changes. Its nested schemas and annotations stay
		// untouched, as do the entity's create and response schemas.
		copy := *property
		copy.Default, copy.ReadOnly = nil, false
		fieldType := typ.FieldByIndex(field.Index).Type
		if fieldType.Kind() == reflect.Pointer || fieldType.Kind() == reflect.Slice {
			copy.Nullable = false
			patch.Properties[field.Name] = &huma.Schema{AnyOf: []*huma.Schema{&copy, {Type: "null"}}}
		} else {
			patch.Properties[field.Name] = &copy
		}
	}
	if previous := registry.Map()[name]; previous != nil {
		before, err := json.Marshal(previous)
		if err != nil {
			panic(err)
		}
		after, err := json.Marshal(patch)
		if err != nil {
			panic(err)
		}
		if !bytes.Equal(before, after) {
			panic("rest: PATCH schema " + name + " already describes a different writable shape")
		}
	} else {
		registry.Map()[name] = patch
	}
	operation := doc.Paths[router.compose(path, false)].Patch
	operation.RequestBody.Content["application/json"].Schema = &huma.Schema{Ref: "#/components/schemas/" + name}
}
