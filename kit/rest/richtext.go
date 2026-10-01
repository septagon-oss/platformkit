package rest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/richtext"
)

// prepareRichText runs after the root's row lock and before any database write.
// columns is nil for create; PATCH only prepares fields the request submitted.
func (s Spec[T]) prepareRichText(ctx context.Context, tx db.Tx[db.Tenant], e T, columns []string) error {
	value := reflect.ValueOf(e).Elem()
	for _, field := range crud.Fields[T]() {
		if field.Widget != "richtext" || (columns != nil && !slices.Contains(columns, field.Column)) {
			continue
		}
		v := value.FieldByIndex(field.Index)
		normal, err := richtext.Prepare(ctx, tx, v.String(), s.RichTextFiles, field.MaxLength)
		if err != nil {
			var refused *richtext.Refused
			if errors.As(err, &refused) {
				return &richTextFieldError{field.Name, refused}
			}
			return fmt.Errorf("richtext field %s: %w", field.Name, err)
		}
		v.SetString(normal)
	}
	return nil
}

type richTextFieldError struct {
	field   string
	refused *richtext.Refused
}

func (e *richTextFieldError) Error() string { return fmt.Sprintf("%s: %s", e.field, e.refused.Error()) }
func (e *richTextFieldError) Unwrap() error { return crud.ErrInvalid }
