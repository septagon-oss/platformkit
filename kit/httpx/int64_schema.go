package httpx

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/septagon-oss/platformkit/kit/internal/jsonbody"
)

type integerSchemaMode string

const (
	runtimeIntegers  integerSchemaMode = ""
	inputIntegers    integerSchemaMode = "Int64Input"
	responseIntegers integerSchemaMode = "Int64Response"
)

func projectSchema(reg huma.Registry, source *huma.Schema, shape *jsonbody.Shape, mode integerSchemaMode, runtime map[string]*huma.Schema) *huma.Schema {
	return projectIntegerSchema(reg, source, shape, mode, runtime, map[string]bool{}, "")
}

func projectIntegerSchema(reg huma.Registry, source *huma.Schema, shape *jsonbody.Shape, mode integerSchemaMode, runtime map[string]*huma.Schema, seen map[string]bool, tag reflect.StructTag) *huma.Schema {
	if source == nil || source.Type == "null" || shape == nil || shape.Opaque {
		return source
	}
	for shape.Type.Kind() == reflect.Pointer {
		shape = shape.Elem
		if shape == nil || shape.Opaque {
			return source
		}
	}
	if source.Ref != "" {
		if !shape.HasInteger() {
			return source
		}
		ref := source.Ref + string(mode)
		if !seen[ref] {
			seen[ref] = true
			derived := projectIntegerSchema(reg, reg.SchemaFromRef(source.Ref), shape, mode, runtime, seen, tag)
			if mode == runtimeIntegers {
				runtime[ref] = derived
			} else {
				if reg.TypeFromRef(ref) != nil {
					panic("httpx: derived integer schema collides with " + ref)
				}
				reg.Map()[strings.TrimPrefix(ref, "#/components/schemas/")] = derived
			}
		}
		copy := *source
		copy.Ref = ref
		return &copy
	}
	copy := *source
	if mode != runtimeIntegers && shape.HasInteger() {
		copy.Default = integerAnnotation(source.Default, tag.Get("default"), shape)
		if len(source.Examples) > 0 {
			copy.Examples = []any{integerAnnotation(source.Examples[0], tag.Get("example"), shape)}
		}
	}
	if shape.Integer && len(source.AnyOf) == 0 {
		return integerScalarSchema(&copy, mode, tag)
	}
	copy.Properties = maps.Clone(source.Properties)
	for name, property := range source.Properties {
		if f := shape.Member(name); f != nil {
			copy.Properties[name] = projectIntegerSchema(reg, property, f.Shape, mode, runtime, seen, f.Tag)
		} else if name == "$schema" && mode != runtimeIntegers && len(property.Examples) > 0 {
			p := *property
			p.Examples = []any{strings.TrimSuffix(fmt.Sprint(property.Examples[0]), ".json") + string(mode) + ".json"}
			copy.Properties[name] = &p
		}
	}
	copy.Items = projectIntegerSchema(reg, source.Items, shape.Elem, mode, runtime, seen, "")
	if additional, ok := source.AdditionalProperties.(*huma.Schema); ok {
		copy.AdditionalProperties = projectIntegerSchema(reg, additional, shape.Elem, mode, runtime, seen, "")
	}
	copy.AnyOf = nil
	for _, branch := range source.AnyOf {
		copy.AnyOf = append(copy.AnyOf, projectIntegerSchema(reg, branch, shape, mode, runtime, seen, tag))
	}
	return &copy
}

func integerScalarSchema(source *huma.Schema, mode integerSchemaMode, tag reflect.StructTag) *huma.Schema {
	if mode == runtimeIntegers {
		return &huma.Schema{ReadOnly: source.ReadOnly, WriteOnly: source.WriteOnly}
	}
	text := *source
	text.Type, text.Format, text.Pattern = "string", "int64", `^(0|-?[1-9][0-9]*)$`
	text.Minimum, text.Maximum, text.ExclusiveMinimum, text.ExclusiveMaximum, text.MultipleOf = nil, nil, nil, nil, nil
	text.Description += " Signed 64-bit decimal integer, -9223372036854775808 through 9223372036854775807."
	text.Enum = nil
	for _, literal := range enumLiterals(tag, source.Enum) {
		digits, err := jsonbody.Decimal([]byte(literal))
		if err != nil {
			panic(err)
		}
		text.Enum = append(text.Enum, digits)
	}
	number := text
	number.Type, number.Pattern = "integer", ""
	number.Default, number.Examples, number.Enum = nil, nil, nil
	number.Extensions = map[string]any{}
	maps.Copy(number.Extensions, source.Extensions)
	for _, key := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum", "multipleOf"} {
		if literal := tag.Get(key); literal != "" {
			text.Description += " " + key + ": " + literal + "."
			number.Extensions[key] = json.Number(literal)
		}
	}
	for _, value := range text.Enum {
		number.Enum = append(number.Enum, json.Number(value.(string)))
	}
	for _, value := range text.Examples {
		number.Examples = append(number.Examples, json.Number(value.(string)))
	}
	if mode == responseIntegers {
		return &text
	}
	text.Pattern = `^-?(0|[1-9][0-9]*)$`
	text.Nullable, number.Nullable = false, false
	union := &huma.Schema{AnyOf: []*huma.Schema{&text, &number}, Default: text.Default, Examples: text.Examples,
		Description: "Prefer decimal strings in clients that cannot retain every integer token.", ReadOnly: source.ReadOnly, WriteOnly: source.WriteOnly}
	text.Default = nil
	if source.Nullable {
		union.AnyOf = append(union.AnyOf, &huma.Schema{Type: "null"})
	}
	return union
}

func integerAnnotation(value any, literal string, shape *jsonbody.Shape) any {
	if value == nil {
		return nil
	}
	data := []byte(literal)
	if literal == "" {
		data, _ = json.Marshal(value)
	}
	encoded, err := jsonbody.Rewrite(data, shape, reflect.Value{}, true, true)
	if err != nil {
		panic("httpx: invalid integer annotation: " + err.Error())
	}
	var result any
	if err := jsonbody.Tokens(encoded, &result); err != nil {
		panic(err)
	}
	return result
}

func enumLiterals(tag reflect.StructTag, fallback []any) []string {
	if literal := tag.Get("enum"); literal != "" {
		return strings.Split(literal, ",")
	}
	var result []string
	for _, value := range fallback {
		data, _ := json.Marshal(value)
		result = append(result, string(data))
	}
	return result
}

func (a *integerAPI) projectOperation() *huma.Operation {
	op := *a.op
	if op.RequestBody != nil && op.RequestBody.Content["application/json"] != nil {
		body := *op.RequestBody
		body.Content = maps.Clone(body.Content)
		body.Content["application/json"] = &huma.MediaType{Schema: a.inputSchema}
		body.Content[int64JSONMediaType] = body.Content["application/json"]
		op.RequestBody = &body
	}
	if a.eligible {
		op.Description += " Send Accept: " + int64JSONMediaType + " to select these success responses."
		op.Responses = maps.Clone(op.Responses)
		for status, response := range op.Responses {
			if media := response.Content[int64JSONMediaType]; strings.HasPrefix(status, "2") && media != nil {
				copy := *response
				copy.Content = map[string]*huma.MediaType{int64JSONMediaType: media}
				op.Responses[status] = &copy
			}
		}
	}
	return &op
}
