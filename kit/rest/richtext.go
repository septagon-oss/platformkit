package rest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// prepareRichText runs after the root's row lock and before any database write.
// columns is nil for create; PATCH only prepares fields the request submitted.
//
// Handing the references to the file module is the second half of this, and it
// happens here for one reason: this is the moment the record's own body has been
// accepted and its row is locked, in the transaction that is about to write it.
// A use recorded afterwards, in a subscriber, is a use that exists a commit later
// than the body that created it — long enough for a sweep to have already decided
// nobody was reading the file, and the file would be gone from under a published
// page. An id is read off the entity because both doors stamp it before they
// write: createRow chooses the id a create will answer with, and an update reads
// the row under its lock, so a body is always filed under the record it became.
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
		// The document Prepare just parsed, and not a scan of the string: what is
		// recorded as a use is exactly what this format can render, and a use for
		// something the renderer would drop is a use that keeps a file alive for
		// a body that never showed it.
		d, _ := richtext.Parse(normal)
		refs := richtext.References(d)
		ids := make([]uuid.UUID, 0, len(refs))
		for _, ref := range refs {
			ids = append(ids, ref.ID)
		}
		if err := s.recordUses(ctx, tx, e, field, ids); err != nil {
			return err
		}
	}
	return nil
}

// recordUses files what one richtext field of this record shows. One field, one
// record, one call: the port's input names the field, so the two doors and the
// delete cannot invent three shapes of the same sentence.
func (s Spec[T]) recordUses(ctx context.Context, tx db.Tx[db.Tenant], e T, field crud.Field, ids []uuid.UUID) error {
	if err := s.fileUses().SetUses(ctx, tx, UsesInput{
		Module: s.Module, Entity: s.Entity, Field: field.Column,
		Locale: recordLocale(ctx), Record: entity.BaseOf(e).ID, Files: ids,
	}); err != nil {
		return fmt.Errorf("richtext field %s: %w", field.Name, err)
	}
	return nil
}

// clearRichTextUses ends every use this record's richtext fields filed. The
// rewrite is the empty set, per field, because a use is keyed by field and locale
// and a record that is gone shows nothing in any of them. The locale is the one
// the write filed under — recordLocale answers the same question at both doors,
// and the day a request's own language becomes that argument (T-0190) the delete
// asks it in the request's language too, which is what kit/rest's own calls will
// then have used.
func (s Spec[T]) clearRichTextUses(ctx context.Context, tx db.Tx[db.Tenant], e T) error {
	for _, field := range crud.Fields[T]() {
		if field.Widget != "richtext" {
			continue
		}
		if err := s.recordUses(ctx, tx, e, field, nil); err != nil {
			return err
		}
	}
	return nil
}

// RecordNoUses is the port a resource gets when nobody wired it to a file
// module: it accepts every body and records nothing, which is what an
// application with no release sweep costs nobody. It is named for what it does
// rather than for what it refuses, and the limit is stated where a reader will
// find it: the moment a sweep exists that releases a file nobody reads, a
// resource left on this port is a resource whose images that sweep is entitled
// to delete out from under a published page. Mount logs it once, naming the
// module and the entity, and the day the sweep lands is the day this default
// becomes the mount-time refusal SPECIFY.md §4.4 asked for — which is why this
// is a named type and not a nil check.
type RecordNoUses struct{}

func (RecordNoUses) SetUses(ctx context.Context, tx db.Tx[db.Tenant], in UsesInput) error {
	return nil
}

// fileUses is the port this Spec records with, which is RecordNoUses when
// nobody wired one: check logs that once at mount, and a value receiver means
// the log is the only thing mount can change, so the answer is taken here
// rather than written into the Spec.
func (s Spec[T]) fileUses() FileUses {
	if s.FileUses == nil {
		return RecordNoUses{}
	}
	return s.FileUses
}

// recordLocale is the locale a use is filed under: the language this tenant
// serves a request that asked for nothing, which is the one the kernel already
// answers a missing Accept-Language with. A tenant that declared no languages
// has no default to file under, and a use has to name one — so "und", the
// register's own tag for "undetermined" and not a language this package
// invented. T-0190 is what turns a request's own language into this argument.
func recordLocale(ctx context.Context) string {
	if t, ok := tenancy.FromContext(ctx); ok && t.Languages != nil && t.Languages.Default != "" {
		return t.Languages.Default
	}
	return "und"
}

type richTextFieldError struct {
	field   string
	refused *richtext.Refused
}

func (e *richTextFieldError) Error() string { return fmt.Sprintf("%s: %s", e.field, e.refused.Error()) }
func (e *richTextFieldError) Unwrap() error { return crud.ErrInvalid }

// richTextProblem is the answer a request gets, with the refusals still in it.
// The Problem is what the API serialises, in the language this repository
// promises its wire contract in; the issues beside it are for the screen that
// redraws the form in the language the reader asked with. See FieldErrorsIn.
type richTextProblem struct {
	*problem.Problem
	field  string
	issues []richtext.Issue
}
