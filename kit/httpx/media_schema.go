package httpx

import (
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// SetFieldMediaType augments a registered entity's wire schema at composition.
// The caller supplies the concrete entity type and named fields explicitly.
func SetFieldMediaType[T any](router *Router, field, mediaType string) {
	router.AnnotateSchema(reflect.TypeFor[T](), func(schema *huma.Schema) {
		if schema == nil || schema.Properties == nil {
			panic("httpx: entity schema is missing")
		}
		property := schema.Properties[field]
		if property == nil || property.Type != huma.TypeString {
			panic("httpx: media field is not a string: " + field)
		}
		if property.Extensions == nil {
			property.Extensions = make(map[string]any)
		}
		property.Extensions["contentMediaType"] = mediaType
	})
}

// SetFieldPicture constrains a derived picture's wire schema without changing
// its string representation or importing HTTP into the entity vocabulary.
func SetFieldPicture[T any](router *Router, field string) {
	router.AnnotateSchema(reflect.TypeFor[T](), func(schema *huma.Schema) {
		property := schema.Properties[field]
		if property == nil {
			panic("httpx: picture schema is missing: " + field)
		}
		property.Type = huma.TypeString
		property.MaxLength = new(686)
		property.Pattern = `^(?:|c1\.[A-Za-z0-9_-]+)$`
		property.ReadOnly = true
	})
}

// AnnotateSchema augments the wire schema of an explicitly declared type during
// composition. The callback must preserve its fields and their runtime types.
func (r *Router) AnnotateSchema(typ reflect.Type, annotate func(*huma.Schema)) {
	schema := r.api.api.OpenAPI().Components.Schemas.Schema(typ, false, "")
	if schema == nil {
		panic("httpx: entity schema is missing")
	}
	annotate(schema)
}
