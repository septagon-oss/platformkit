package httpx

import (
	"reflect"

	"github.com/danielgtaylor/huma/v2"
)

// SetFieldMediaType augments a registered entity's wire schema at composition.
// The caller supplies the concrete entity type and named fields explicitly.
func SetFieldMediaType[T any](router *Router, field, mediaType string) {
	registry := router.api.api.OpenAPI().Components.Schemas
	schema := registry.Schema(reflect.TypeFor[T](), false, "")
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
}
