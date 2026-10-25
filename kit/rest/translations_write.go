// The write half of the translation door of kit/rest: what a record does when a
// person says "this is the Portuguese of it".
//
// Four doors, all of them commands of the record rather than routes of the
// translation module: the guard is the record's own write permission, the source row
// is locked before its text is lifted, and the value is validated by the field's own
// rules. README.md, "Translating a record" says why that placement is the design, and
// why the two verbs name the noun they act on.
package rest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/richtext"
)

// translateBody is the write of one record's translation: the language, the text
// of each field, and the revision the caller read each field at.
//
// The expected revisions travel beside the values rather than in a header
// because they are per field: a side-by-side editor has six rows on the screen,
// each with its own revision, and one lost row must not be the reason the other
// five silently overwrite what somebody else just typed.
type translateBody struct {
	Lang   string            `json:"lang" doc:"The language being written: a tag this tenant is served in, other than its own"`
	Values map[string]string `json:"values" doc:"The text of each translatable field, keyed by field name"`
	// Expected is the revision of each field as the caller read it, 0 for a
	// field with no translation yet. A field whose stored revision differs is a
	// 409 that writes nothing, including none of the other fields in this body.
	Expected map[string]int64 `json:"expected,omitempty" doc:"Each field's revision as read; 0 for a field with no translation yet"`
}

// translationFieldsBody is the body of the three doors that act on a set of
// fields rather than on their text: review, suggest and untranslate. An empty
// Fields is every field of the record in that language — the reviewer who
// checked the whole record should not have to know its field names.
type translationFieldsBody struct {
	Lang     string           `json:"lang" doc:"The language this acts on: a tag this tenant is served in, other than its own"`
	Fields   []string         `json:"fields,omitempty" doc:"The field names; empty for every translatable field of the record"`
	Expected map[string]int64 `json:"expected,omitempty" doc:"Each field's revision as read; 0 for a field with no translation yet"`
}

// mountTranslationDoors registers the four commands. It is called from Mount for
// a Spec whose entity declares a translatable field and for no other: a resource
// with nothing to translate would otherwise advertise a door that can only be
// refused, which is the same rule that keeps `?lang=` off a plain resource's
// reads.
//
// None of them declares an event extension, and that is not an oversight: the
// event is `translation.updated`, which belongs to whichever module the
// composition wired behind the port and is declared in that module's manifest.
// The kernel's route knows the port, not the module behind it, and a route that
// promised an event it does not itself publish is the lie the boot gate exists to
// catch.
func (s Spec[T]) mountTranslationDoors(surfaces httpx.Surfaces) {
	rich := (&specSource[T]{spec: s}).RichText()
	values, expected := s.translationKeys(rich)
	Command(surfaces, s, "translate",
		"Translate a "+s.Entity,
		"Writes this language's text of the named fields, stamped as typed by a person. The record's own fields are untouched.",
		nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in translateBody) (T, error) {
			return s.translateRow(ctx, tx, id, in)
		}, CommandOptions{MapArgs: &MapArgs{Args: map[string]crud.MapArg{
			"values":   {Elem: crud.TypeString, Keys: values},
			"expected": {Elem: crud.TypeInt, Keys: expected},
		}}})
	Command(surfaces, s, "review-translation",
		"Mark a "+s.Entity+"'s translation reviewed",
		"Names the fields a person has checked against the source. A field whose source has moved since it was translated is refused.",
		nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in translationFieldsBody) (T, error) {
			return s.reviewTranslation(ctx, tx, id, in)
		}, CommandOptions{})
	Command(surfaces, s, "suggest-translation",
		"Ask the machine for a draft of a "+s.Entity,
		"Saves the machine's text as a draft nobody has reviewed; it is never served publicly until a person marks it reviewed. Refuses, writing nothing, when the installation names no machine translator.",
		nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in translationFieldsBody) (T, error) {
			return s.suggestTranslation(ctx, tx, id, in)
		}, CommandOptions{})
	Command(surfaces, s, "untranslate",
		"Remove a "+s.Entity+"'s translation",
		"Deletes this language's rows for the named fields, or for every field when none is named. The record itself is untouched.",
		nil,
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in translationFieldsBody) (T, error) {
			return s.untranslateRow(ctx, tx, id, in)
		}, CommandOptions{})
}

