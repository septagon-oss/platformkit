package httpx

import (
	"encoding/json"
	"maps"
	"mime"
	"reflect"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/septagon-oss/platformkit/kit/internal/jsonbody"
)

// int64JSONMediaType selects decimal-string int64 responses. Ordinary JSON
// remains numeric during the deprecation window; both forms are accepted as input.
const int64JSONMediaType = "application/vnd.platformkit.int64+json"

type integerAPI struct {
	huma.API
	doc          huma.OpenAPI
	registry     *integerRegistry
	owner        *API
	op           *huma.Operation
	body, output *jsonbody.Shape
	patch        bool
	eligible     bool
	inputSchema  *huma.Schema
}

type integerRegistry struct {
	huma.Registry
	validation map[string]*huma.Schema
}

func (r *integerRegistry) SchemaFromRef(ref string) *huma.Schema {
	if s := r.validation[ref]; s != nil {
		return s
	}
	return r.Registry.SchemaFromRef(ref)
}

func (a *integerAPI) OpenAPI() *huma.OpenAPI { return &a.doc }

func newIntegerAPI[I, O any](owner *API) *integerAPI {
	a := &integerAPI{API: owner.api, owner: owner, doc: *owner.api.OpenAPI()}
	components := *a.doc.Components
	a.registry = &integerRegistry{Registry: components.Schemas, validation: map[string]*huma.Schema{}}
	components.Schemas = a.registry
	a.doc.Components = &components
	if f, ok := reflect.TypeFor[I]().FieldByName("Body"); ok {
		a.body = bodyShape(f.Type)
	}
	if f, ok := reflect.TypeFor[O]().FieldByName("Body"); ok {
		a.output = jsonbody.Describe(f.Type)
		_, protocol := reflect.New(f.Type).Interface().(interface{ ContentType(string) string })
		_, protocolValue := reflect.Zero(f.Type).Interface().(interface{ ContentType(string) string })
		a.eligible = !protocol && !protocolValue && f.Type.Kind() != reflect.Func && f.Type != reflect.TypeFor[[]byte]()
	}
	if err := jsonbody.Audit(a.body); err != nil {
		owner.noteRefusal("httpx: " + err.Error())
	}
	return a
}

func bodyShape(t reflect.Type) *jsonbody.Shape {
	if metadata, ok := reflect.New(t).Interface().(interface{ JSONBodyType() reflect.Type }); ok {
		t = metadata.JSONBodyType()
	} else if t.Kind() == reflect.Pointer {
		if metadata, ok := reflect.New(t.Elem()).Interface().(interface{ JSONBodyType() reflect.Type }); ok {
			t = metadata.JSONBodyType()
		}
	}
	return jsonbody.Describe(t)
}

func (a *integerAPI) DocumentOperation(op *huma.Operation) {
	a.op = op
	if !op.Hidden {
		a.API.OpenAPI().AddOperation(op)
	}
	if op.RequestBody != nil {
		if media := op.RequestBody.Content["application/json"]; media != nil {
			original := *media.Schema
			runtime := projectSchema(a.registry.Registry, &original, a.body, runtimeIntegers, a.registry.validation)
			*media.Schema = *runtime
			media.Schema = &original
			op.RequestBody.Description += " Signed 64-bit fields also accept exact decimal strings; /openapi.json?int64=string documents both input forms."
		}
	}
	a.owner.integers = append(a.owner.integers, a)
}

func (a *integerAPI) Unmarshal(contentType string, data []byte, target any) error {
	media, _, _ := mime.ParseMediaType(contentType)
	if media != "" && media != "application/json" && media != int64JSONMediaType {
		return a.API.Unmarshal(contentType, data, target)
	}
	if _, untyped := target.(*any); untyped {
		return jsonbody.Tokens(data, target)
	}
	if a.body != nil {
		var err error
		data, err = jsonbody.Rewrite(data, a.body, reflect.Value{}, false, a.patch)
		if err != nil {
			return err
		}
	}
	if err := jsonbody.Tokens(data, target); err != nil {
		return err
	}
	return jsonbody.Defaults(reflect.ValueOf(target).Elem(), jsonbody.Describe(reflect.TypeOf(target).Elem()))
}

func (a *integerAPI) Negotiate(accept string) (string, error) {
	if a.eligible {
		if decimalAccepted(accept) {
			return int64JSONMediaType, nil
		}
		return "application/json", nil
	}
	return a.API.Negotiate(accept)
}

func (a *integerAPI) Transform(ctx huma.Context, status string, value any) (any, error) {
	value, err := a.API.Transform(ctx, status, value)
	if err != nil || !a.eligible || !strings.HasPrefix(status, "2") {
		return value, err
	}
	r, w := humachi.Unwrap(ctx)
	mergeVary(w.Header(), "Accept")
	if !decimalAccepted(r.Header.Get("Accept")) {
		return value, nil
	}
	response := ctx.Operation().Responses[status]
	if response == nil || response.Content[int64JSONMediaType] == nil {
		return value, nil
	}
	ref := response.Content[int64JSONMediaType].Schema.Ref
	if ref == "" {
		return value, nil
	}
	url := "/schemas/" + strings.TrimPrefix(ref, "#/components/schemas/") + ".json"
	v := reflect.ValueOf(value)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() == reflect.Struct {
		copy := reflect.New(v.Type()).Elem()
		copy.Set(v)
		if f := copy.FieldByName("Schema"); f.IsValid() && f.CanSet() && f.Kind() == reflect.String {
			f.SetString(url)
		}
		value = copy.Interface()
	}
	ctx.SetHeader("Link", "<"+url+">; rel=\"describedBy\"")
	return value, nil
}

func (a *API) integerDocuments() {
	for _, operation := range a.integers {
		operation.documentIntegers()
	}
}

func (a *integerAPI) documentIntegers() {
	registry := a.API.OpenAPI().Components.Schemas
	if a.op.RequestBody != nil {
		if media := a.op.RequestBody.Content["application/json"]; media != nil {
			a.inputSchema = projectSchema(registry, media.Schema, a.body, inputIntegers, nil)
		}
	}
	if !a.eligible {
		return
	}
	for status, response := range a.op.Responses {
		if !strings.HasPrefix(status, "2") {
			continue
		}
		if media := response.Content["application/json"]; media != nil {
			derived := projectSchema(registry, media.Schema, a.output, responseIntegers, nil)
			response.Content[int64JSONMediaType] = &huma.MediaType{Schema: derived}
			if a.output.HasInteger() && !strings.Contains(response.Description, "Numeric int64 output is deprecated") {
				response.Description += " Numeric int64 output is deprecated; select " + int64JSONMediaType + " for exact decimal strings."
			}
		}
	}
}

// Keep numeric legacy media declarations intact while deriving the selected view.
func (a *API) integerDocument() ([]byte, error) {
	doc := *a.api.OpenAPI()
	doc.Paths = maps.Clone(doc.Paths)
	for path, item := range doc.Paths {
		copied := *item
		doc.Paths[path] = &copied
	}
	for _, operation := range a.integers {
		if operation.op.Hidden {
			continue
		}
		item := reflect.ValueOf(doc.Paths[operation.op.Path]).Elem()
		for i := range item.NumField() {
			field := item.Field(i)
			if field.CanSet() && field.Type() == reflect.TypeFor[*huma.Operation]() && field.Interface() == operation.op {
				field.Set(reflect.ValueOf(operation.projectOperation()))
			}
		}
	}
	data, err := json.Marshal(&doc)
	if err != nil {
		return nil, err
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	result["components"], err = json.Marshal(doc.Components)
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}
