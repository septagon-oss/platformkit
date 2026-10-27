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

// prepareFiles runs after the root's row lock and before any database write, and
// files every file id this record is about to show — the ones a richtext body
// names and the ones a picture field holds. columns is nil for create; PATCH only
// prepares fields the request submitted.
//
// A picture is walked with a body because both are ids this record shows, and a use
// filed for one and not the other is a sweep deleting a cover out from under a page
// that still names it. The filing happens here rather than in a subscriber because
// this is the moment the body has been accepted and the row locked, in the
// transaction about to write it: a use recorded a commit later is one a sweep could
// already have decided nobody was reading. Both doors stamp the id before they write.
func (s Spec[T]) prepareFiles(ctx context.Context, tx db.Tx[db.Tenant], e T, columns []string) error {
	value := reflect.ValueOf(e).Elem()
	// Asked once per write, and once there is anything to ask for: the audience is a
	// fact of the record, not of the field, and two fields of one record cannot
	// disagree about who it is showing them to. See Spec.Audience for why the answer
	// has to be had here rather than after the write.
	public, err := s.audience(ctx, tx, e)
	if err != nil {
		return err
	}
	// Two passes over the fields that show a file, and the reason is the file ledger. Each
	// field filed is one call that locks the files it names, so a save with two of
	// them locks in whatever order the entity declares its fields in — and two records
	// sharing two pictures, each naming them in the other's order, then hold one and
	// wait for the other, which Postgres ends by killing one save over. The whole save
	// is named to the ledger once, before anything is filed, so that the locks of a
	// save are taken in one pass whose order the ledger decides. See FileUsesOrder.
	var pending []pendingUses
	whole := SaveUses{Module: s.Module, Entity: s.Entity, Record: entity.BaseOf(e).ID}
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
		whole.Files = append(whole.Files, ids...)
		pending = append(pending, pendingUses{name: field.Name, in: UsesInput{
			Module: s.Module, Entity: s.Entity, Field: field.Column,
			Locale: recordLocale(ctx), Record: whole.Record, Files: ids, Public: public,
		}})
	}
	for _, field := range crud.Fields[T]() {
		if field.Type == entity.TypePicture || !entity.PicturesAre(field.Present) ||
			(columns != nil && !slices.Contains(columns, field.Column)) {
			continue
		}
		ids := pictureIDs(value.FieldByIndex(field.Index))
		whole.Files = append(whole.Files, ids...)
		pending = append(pending, pendingUses{name: field.Name, in: UsesInput{
			Module: s.Module, Entity: s.Entity, Field: field.Column,
			Locale: recordLocale(ctx), Record: whole.Record, Files: ids, Public: public,
		}})
	}
	if err := s.orderUses(ctx, tx, whole); err != nil {
		return err
	}
	for _, p := range pending {
		if err := s.fileUses().SetUses(ctx, tx, p.in); err != nil {
			return fmt.Errorf("field %s: %w", p.name, err)
		}
	}
	return nil
}

// pictureIDs is the file ids one picture field holds: none, one, or the list its
// owner ordered. A nil id and an absent picture are the same answer, because the
// record shows nothing either way and the ledger's own diff is what ends the use
// a previous save filed.
//
// The column is read through reflection rather than a type assertion on one Go
// type because the field is the entity's to write: `*uuid.UUID`, a named slice
// over []uuid.UUID and their pointer forms all hold the same fact, and a picture
// column that silently filed nothing would be a picture a sweep is entitled to
// delete.
func pictureIDs(v reflect.Value) []uuid.UUID {
	for v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	var ids []uuid.UUID
	add := func(id uuid.UUID) {
		if id != uuid.Nil {
			ids = append(ids, id)
		}
	}
	if v.Kind() == reflect.Slice {
		for i := range v.Len() {
			one(v.Index(i), add)
		}
		return ids
	}
	one(v, add)
	return ids
}

func one(v reflect.Value, add func(uuid.UUID)) {
	switch id := v.Interface().(type) {
	case uuid.UUID:
		add(id)
	case *uuid.UUID:
		if id != nil {
			add(*id)
		}
	}
}

// pendingUses is one field's file references with the name of the field they came
// from, which is what the refusal a filed use produces still has to carry.
type pendingUses struct {
	name string
	in   UsesInput
}

