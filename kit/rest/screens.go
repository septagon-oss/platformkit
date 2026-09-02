package rest

// screens.go registers the entity itself alongside its routes, so that the
// screens of stage E4 are derived from the same Spec the API is.
//
// The five closures are the five routes without the HTTP: same transaction,
// same errors, same events, same read-only fields. A screen calls them in
// process rather than calling its own API over a socket, which would be a
// second request, a second transaction and a second authorization of a caller
// the first one already recognised.

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
)

// resource is this Spec as httpx.Resource, for Mount to register.
func (s Spec[T]) resource() httpx.Resource {
	schema := s.Schema()
	// each is the shape every closure below has: take the request's
	// transaction, do one thing, and answer with the one error mapping.
	each := func(ctx context.Context, run func(db.Tx[db.Tenant]) (T, error)) (map[string]any, error) {
		tx, err := transaction(ctx)
		if err != nil {
			return nil, err
		}
		e, err := run(tx)
		if err != nil {
			return nil, Fault(err)
		}
		return row(e)
	}
	return httpx.Resource{
		Module: s.Module, Entity: s.Entity, Path: s.Path,
		Read: s.Read, Write: s.Write, Immutable: s.Immutable, Schema: schema,

		List: func(ctx context.Context, q crud.Query) ([]map[string]any, int64, error) {
			tx, err := transaction(ctx)
			if err != nil {
				return nil, 0, err
			}
			items, total, err := crud.List[T](tx, q)
			if err != nil {
				return nil, 0, Fault(err)
			}
			rows := make([]map[string]any, 0, len(items))
			for _, item := range items {
				r, err := row(item)
				if err != nil {
					return nil, 0, err
				}
				rows = append(rows, r)
			}
			return rows, total, nil
		},
		Get: func(ctx context.Context, id uuid.UUID) (map[string]any, error) {
			return each(ctx, func(tx db.Tx[db.Tenant]) (T, error) { return crud.Get[T](tx, id) })
		},
		Create: func(ctx context.Context, values map[string]any) (map[string]any, error) {
			return each(ctx, func(tx db.Tx[db.Tenant]) (T, error) {
				e, err := decode[T](values)
				if err != nil {
					return e, err
				}
				crud.Reset(e) // the four fields the server owns, whatever a form sent
				if err := crud.Create(ctx, tx, e); err != nil {
					return e, err
				}
				return e, s.emit(ctx, tx, Created, e, s.AfterCreate)
			})
		},
		Update: func(ctx context.Context, id uuid.UUID, values map[string]any) (map[string]any, error) {
			return each(ctx, func(tx db.Tx[db.Tenant]) (T, error) {
				e, err := crud.Get[T](tx, id)
				if err != nil {
					return e, err
				}
				// merge is the PATCH route's own: read-only and Immutable
				// fields are refused here exactly as they are over HTTP.
				columns, err := merge(e, schema.Fields, s.Immutable, values)
				if err != nil {
					return e, err
				}
				if err := crud.Update(ctx, tx, e, append(columns, "updated_at")...); err != nil {
					return e, err
				}
				return e, s.emit(ctx, tx, Updated, e, s.AfterUpdate)
			})
		},
		Delete: func(ctx context.Context, id uuid.UUID) error {
			_, err := each(ctx, func(tx db.Tx[db.Tenant]) (T, error) {
				e, err := crud.Get[T](tx, id)
				if err != nil {
					return e, err
				}
				if err := crud.Delete[T](tx, id, s.SoftDelete); err != nil {
					return e, err
				}
				return e, s.emit(ctx, tx, Deleted, e, s.AfterDelete)
			})
			return err
		},
	}
}

// row is the entity as a screen reads it. It is the entity's own JSON, so a
// column shows what the API would have sent and json:"-" hides a field from
// both at once.
func row(e any) (map[string]any, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(b, &m)
}

// decode builds an entity from a form's values, through the same decoder the
// request body goes through, so "3" is an int in both.
//
// T is a pointer type, so unmarshalling into &e is unmarshalling into a
// **Task, and encoding/json allocates the Task. That is why this needs no
// reflection of its own.
func decode[T crud.Entity](values map[string]any) (T, error) {
	var e T
	b, err := json.Marshal(values)
	if err != nil {
		return e, err
	}
	return e, json.Unmarshal(b, &e)
}
