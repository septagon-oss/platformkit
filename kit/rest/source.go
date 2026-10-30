package rest

import (
	"context"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// ReadSource supplies authorized service projections for both resource adapters.
// Both callbacks are required. List applies scope before counting or pagination
// and refuses unsupported query fields. It must return zero results on error.
// A source permits only List/Read operations; commands own its mutations.
type ReadSource[T crud.Entity] struct {
	Get  func(context.Context, db.Tx[db.Tenant], uuid.UUID) (T, error)
	List func(context.Context, db.Tx[db.Tenant], crud.Query) ([]T, int64, error)
}

func (s Spec[T]) get(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (T, error) {
	if s.Session != nil {
		var zero T
		return zero, crud.ErrInvalid
	}
	if s.Source != nil {
		return s.Source.Get(ctx, tx, id)
	}
	return crud.GetCtx[T](ctx, tx, id)
}

func (s Spec[T]) list(ctx context.Context, tx db.Tx[db.Tenant], q crud.Query) ([]T, int64, error) {
	if s.Session != nil {
		return nil, 0, crud.ErrInvalid
	}
	if s.Source != nil {
		return s.Source.List(ctx, tx, q)
	}
	return crud.ListCtx[T](ctx, tx, q)
}

func (s Spec[T]) count(ctx context.Context, tx db.Tx[db.Tenant]) (int64, error) {
	if s.Session != nil {
		return 0, crud.ErrInvalid
	}
	if s.Source != nil {
		_, total, err := s.Source.List(ctx, tx, crud.Query{Limit: 1})
		return total, err
	}
	return crud.CountCtx[T](ctx, tx)
}
