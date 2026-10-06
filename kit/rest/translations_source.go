package rest

import (
	"context"
	"reflect"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// specSource is TranslationSource over one mounted Spec. Everything it knows it
// learned from the Spec and from the entity's own schema: the module's and
// entity's names, which fields are translatable, which of those are richtext,
// and where each one sits in the struct.
//
// It reads through crud, which is what puts the read under row-level security
// and under the caller's transaction: a translation overview that reached the
// entity's table by another route would be an overview that leaked a page of
// somebody else's rows.
type specSource[T crud.Entity] struct{ spec Spec[T] }

func (s *specSource[T]) Module() string { return s.spec.Module }
func (s *specSource[T]) Entity() string { return s.spec.Entity }

// translatableFields is the Spec's entity's translatable strings, in schema
// order. The order is the side-by-side view's order, so "next field" walks the
// page the way a person reads it rather than the way a map iterates.
func translatableFields[T crud.Entity]() []crud.Field {
	var out []crud.Field
	for _, f := range crud.Fields[T]() {
		if f.Translatable {
			out = append(out, f)
		}
	}
	return out
}

func (s *specSource[T]) Fields() []string {
	var out []string
	for _, f := range translatableFields[T]() {
		out = append(out, f.Name)
	}
	return out
}

func (s *specSource[T]) RichText() map[string]bool {
	out := map[string]bool{}
	for _, f := range translatableFields[T]() {
		out[f.Name] = f.Widget == "richtext"
	}
	return out
}

func (s *specSource[T]) Rows(ctx context.Context, tx db.Tx[db.Tenant], ids []uuid.UUID) ([]SourceRow, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	// The one read here that is not crud.List, because the set is a list of ids
	// rather than an equality filter and crud.Query spells only equalities. It
	// goes through the same connection under the caller's transaction, which is
	// where row-level security lives, and keeps crud's own rule that a
	// soft-deleted row is not found.
	var rows []T
	if err := tx.DB().Where("deleted_at IS NULL AND id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, crud.Classify(err)
	}
	out := make([]SourceRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.lift(row))
	}
	return out, nil
}

func (s *specSource[T]) Page(ctx context.Context, tx db.Tx[db.Tenant], limit, offset int) ([]SourceRow, int64, error) {
	rows, total, err := crud.List[T](tx, crud.Query{Limit: limit, Offset: offset})
	if err != nil {
		return nil, 0, err
	}
	out := make([]SourceRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.lift(row))
	}
	return out, total, nil
}

// lift reads the translatable fields out of one entity value by the field index
// the schema derived — the same mechanism kit/rest's PATCH merge uses to write
// one back, and the only reflection in this file.
func (s *specSource[T]) lift(row T) SourceRow {
	base := entity.BaseOf(row)
	out := SourceRow{ID: base.ID, UpdatedAt: base.UpdatedAt, Values: map[string]string{}}
	v := reflect.ValueOf(row)
	for _, f := range translatableFields[T]() {
		fv := fieldAt(v, f.Index)
		if fv.Kind() == reflect.String {
			out.Values[f.Name] = fv.String()
		}
	}
	return out
}

// fieldAt walks one field's index into a struct pointer, and answers the zero
// Value for anything it cannot reach: a field the index does not lead to is a
// field with no text, which is what a missing translation of it reads as.
//
// The pointer is dereferenced before the first index and between every step
// after it: the index crud derived is relative to the struct behind the entity
// pointer, so a walk that started at the pointer itself would stop at its first
// step — which is exactly how `lift` came to answer an empty source text for
// every field of every record, quietly, until a read door asked the question.
func fieldAt(v reflect.Value, index []int) reflect.Value {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	for i, x := range index {
		if i > 0 {
			for v.Kind() == reflect.Pointer {
				if v.IsNil() {
					return reflect.Value{}
				}
				v = v.Elem()
			}
		}
		if v.Kind() != reflect.Struct || x >= v.NumField() {
			return reflect.Value{}
		}
		v = v.Field(x)
	}
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	return v
}