// translationDoor is the half all four commands share, and the order in it is the
// whole safety argument: lock the record, recheck whose it is, settle the language
// question, and only then hand anybody the source.
//
// The recheck is not decoration: `crud.GetForUpdate` locks without filtering, because
// a table whose read policy shows the row to every tenant answers a foreign read with
// the row itself, and it is `RecheckTenant` that refuses — the reasoning that put it
// in `updateRow` and `deleteRow`, one step earlier. The language is checked against
// the tenant's own declaration, so nothing is filed in a language no reader can be
// routed to, nor written over the source.
func (s Spec[T]) translationDoor(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, lang string) (T, SourceRow, error) {
	e, err := crud.GetForUpdate[T](tx, id)
	if err != nil {
		return e, SourceRow{}, err
	}
	if err := crud.RecheckTenant(tx, e); err != nil {
		return e, SourceRow{}, err
	}
	src := (&specSource[T]{spec: s}).lift(e)
	if lang == "" {
		return e, src, fmt.Errorf("%w: no language was named", crud.ErrInvalid)
	}
	languages := db.TenantOf(tx).Languages
	if !slices.Contains(languages.Preferred(), lang) {
		return e, src, fmt.Errorf("%w: %s is not a language this tenant is served in, which speaks %s",
			crud.ErrInvalid, lang, strings.Join(languages.Preferred(), ", "))
	}
	if lang == languages.Default {
		return e, src, fmt.Errorf("%w: %s is this tenant's own language; write the %s itself, not a translation of it",
			crud.ErrInvalid, lang, s.Entity)
	}
	return e, src, nil
}

// translationKeys declares the keys the translate body's two maps take: one per field
// this entity says in another language, in the order its own schema declares them.
// Nothing else could: `Values map[string]string` says only "text, keyed by a string",
// and no reflection over it can learn that this entity has a title and a body, so a
// schema derived from the Go type offered a map with no keys — a form with nothing to
// fill in. The keys come from the entity, beside the one fact a form needs from the
// field's schema: a richtext body is typed into a prose box and a title into a line.
// `Expected` carries the same keys as hidden revision carriers, because a form that
// cannot say which revision it read saves twice or 409s.
func (s Spec[T]) translationKeys(rich map[string]bool) (values, expected []crud.MapKey) {
	for _, f := range translatableFields[T]() {
		widget := "text"
		if rich[f.Name] {
			widget = "textarea"
		}
		values = append(values, crud.MapKey{Name: f.Name, Label: display.FieldLabel(f), Widget: widget})
		expected = append(expected, crud.MapKey{
			Name: f.Name, Label: display.FieldLabel(f), Widget: "hidden", From: entity.KeyFromRevision,
		})
	}
	return values, expected
}

func (s Spec[T]) translateRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in translateBody) (T, error) {
	e, src, err := s.translationDoor(ctx, tx, id, in.Lang)
	if err != nil {
		return e, err
	}
	if len(in.Values) == 0 {
		return e, fmt.Errorf("%w: name at least one field to translate", crud.ErrInvalid)
	}
	rich := (&specSource[T]{spec: s}).RichText()
	values := make(map[string]string, len(in.Values))
	for name, typed := range in.Values {
		f, ok := translatableField[T](name)
		if !ok {
			// Refused rather than ignored: a body that names `summary` when the
			// entity's field is `title` would otherwise file text under a name no
			// read will ever ask for, which is a row nothing can find or delete.
			return e, fmt.Errorf("%w: %s is not a translatable field of %s", crud.ErrInvalid, name, s.Entity)
		}
		values[name], err = s.prepareTranslated(ctx, tx, f, rich[name], typed)
		if err != nil {
			return e, err
		}
	}
	return e, s.Translations.Save(ctx, tx, SaveQuery{
		Module: s.Module, Entity: s.Entity, Locale: in.Lang, RecordID: id,
		Values: values, Expected: in.Expected, Source: src.Values,
		Origin: OriginHuman, RichText: rich,
	})
}

