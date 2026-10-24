package rest

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
)

func (s Singleton[T]) saveForm(ctx context.Context, tx db.Tx[db.Tenant], values map[string]any) (T, error) {
	var zero T
	tag, values, err := conditional(values)
	if err != nil {
		return zero, err
	}
	current, err := s.Load(ctx, tx)
	if err != nil {
		return zero, err
	}
	if tag != "" {
		// Lock the stored root before reloading the owner's projection and checking
		// its revision. Save retains ownership of validation, counters and events.
		if _, err := crud.GetForUpdate[T](tx, entity.BaseOf(current).ID); err != nil {
			return zero, err
		}
		current, err = s.Load(ctx, tx)
		if err != nil {
			return zero, err
		}
		if err := crud.RecheckTenant(tx, current); err != nil {
			return zero, err
		}
		if err := refuseStaleTag(tag, (Spec[T]{}).revisionOf(crud.Fields[T](), current)); err != nil {
			return zero, err
		}
	}
	if _, err := merge(current, crud.Fields[T](), nil, values); err != nil {
		return zero, err
	}
	return s.Save(ctx, tx, current)
}