// SaveUses is one record's save in the one sentence the file ledger needs before
// any of it is filed: which record, and every file that save names across every
// richtext and picture field of it. A record being deleted names nothing and says so with
// an empty Files: the ledger knows what the record shows, and it is the ledger that
// has to lock those rows in one pass rather than one field at a time.
type SaveUses struct {
	Module string
	Entity string
	Record uuid.UUID
	Files  []uuid.UUID
}

// FileUsesOrder is the optional half of FileUses: the wiring that answers it is told
// a whole save, in one call, before a single field of it is filed.
//
// A wiring that does not answer it is not refused and not warned about, and the
// reason is who is without it. RecordNoUses files no uses, so it locks no file row
// and has nothing to order; a deployment that wired its own recorder before this
// capability existed keeps the one-pass-per-field order it always had, which is the
// order that can collide and is not a new hazard this kit introduced. What a resource
// with two richtext fields should be wired to is a recorder that answers.
type FileUsesOrder interface {
	OrderUses(ctx context.Context, tx db.Tx[db.Tenant], in SaveUses) error
}

// orderUses tells the ledger the whole save. It asks nothing of a wiring that
// records no uses, and nothing of one that cannot be told; the error it produces
// names the record whose files it could not order, because the caller is about to
// refuse the write and has to say whose audience was at stake.
func (s Spec[T]) orderUses(ctx context.Context, tx db.Tx[db.Tenant], in SaveUses) error {
	if s.FileUses == nil {
		return nil
	}
	ordered, ok := s.FileUses.(FileUsesOrder)
	if !ok {
		return nil
	}
	if err := ordered.OrderUses(ctx, tx, in); err != nil {
		return fmt.Errorf("who %s/%s %s shows: %w", in.Module, in.Entity, in.Record, err)
	}
	return nil
}

// audience is Spec.Audience asked, with "nobody" as the answer for the Spec that
// declares no public face. It is asked once, before any use is filed, so that an
// error costs the write nothing: a record whose audience cannot be answered never
// has bytes promoted by this write, and nothing half-filed survives it.
func (s Spec[T]) audience(ctx context.Context, tx db.Tx[db.Tenant], e T) (bool, error) {
	if s.Audience == nil {
		return false, nil
	}
	public, err := s.Audience(ctx, tx, e)
	if err != nil {
		return false, fmt.Errorf("who %s/%s shows its files to: %w", s.Module, s.Entity, err)
	}
	return public, nil
}

// recordUses files what one field of this record shows. One field, one record,
// one call: the port's input names the field, so the two doors and the delete
// cannot invent three shapes of the same sentence.
func (s Spec[T]) recordUses(ctx context.Context, tx db.Tx[db.Tenant], e T, field crud.Field, ids []uuid.UUID, public bool) error {
	if err := s.fileUses().SetUses(ctx, tx, UsesInput{
		Module: s.Module, Entity: s.Entity, Field: field.Column,
		Locale: recordLocale(ctx), Record: entity.BaseOf(e).ID, Files: ids, Public: public,
	}); err != nil {
		return fmt.Errorf("field %s: %w", field.Name, err)
	}
	return nil
}

// clearFileUses ends every use this record's richtext and picture fields filed. The
// rewrite is the empty set, per field, because a use is keyed by field and locale
// and a record that is gone shows nothing in any of them. The locale is the one
// the write filed under — recordLocale answers the same question at both doors,
// and the day a request's own language becomes that argument the delete
// asks it in the request's language too, which is what kit/rest's own calls will
// then have used.
func (s Spec[T]) clearFileUses(ctx context.Context, tx db.Tx[db.Tenant], e T) error {
	// The record's whole ledger is named first, with no files of its own, so that a
	// delete ending uses of two pictures takes both locks in one ascending pass and
	// not one pass per field. What the record shows is what the ledger holds; the save
	// is about to end all of it.
	if err := s.orderUses(ctx, tx, SaveUses{
		Module: s.Module, Entity: s.Entity, Record: entity.BaseOf(e).ID,
	}); err != nil {
		return err
	}
	for _, field := range crud.Fields[T]() {
		if field.Type == entity.TypePicture || (field.Widget != "richtext" && !entity.PicturesAre(field.Present)) {
			continue
		}
		if err := s.recordUses(ctx, tx, e, field, nil, false); err != nil {
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
// module and the entity, and the day the sweep lands is the day mounting a
// resource on this port becomes a refusal rather than a logged default — which
// is why this is a named type and not a nil check.
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
// invented. A request's own language is not yet this argument.
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
