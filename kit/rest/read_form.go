package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

type readInputSpec struct {
	fields, output []entity.Field
	list           bool
	run            func(context.Context, map[string]any) (any, error)
	mount          func(*httpx.Router, huma.Operation, httpx.Auth)
}

// ReadForm declares generated inputs for a typed, read-only query. I is a flat
// input struct; O is a readable struct or slice of structs. Set Module, Entity,
// Path, ReadAuth (or Read) and Present on the returned Spec before mounting.
// Neither type needs entity identity. Invalid shapes panic; failures return no O.
func ReadForm[I, O any](run func(context.Context, db.Tx[db.Tenant], uuid.UUID, I) (O, error)) Spec[entity.Entity] {
	fields := entity.FieldsOf(reflect.TypeFor[I]())
	outType := reflect.TypeFor[O]()
	list := outType.Kind() == reflect.Slice
	if list {
		outType = outType.Elem()
	}
	output := entity.FieldsOf(outType)
	if run == nil || inputSchemaFault(reflect.TypeFor[I](), fields) != "" || inputSchemaFault(outType, output) != "" || len(output) == 0 {
		panic("rest: ReadForm requires flat typed input and readable struct output")
	}
	registry := huma.NewMapRegistry("#/components/schemas/", huma.DefaultSchemaNamer)
	schema := huma.SchemaFromType(registry, reflect.TypeFor[I]())
	prepare, _, err := commandDefaults[I](fields)
	if err != nil {
		panic(err)
	}
	invoke := func(ctx context.Context, tx db.Tx[db.Tenant], actor uuid.UUID, values map[string]any) (O, error) {
		var zero O
		in, err := prepare(values)
		if err != nil {
			return zero, err
		}
		if err := validateReadInput(ctx, in, fields, registry, schema); err != nil {
			return zero, err
		}
		out, err := run(ctx, tx, actor, in)
		if err != nil {
			return zero, err
		}
		return out, nil
	}
	input := &readInputSpec{fields: fields, output: output, list: list}
	input.run = func(ctx context.Context, values map[string]any) (any, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		actor, _ := tenancy.PrincipalFrom(ctx)
		out, err := invoke(ctx, tx, actor.UserID, values)
		if err != nil {
			return nil, Fault(err)
		}
		return out, nil
	}
	input.mount = func(router *httpx.Router, op huma.Operation, auth httpx.Auth) {
		Operation(router, op, auth, func(ctx context.Context, tx db.Tx[db.Tenant], actor uuid.UUID, body *collectionInput[defaultCommandBody[I]]) (O, error) {
			values := map[string]any{}
			if body.Body != nil {
				d := json.NewDecoder(bytes.NewReader(body.Body.raw))
				d.UseNumber()
				if err := d.Decode(&values); err != nil {
					var zero O
					return zero, problem.New(422, "Invalid query input")
				}
			}
			return invoke(ctx, tx, actor, values)
		}, OperationOptions{CacheControl: "private, no-store"})
	}
	return Spec[entity.Entity]{input: input}
}

func validateReadInput[I any](ctx context.Context, in I, fields []entity.Field, registry huma.Registry, schema *huma.Schema) error {
	encoded, err := json.Marshal(in)
	if err != nil {
		return err
	}
	var values map[string]any
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	err = decoder.Decode(&values)
	if err != nil {
		return err
	}
	result := &huma.ValidateResult{}
	huma.Validate(registry, schema, huma.NewPathBuffer(nil, 0), huma.ModeWriteToServer, values, result)
	if len(result.Errors) > 0 {
		p := problem.New(422, "Invalid query input")
		for _, err := range result.Errors {
			if detail, ok := errors.AsType[*huma.ErrorDetail](err); ok {
				p.Errors = append(p.Errors, detail.Location+": "+detail.Message)
			}
		}
		return p
	}
	raw := map[string]string{}
	for name, value := range values {
		raw[name] = Text(value)
	}
	for _, f := range fields {
		if err := validateFormField(raw, f, map[string]any{}); err != nil {
			return err
		}
	}
	v, ok := any(in).(entity.Validator)
	if !ok {
		v, ok = any(&in).(entity.Validator)
	}
	if !ok {
		return nil
	}
	if err := v.Validate(ctx); err != nil {
		return fmt.Errorf("%w: %w", crud.ErrInvalid, err)
	}
	return nil
}

func (s Spec[T]) mountReadForm(surfaces httpx.Surfaces) {
	if bad := s.readFormFault(); bad != "" {
		panic("rest: ReadForm: " + bad)
	}
	r := httpx.Resource{Module: s.Module, Entity: s.Entity, Path: s.Path, Schema: s.Schema(),
		Read: s.Read, ReadBy: s.ReadAuth, Present: s.Present, Screen: surfaces.App.PagePath(s.Path)}
	r.Schema.Path = surfaces.App.Prefix() + s.Path
	r.InputRun, r.InputOutput, r.InputList = s.input.run, s.input.output, s.input.list
	if bad := formHintsFault(r); bad != "" {
		panic("rest: ReadForm: " + bad)
	}
	surfaces.RegisterResource(r)
	s.input.mount(surfaces.App, huma.Operation{OperationID: s.Module + "-" + s.Entity + "-query", Method: "POST", Path: s.Path, Summary: "Query " + s.Entity}, s.readAuth())
}

func (s Spec[T]) readFormFault() string {
	if s.readFormHasStorage() {
		return "input-only Specs cannot declare storage or writes"
	}
	if s.OperatorRead || s.readAuth().Operator() || !s.readAuth().Declared() || s.readAuth().IsPublic() || (s.Read != "" && s.ReadAuth.Declared()) {
		return "generated queries require one workspace read guard"
	}
	// Validate the ordinary address without reflecting an entity carrier.
	identity := Spec[entity.Entity]{Module: s.Module, Entity: s.Entity, Path: s.Path, ReadAuth: httpx.SignedIn(), WriteAuth: httpx.SignedIn()}
	if bad := identity.identityFault(); bad != "" {
		return bad
	}
	if bad := entryHintsFault(s.Entity, s.Present, s.input.fields); bad != "" {
		return bad
	}
	if bad := fieldHintFault(s.Entity, s.input.fields, s.Present); bad != "" {
		return bad
	}
	for _, f := range s.input.fields {
		if entity.ServerOwned(f) || f.Widget == "file" || f.Widget == "password" || f.Type == entity.TypeMap {
			return fmt.Sprintf("input field %q is not a supported question", f.Name)
		}
	}
	return widgetFault(s.input.fields)
}

func (s Spec[T]) readFormHasStorage() bool {
	return s.Source != nil || len(s.Operations) > 0 || s.Write != "" || s.WriteAuth.Declared() || s.OperatorWrite || s.SoftDelete || len(s.Immutable) > 0 || s.History || s.Versions != nil || s.Translations != nil || len(s.HookEvents) > 0 || len(s.HookTopics) > 0 || s.Gate != nil || s.Create != nil || s.AfterCreate != nil || s.AfterDelete != nil || s.Audience != nil || s.FileUses != nil || s.RichTextFiles != nil
}