func (s Spec[T]) reviewTranslation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in translationFieldsBody) (T, error) {
	e, src, err := s.translationDoor(ctx, tx, id, in.Lang)
	if err != nil {
		return e, err
	}
	// The source is the record's own text as this transaction holds it under the
	// lock, which is what makes "I checked this against the source" a claim about
	// the source that is true at commit. The port refuses a field it was given no
	// source for rather than stamping it anyway.
	return e, s.Translations.Review(ctx, tx, ReviewQuery{
		Module: s.Module, Entity: s.Entity, Locale: in.Lang, RecordID: id,
		Fields: in.Fields, Expected: in.Expected, Source: src.Values,
		RichText: (&specSource[T]{spec: s}).RichText(),
	})
}

func (s Spec[T]) suggestTranslation(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in translationFieldsBody) (T, error) {
	e, src, err := s.translationDoor(ctx, tx, id, in.Lang)
	if err != nil {
		return e, err
	}
	languages := db.TenantOf(tx).Languages
	return e, s.Translations.Suggest(ctx, tx, SuggestQuery{
		Module: s.Module, Entity: s.Entity, Locale: in.Lang, RecordID: id,
		Fields: in.Fields, Expected: in.Expected, Source: src.Values,
		RichText: (&specSource[T]{spec: s}).RichText(),
		From:     languages.Default,
	})
}

func (s Spec[T]) untranslateRow(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in translationFieldsBody) (T, error) {
	e, src, err := s.translationDoor(ctx, tx, id, in.Lang)
	if err != nil {
		return e, err
	}
	return e, s.Translations.Untranslate(ctx, tx, ReviewQuery{
		Module: s.Module, Entity: s.Entity, Locale: in.Lang, RecordID: id,
		Fields: in.Fields, Expected: in.Expected, Source: src.Values,
		RichText: (&specSource[T]{spec: s}).RichText(),
	})
}

// translatedPatchInput is the record's PATCH with the language question added.
// It exists because `?lang=` is one contract, not a read-only one: writing a
// record `?lang=<other>` means "this is that language's text of these fields",
// and a parameter that silently wrote the Portuguese into the English column
// while claiming 200 would file text under the wrong language and destroy the
// source it should have been measured against.
//
// It is a separate shape from patchInput for the same reason the two read doors
// are: a resource with no translatable field must not advertise a `?lang=` its
// PATCH would have to refuse.
type translatedPatchInput struct {
	ID   uuid.UUID      `path:"id" format:"uuid" doc:"The row's id"`
	Lang string         `query:"lang" doc:"Write this language's translation of the body's fields instead of the record itself; the tenant's own language, or none, writes the record"`
	Body map[string]any `doc:"The translatable fields to write in that language"`
	// IfMatch is the row's ETag as the caller read it, honoured by both halves of
	// this door: it conditions the write of the record, and it conditions the write
	// of a translation, because a translation is measured against the source its
	// author read. Same rule, same refusal, as the plain PATCH's parameter.
	IfMatch string `header:"If-Match" doc:"The row's ETag, as read; a tag the row has passed refuses the write with 412. A resource whose entity carries no write count answers 422."`
}

