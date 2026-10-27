package rest

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/internal/jsonbody"
)

// defaultCommandBody keeps defaults out of huma's mutable, float-based default
// decoder. Validation still uses its schema; the typed preparation owns values.
type defaultCommandBody[I any] struct{ raw json.RawMessage }

func (defaultCommandBody[I]) JSONBodyType() reflect.Type { return reflect.TypeFor[I]() }

func (b *defaultCommandBody[I]) UnmarshalJSON(data []byte) error {
	b.raw = append(b.raw[:0], data...)
	return nil
}

var defaultTag = regexp.MustCompile(`\bdefault:"(?:[^"\\]|\\.)*"`)

func (defaultCommandBody[I]) Schema(registry huma.Registry) *huma.Schema {
	typ := reflect.TypeFor[I]()
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	fields := []reflect.StructField{}
	for _, f := range reflect.VisibleFields(typ) {
		if !f.IsExported() || f.Anonymous {
			continue
		}
		f.Tag = reflect.StructTag(defaultTag.ReplaceAllString(string(f.Tag), ""))
		fields = append(fields, f)
	}
	schema := huma.SchemaFromType(registry, reflect.StructOf(fields))
	// Defaults remain documented, but the request must be allowed to omit them.
	for _, sf := range reflect.VisibleFields(typ) {
		value := sf.Tag.Get("default")
		if value == "" {
			continue
		}
		name := jsonNameOf(sf)
		if prop := schema.Properties[name]; prop != nil {
			if f, ok := entity.FieldNamed(entity.FieldsOf(typ), name); ok {
				prop.Default, _ = entity.DefaultValue(f)
			} else {
				prop.Default = json.RawMessage(value)
			}
			required := schema.Required[:0]
			for _, requiredName := range schema.Required {
				if requiredName != name {
					required = append(required, requiredName)
				}
			}
			schema.Required = required
		}
	}
	return schema
}

func mountDefaultCommand[I, T any](router *httpx.Router, op huma.Operation, auth httpx.Auth, collection bool,
	prepare func(map[string]any) (I, error), answer func(context.Context, db.Tx[db.Tenant], uuid.UUID, *I) (T, error),
	opts OperationOptions,
) {
	invoke := func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, body *defaultCommandBody[I]) (T, error) {
		var values map[string]any
		if body != nil {
			if err := jsonbody.Tokens(body.raw, &values); err != nil {
				var zero T
				return zero, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
			}
		}
		in, err := prepare(values)
		if err != nil {
			var zero T
			return zero, err
		}
		return answer(ctx, tx, id, &in)
	}
	if collection {
		Operation(router, op, auth, func(ctx context.Context, tx db.Tx[db.Tenant], _ uuid.UUID, in *collectionInput[defaultCommandBody[I]]) (T, error) {
			return invoke(ctx, tx, uuid.Nil, in.Body)
		}, opts)
	} else {
		Operation(router, op, auth, func(ctx context.Context, tx db.Tx[db.Tenant], _ uuid.UUID, in *commandInput[defaultCommandBody[I]]) (T, error) {
			return invoke(ctx, tx, in.ID, in.Body)
		}, opts)
	}
}
