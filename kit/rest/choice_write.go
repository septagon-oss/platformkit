package rest

import (
	"context"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
)

func (s Spec[T]) updateChoiceRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, fields []entity.Field, values map[string]any, quoted string) (T, error) {
	if err := choiceCondition(fields, s.Immutable, quoted); err != nil {
		var zero T
		return zero, err
	}
	return s.updateRow(ctx, tx, id, fields, values, quoted)
}

func (s Spec[T]) updateRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, fields []entity.Field, values map[string]any, quoted string) (T, error) {
	e, err := s.updateRowRaw(ctx, tx, id, fields, values, quoted)
	if err != nil && hasChoices(fields) {
		var zero T
		return zero, err
	}
	return e, err
}

func (s Spec[T]) choicePatch(tx db.Tx[db.Tenant], e T, fields []entity.Field, values map[string]any, quoted string) (map[string]any, error) {
	if !hasChoices(fields) {
		return values, nil
	}
	if err := crud.RecheckTenant(tx, e); err != nil {
		return nil, err
	}
	if err := refuseStaleTag(quoted, s.revisionOf(fields, e)); err != nil {
		return nil, err
	}
	if name := foldedName(values, s.Immutable); name != "" {
		return nil, immutableRefusal(name)
	}
	current, err := row(e)
	if err != nil {
		return nil, err
	}
	return normalizeChoices(fields, values, current, false)
}