// localePatch is the PATCH of a body that is somebody's Portuguese, not the
// record's English: the same door translate walks — lock, tenant recheck,
// language check against the tenant's declaration, the field's own rules — and
// the same port call. The source row is never touched; the body must name only
// translatable fields, because a mixed body would be half a translation and
// half a source edit, and the caller should find out which one they asked for
// before either of them is written.
//
// A locale write carries no expected revisions — the body is the field map, not
// the translate body's three-part shape — so it creates a translation, and a
// field that already has one is the 409 that points at /translate, which names
// the revision it overwrites. The row's own tag is the one condition it does
// carry: the source this text was drafted against is the row the tag names, and a
// row that moved since is a source the caller never held.
func (s Spec[T]) localePatch(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, lang string, body map[string]any, ifMatch string) (T, error) {
	e, src, err := s.translationDoor(ctx, tx, id, lang)
	if err != nil {
		return e, err
	}
	// Asked after the language question and before one word of the body is read:
	// a body in a language this tenant is not served in is the caller's mistake to
	// fix, and a 412 about a revision would send them to re-read a row they were
	// never allowed to translate.
	if err := refuseStaleTag(ifMatch, s.revisionOf(crud.Fields[T](), e)); err != nil {
		return e, err
	}
	if len(body) == 0 {
		return e, fmt.Errorf("%w: name at least one field to translate", crud.ErrInvalid)
	}
	rich := (&specSource[T]{spec: s}).RichText()
	values := make(map[string]string, len(body))
	for name, typed := range body {
		f, ok := translatableField[T](name)
		if !ok {
			return e, fmt.Errorf("%w: %s is not a translatable field of %s; a ?lang=%s write writes only translations — the record itself is written without the parameter",
				crud.ErrInvalid, name, s.Entity, lang)
		}
		text, ok := typed.(string)
		if !ok {
			return e, fmt.Errorf("%w: a translation of %s must be text", crud.ErrInvalid, name)
		}
		values[name], err = s.prepareTranslated(ctx, tx, f, rich[name], text)
		if err != nil {
			return e, err
		}
	}
	return e, s.Translations.Save(ctx, tx, SaveQuery{
		Module: s.Module, Entity: s.Entity, Locale: lang, RecordID: id,
		Values: values, Source: src.Values,
		Origin: OriginHuman, RichText: rich,
	})
}

// prepareTranslated runs the field's own rules over the text a person typed in
// another language: the same normalisation, the same ceiling and the same image
// resolution the record's own write runs, so a translation cannot reach a state
// the source could never be saved in. A richtext body is stored normalised for
// the same reason the source is: two spellings of one document must not read as
// two translations.
//
// The plain branch is not "return whatever arrived". The generic write doors
// bind the body into the entity, so the field's declared maxLength and enum are
// enforced by the schema the body is validated against; a translated body is a
// map of free strings, and nothing beside this function would ever look at the
// field's own ceiling again. A Portuguese title longer than the column is the
// same mistake in another language, and the door that made it is the one that
// has to refuse it.
func (s Spec[T]) prepareTranslated(ctx context.Context, tx db.Tx[db.Tenant], f crud.Field, isRichText bool, typed string) (string, error) {
	if !isRichText {
		if err := fieldRule(f).Check(typed); err != nil {
			return "", err
		}
		return typed, nil
	}
	normal, err := richtext.Prepare(ctx, tx, typed, s.RichTextFiles, f.MaxLength)
	if err != nil {
		var refused *richtext.Refused
		if errors.As(err, &refused) {
			return "", &richTextFieldError{f.Name, refused}
		}
		return "", fmt.Errorf("richtext field %s: %w", f.Name, err)
	}
	return normal, nil
}

// translatableField finds one field by its JSON name among the ones the entity
// declares translatable. A field that exists and is not translatable answers "no
// such field" here on purpose: the text of a title lives in the row, so a door
// that accepted `title` for an entity whose title is not translatable would be a
// door that writes a translation nothing reads and nothing deletes.
func translatableField[T crud.Entity](name string) (crud.Field, bool) {
	for _, f := range translatableFields[T]() {
		if f.Name == name {
			return f, true
		}
	}
	return crud.Field{}, false
}
