// The translation door of kit/rest: what a resource does with a field tagged
// `i18n:"translatable"`.
//
// Two interfaces live here, and the direction each points in is the reason
// neither lives in a module.
//
// Translations is the door a rest.Spec knocks on to read and write one
// record's translations. It is declared in the kernel because the direction of
// the dependency is module→kernel: a module may not be reached from the
// kernel's HTTP layer, so the shape the kernel asks for has to be the kernel's
// own, and modules/translation implements it. The Spec holds it as one field
// wired by hand at composition, the same way RichTextFiles is wired; nothing
// discovers it.
//
// TranslationSource points the other way, and exists because one question is
// unanswerable from the translations table alone. "Missing" means *no row*,
// and no row of which records exist is a fact about the entity's table, which
// belongs to another module. So the row set of the entity arrives at the
// translation module through a port the translation module does not own and
// cannot fill — house rule 5, a higher-tier collaborator reached through a
// port declared here and implemented at composition — and the only reflection
// anywhere is the kernel's own, one line per mounting module:
//
//	Sources: []rest.TranslationSource{rest.TranslationSourceOf(contentSpec)}
//
// There is no registry and no discovery: the composition writes the list and
// the compiler checks it.
package rest

import (
	"cmp"
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// The five states one translatable field of one response can be in. Four of
// them belong to a row; missing belongs to the pair with no row, which is why
// the storage column holds three values and a response names five.
const (
	// FallbackMissing: this locale has no row for the field, so the value shown
	// is the source.
	FallbackMissing = "missing"
	// FallbackOutdated: the row exists, but its source has moved since. The
	// value shown is the translation, and it is behind.
	FallbackOutdated = "outdated"
	// FallbackMachine: the workspace sees a machine draft no person has
	// reviewed yet. The value shown is that draft, labelled.
	FallbackMachine = "machine"
	// FallbackWithheld: the only row is an unreviewed machine draft and the
	// door is public, so the value shown is the source. The row is not served
	// and its existence is not announced.
	FallbackWithheld = "withheld"
	// FallbackRemoved: the caller named a language the tenant no longer
	// declares. Rows exist; nothing serves them.
	FallbackRemoved = "removed"
)

// The two origins a translation can arrive from. Provenance is never erased:
// a person who types over a machine draft keeps origin machine and gains a
// reviewed_at, because what a person typed is reviewed by being typed.
const (
	OriginHuman   = "human"
	OriginMachine = "machine"
)

// TranslatedField is one field of one record as one locale has it.
type TranslatedField struct {
	Value string `json:"value"`
	// Status is FallbackOutdated, FallbackMachine or "" — the two states a
	// caller cannot derive from the value alone. A field returned with an
	// empty Status is that locale's own reviewed text: nothing to say, and the
	// field is absent from _i18n.
	Status string `json:"status,omitempty"`
	// Origin and ReviewedAt are what a reviewer sees and a public reader does
	// not: "Machine — needs review" is drawn from these two.
	Origin     string     `json:"origin,omitempty"`
	ReviewedAt *time.Time `json:"reviewedAt,omitempty"`
	// Revision is the row's own, which is what a caller sends back as its
	// expected revision. A field with no row expects 0.
	Revision int64 `json:"revision"`
	// SourceText and SourceHash are the source as this translation saw it. The
	// side-by-side view needs the first to highlight against, and the second
	// to say whether it still matches.
	SourceText string `json:"sourceText,omitempty"`
	SourceHash string `json:"sourceHash,omitempty"`
}

// TranslatedQuery names one locale's rows for a page of records of one entity,
// and — this is the part that is not a filter — the current source text of
// every translatable field of every record named. Outdating is a comparison
// against the source as it stands, and the caller is the one holding the source
// under the row lock; the translation module never reaches into another
// module's table to re-read it.
type TranslatedQuery struct {
	Module, Entity string
	// Locale is the negotiated tag, which is never the tenant's default: a
	// read in the default language asks no question of this table.
	Locale    string
	RecordIDs []uuid.UUID
	// Sources is the current text of each translatable field, per record.
	Sources map[uuid.UUID]map[string]string
	// RichText names which fields are richtext, so the digest this read
	// compares against is the digest the write stored.
	RichText map[string]bool
	// Public is the door. Only the public door withholds an unreviewed machine
	// draft; it is one line inside the one function that answers this, so no
	// door can forget it.
	Public bool
}

// TranslatedRecord is one record's rows in the requested locale. A field with
// no row is simply absent from Fields: the caller falls back to the source and
// says so.
type TranslatedRecord struct {
	ID     uuid.UUID
	Fields map[string]TranslatedField
}

// SaveQuery writes one record's translation of every field named, in one
// locale. Values are raw field values, already validated by the field's own
// rules — kit/rest runs them, because the rules are the field's and the
// translation module has no business knowing what a page is.
type SaveQuery struct {
	Module, Entity, Locale string
	RecordID               uuid.UUID
	Values                 map[string]string
	// Expected maps a field to the revision the caller read, 0 for a field
	// with no row yet. A field whose stored revision differs is crud.ErrConflict
	// and writes nothing.
	Expected map[string]int64
	// Source is the current source text of each field being saved, from the
	// row the caller holds locked. It is what source_text and source_hash are
	// written from, so they describe a source that existed at commit.
	Source   map[string]string
	Origin   string
	RichText map[string]bool
}

// ReviewQuery marks one record's fields reviewed in one locale. Fields is the
// empty set for "every field of this record in this locale".
//
// Source carries the current text of each field because a review is a claim
// about the source: "this translation still says what the source says". The
// caller is the one holding the source row locked, so the source arrives here
// rather than being re-read by a module that has no business reading another
// module's table.
type ReviewQuery struct {
	Module, Entity, Locale string
	RecordID               uuid.UUID
	Fields                 []string
	Expected               map[string]int64
	Source                 map[string]string
	RichText               map[string]bool
}

// SuggestQuery asks the configured machine translator for a draft of one
// record's fields in one locale and saves it as a draft: origin machine, no
// reviewed_at. With no translator configured the installation serves no
// machine translation at all, and the answer is a refusal that writes nothing.
type SuggestQuery struct {
	Module, Entity, Locale string
	RecordID               uuid.UUID
	Fields                 []string
	Expected               map[string]int64
	Source                 map[string]string
	RichText               map[string]bool
	// From is the tenant's default language, which is what the draft is
	// translated out of.
	From string
}

// OverviewState is the closed set the overview counts by.
const (
	StateMissing  = "missing"
	StateOutdated = "outdated"
	StateMachine  = "machine"
	StateComplete = "complete"
)

// OverviewQuery asks what one entity and one locale look like across a page of
// the entity's own rows — which is why it needs the Source port: the page of
// records is the entity's, not the translation table's, and a record with no
// row in this locale is one of the three answers it is being asked for.
type OverviewQuery struct {
	Module, Entity, Locale string
	Limit, Offset          int
	// State, when not "", keeps only the records with at least one field in
	// this state. It is a filter over the derived state, not a column, and the
	// total returned is the total after the filter.
	State string
	// IncludeRemoved asks for rows of locales the tenant no longer declares,
	// which no list shows until somebody asks for them.
	IncludeRemoved bool
	// Languages is the tenant's declaration, so this module can tell a locale
	// the tenant speaks from one it stopped speaking without importing the
	// tenant module.
	Languages []string
}

// OverviewRow is one record: its state per translatable field, and the counts
// the table's column shows. A record the caller cannot see is not in the page
// at all — the page comes from the entity's own live rows — so "gone" is
// answered by absence from the list, not by a flag on it.
type OverviewRow struct {
	ID        uuid.UUID
	UpdatedAt time.Time
	States    map[string]string
	Missing   int
	Outdated  int
	Machine   int
	Complete  int
}

// OverviewCounts is the whole set, which a page of rows cannot answer.
type OverviewCounts struct {
	Missing, Outdated, Machine, Complete int
}

// Translations is the door: two reads and six writes, every one inside the
// caller's transaction, every refusal writing nothing and publishing nothing.
//
// The tenant is never named in a query: it is the transaction's, and
// row-level security is what makes a wrong-tenant read answer "not found"
// rather than answer somebody else's row.
type Translations interface {
	// Translated returns, for each record named, only the fields that have a
	// row in the query's locale, each with the status this door must report.
	Translated(ctx context.Context, tx db.Tx[db.Tenant], q TranslatedQuery) ([]TranslatedRecord, error)

	// Save writes one record's translation of every field named, in locale,
	// after the caller has locked the source row. A value equal to what is
	// stored writes nothing, bumps nothing and publishes nothing. It publishes
	// translation.updated for whatever it changed.
	Save(ctx context.Context, tx db.Tx[db.Tenant], q SaveQuery) error

	// Review marks the named fields — all of them when Fields is empty —
	// reviewed by the caller. A field whose source has moved since it was
	// translated is refused: marking a stale translation reviewed would make
	// the completeness badge a lie. Reviewing what is already reviewed writes
	// nothing and publishes nothing.
	Review(ctx context.Context, tx db.Tx[db.Tenant], q ReviewQuery) error

	// Suggest asks the machine translator for a draft and saves it. It writes
	// nothing when there is no translator, when the provider refuses, or when
	// the provider hands back the text it was given.
	Suggest(ctx context.Context, tx db.Tx[db.Tenant], q SuggestQuery) error

	// Untranslate deletes one record's rows in one locale — a write, so it is
	// audited and publishes translation.updated with the removed status.
	Untranslate(ctx context.Context, tx db.Tx[db.Tenant], q ReviewQuery) error

	// Overview answers one entity and locale over a page of the entity's own
	// rows: each record's per-field state, the totals behind the page, and the
	// total after q.State.
	Overview(ctx context.Context, tx db.Tx[db.Tenant], q OverviewQuery) ([]OverviewRow, OverviewCounts, int64, error)

	// ForgetRecord deletes every translation of one record, in any locale. It
	// is what the record's own delete runs in the same transaction as the row
	// delete, and it is the only thing holding record_id's meaning, since no
	// foreign key can span two modules.
	ForgetRecord(ctx context.Context, tx db.Tx[db.Tenant], module, entity string, recordID uuid.UUID) error

	// MarkOutdated is the source write's half of the staleness rule: a write that
	// changed one translatable field of one record marks that field's translations
	// of it outdated, in the same transaction. The read derives the state from the
	// source it is holding either way; this maintains the column the overview
	// counts, so the overview does not count a paragraph that has been rewritten
	// "up to date".
	MarkOutdated(ctx context.Context, tx db.Tx[db.Tenant], module, entity, field string, recordID uuid.UUID) error
}

// translationFault refuses the two mounts that could only orphan something: a
// translatable field with no port to read or write it — the mount whose `?lang=`
// would answer the source and say nothing, which is the lie a translation feature
// is not allowed to tell — and a port wired to an entity with no translatable
// field, which is a wiring somebody believed about a field tag that is not there.
//
// It is a mount-time panic like the rest of check: both are composition mistakes,
// invisible at runtime until a reader is served the wrong text.
func (s Spec[T]) translationFault() string {
	fields := translatableFields[T]()
	switch {
	case len(fields) > 0 && s.Translations == nil:
		return fmt.Sprintf("field %q is `i18n:\"translatable\"` and the Spec has no Translations port: ?lang= would read the source and call it the translation",
			fields[0].Name)
	case len(fields) == 0 && s.Translations != nil:
		return "Translations is wired to an entity with no `i18n:\"translatable\"` field, so nothing would ever reach it"
	}
	return ""
}

// SourceRow is one live row of the entity, with the current text of each of its
// translatable fields. UpdatedAt is the source row's own, which is the date the
// banner shows — "Source changed on 1 Oct" names when the source moved, and the
// translation row's own updated_at says when the translation was written, which
// is a different fact and the wrong one to show a reviewer.
type SourceRow struct {
	ID        uuid.UUID
	UpdatedAt time.Time
	Values    map[string]string
}

// TranslationSource is the entity's row set, as the translation module has to
// be given it rather than reach for it.
type TranslationSource interface {
	// Module and Entity are the mounted Spec's two own names, so a row set can
	// only ever be matched with the resource that declares it.
	Module() string
	Entity() string
	// Fields names the translatable JSON field names, in the order the Spec
	// declares them — which is the order the side-by-side view walks.
	Fields() []string
	// RichText names which of those fields are richtext, because the rules for
	// hashing and splitting a paragraph belong to that format and to nothing
	// else.
	RichText() map[string]bool
	// Rows answers the named records through crud under the caller's
	// transaction, under row-level security. A record with no row is absent,
	// which is how a deleted source says so.
	Rows(ctx context.Context, tx db.Tx[db.Tenant], ids []uuid.UUID) ([]SourceRow, error)
	// Page is the entity's live rows in its own default order, with the total
	// the overview's pager needs.
	Page(ctx context.Context, tx db.Tx[db.Tenant], limit, offset int) ([]SourceRow, int64, error)
}

// TranslationSourceOf is the generic implementation of TranslationSource over a
// mounted Spec: the entity's own rows, read through crud.List under the caller's
// transaction, with the translatable fields lifted out by the field index the
// schema already carries. It is a function and not a registry: the composition
// names each entity it wants translated, and a module it does not name is not.
func TranslationSourceOf[T crud.Entity](s Spec[T]) TranslationSource {
	return &specSource[T]{spec: s}
}

// translatedIDInput and translatedListInput are the two read doors of a
// resource that declares a translatable field. They exist as two shapes rather
// than one, with ?lang= on every GET in the installation, because a parameter
// nothing reads is a parameter a caller will send and a client will trust: for a
// resource with no translatable field the honest answer to ?lang= is that the
// document does not offer it.
type translatedIDInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The row's id"`
	// Lang is spelled out beside the path id rather than embedded over idInput
	// because huma binds the parameters of the struct it is handed, and an
	// embedded struct's fields are not its own — a case that embeds them reads a
	// zero id and a blank language, and both answers look like a 404 and an
	// untranslated row rather than like a wiring mistake.
	Lang string `query:"lang" doc:"Answer in this language: a translatable field with no row in it is served in the tenant's own, named in _i18n"`
}

type translatedListInput struct {
	Limit  int      `query:"limit" default:"50" minimum:"1" maximum:"200" doc:"Rows per page"`
	Offset int      `query:"offset" minimum:"0" doc:"Rows to skip"`
	Sort   string   `query:"sort" doc:"A field name, or a field name prefixed with - for descending"`
	Filter []string `query:"filter" doc:"field:value, repeated"`
	Lang   string   `query:"lang" doc:"Answer in this language: a translatable field with no row in it is served in the tenant's own, named in _i18n"`
}

// page is the same four parameters as listInput, which is the shape the paging
// and the filter parsing are written against.
func (in *translatedListInput) page() listInput {
	return listInput{Limit: in.Limit, Offset: in.Offset, Sort: in.Sort, Filter: in.Filter}
}

// TranslatedItem and TranslatedPage are the two read doors of a resource with a
// translatable field. The header they carry is not on the shared Item and Page,
// and that is the point: a response is in a language only when something could
// answer in one, and an installation whose every entity is monolingual would
// otherwise document a header nothing ever sets.
type TranslatedItem[T any] struct {
	Body T
	// ContentLanguage is the language the rows in this response are written in,
	// which is the tag that was asked for when the tenant speaks it.
	ContentLanguage string `header:"Content-Language"`
}

type TranslatedPage[T any] struct {
	ContentLanguage string `header:"Content-Language"`
	Body            struct {
		Items  []T   `json:"items"`
		Total  int64 `json:"total"`
		Limit  int   `json:"limit"`
		Offset int   `json:"offset"`
	}
}

// pageRows and itemRow are the two translated doors; plainRows and oneRow are
// what the same resource looks like with no language asked. They are factored out
// of Mount so the four registrations are three lines each and not four copies of
// the paging, the read and the error mapping. Each translated one answers, as its
// second result, the language the response is written in — the one asked for or
// the one negotiated from the caller's own signals — which is what decides
// whether the response may claim a Content-Language at all and what it names.
func (s Spec[T]) pageRows(ctx context.Context, in listInput, schema crud.Schema, lang string) (*TranslatedPage[T], string, error) {
	tx, err := transaction(ctx)
	if err != nil {
		return nil, "", err
	}
	q, err := in.query(schema.Fields)
	if err != nil {
		return nil, "", Fault(err)
	}
	items, total, err := crud.List[T](tx, q)
	if err != nil {
		return nil, "", err
	}
	out := &TranslatedPage[T]{}
	out.Body.Items, out.Body.Total, out.Body.Limit, out.Body.Offset = items, total, q.Limit, q.Offset
	served, err := s.overlay(ctx, tx, lang, out.Body.Items)
	if err != nil {
		return nil, "", err
	}
	return out, served, nil
}

func (s Spec[T]) plainRows(ctx context.Context, in listInput, schema crud.Schema) (*Page[T], error) {
	tx, err := transaction(ctx)
	if err != nil {
		return nil, err
	}
	q, err := in.query(schema.Fields)
	if err != nil {
		return nil, Fault(err)
	}
	items, total, err := crud.List[T](tx, q)
	if err != nil {
		return nil, Fault(err)
	}
	out := &Page[T]{}
	out.Body.Items, out.Body.Total, out.Body.Limit, out.Body.Offset = items, total, q.Limit, q.Offset
	return out, nil
}

func (s Spec[T]) itemRow(ctx context.Context, id uuid.UUID, lang string) (*TranslatedItem[T], string, error) {
	tx, err := transaction(ctx)
	if err != nil {
		return nil, "", err
	}
	e, err := crud.Get[T](tx, id)
	if err != nil {
		return nil, "", Fault(err)
	}
	// The entity is a pointer, so the overlay writes into the same value this
	// response carries: the language replaces the field, and `_i18n` says which
	// field it could not.
	out := &TranslatedItem[T]{Body: e}
	served, err := s.overlay(ctx, tx, lang, []T{e})
	if err != nil {
		return nil, "", err
	}
	return out, served, nil
}

func (s Spec[T]) oneRow(ctx context.Context, id uuid.UUID) (T, error) {
	var e T
	tx, err := transaction(ctx)
	if err != nil {
		return e, err
	}
	e, err = crud.Get[T](tx, id)
	if err != nil {
		return e, Fault(err)
	}
	return e, nil
}

// overlay puts one page of rows into the language the caller asked for, and
// says which fields it could not: the value in each field is replaced by this
// locale's row, and every field left in the tenant's own language is named in
// `_i18n` with the reason — missing, withheld or removed.
//
// Two things it never does. It never reads the entity's table again: the source
// text each translation was measured against is lifted out of the rows already
// loaded, which is the same copy the staleness rule needs and the reason the port
// takes the source as a parameter instead of reaching over. And it never serves an
// unreviewed machine draft — that rule is one line inside the port's own
// Translated, keyed on this being a guarded door rather than on who is asking, and
// a door that decided it locally is a door that forgets it.
//
// Public is false here because every route in this file is guarded by the Spec's
// Read: the door that shows a stranger the record is a module's own handler, and
// it sets Public when it calls the port.
//
// It answers the language the response is written in, which is what decides the
// response's Content-Language: a read in the tenant's own language says so, and
// a read that named no language and expressed no preference is written in the
// source and claims nothing.
//
// A read that named no language but *expressed* one — a `lang` cookie the
// workspace left behind, an `Accept-Language` in the caller's own order — is
// negotiated here, in the one place both read doors pass through, so no door can
// forget the question the caller asked without spelling it in a query.
func (s Spec[T]) overlay(ctx context.Context, tx db.Tx[db.Tenant], lang string, rows []T) (string, error) {
	languages := db.TenantOf(tx).Languages
	if lang == "" && len(rows) > 0 {
		lang = negotiateLang(ctx, languages)
	}
	if lang == "" || !slices.Contains(languages.Preferred(), lang) || len(rows) == 0 {
		return "", nil
	}
	if lang == languages.Default {
		return languages.Default, nil // the source copy is the translation of the default language
	}
	src := &specSource[T]{spec: s}
	ids := make([]uuid.UUID, 0, len(rows))
	sources := make(map[uuid.UUID]map[string]string, len(rows))
	lifted := make(map[uuid.UUID]SourceRow, len(rows))
	for _, row := range rows {
		rec := src.lift(row)
		ids = append(ids, rec.ID)
		sources[rec.ID] = rec.Values
		lifted[rec.ID] = rec
	}
	translated, err := s.Translations.Translated(ctx, tx, TranslatedQuery{
		Module: s.Module, Entity: s.Entity, Locale: lang,
		RecordIDs: ids, Sources: sources, RichText: src.RichText(),
	})
	if err != nil {
		return "", err
	}
	byRecord := make(map[uuid.UUID]map[string]TranslatedField, len(translated))
	for _, rec := range translated {
		byRecord[rec.ID] = rec.Fields
	}
	for _, row := range rows {
		id := entity.BaseOf(row).ID
		fields := byRecord[id]
		fallbacks := map[string]entity.Fallback{}
		// A field the port did not answer has no row in this locale, and the
		// port's own documentation says the caller falls back to the source and
		// says so. Saying so is done here, over the entity's own list of
		// translatable fields, because a response that silently kept one field
		// in English is the exact lie `_i18n` exists to prevent.
		for _, translatable := range translatableFields[T]() {
			if _, has := fields[translatable.Name]; !has {
				fallbacks[translatable.Name] = entity.Fallback{Locale: languages.Default, Status: FallbackMissing}
			}
		}
		for name, f := range fields {
			if f.Status == FallbackMissing || f.Status == FallbackWithheld || f.Status == FallbackRemoved {
				// The value in the field is the source, and the caller is told so.
				fallbacks[name] = entity.Fallback{Locale: languages.Default, Status: f.Status}
				continue
			}
			fld, ok := crud.FieldNamed(crud.Fields[T](), name)
			if !ok || !setString(reflect.ValueOf(row), fld.Index, f.Value) {
				continue
			}
			if f.Status != "" {
				fallbacks[name] = entity.Fallback{Locale: lang, Status: f.Status}
			}
		}
		base := entity.BaseOf(row)
		if len(fallbacks) > 0 {
			base.I18N = &fallbacks
		}
	}
	return lang, nil
}

// CookieNameLanguage is the workspace's language cookie: the language the last
// page this person read was served in, and therefore the language the next one
// should be. It is a first-party cookie like the session's — the __Host- spelling
// over https, the plain one where a browser would refuse Secure — and the
// workspace shell sets it when a person switches language.
const CookieNameLanguage = "lang"

// negotiateLang is the language question of a read that did not name one, in the
// order the contract fixes: what the caller said for this one request (`?lang=`,
// handled by the caller of this function, which is why silence arrives here),
// then the cookie the workspace set, then the browser's own list in the order the
// caller ranked it, and then nothing.
//
// Silence is not answered with the tenant's default: a caller who expressed no
// preference is served the source and the response claims no language, which is
// the same answer there was before negotiation existed. Defaulting a preference
// nobody expressed would put a Content-Language on responses whose callers never
// asked a question, and would make every cache in front of the installation hold
// one response where the caller's own header asked for another.
//
// A preference the tenant is not served in is dropped, not honoured: serving
// somebody's first choice under a header the tenant cannot keep would be a
// response that lies about its own language.
func negotiateLang(ctx context.Context, languages *tenancy.Languages) string {
	req, ok := httpx.RequestFrom(ctx)
	if !ok {
		return ""
	}
	speaking := languages.Preferred()
	for _, name := range []string{httpx.CookieName(CookieNameLanguage, true), CookieNameLanguage} {
		if c, err := req.Cookie(name); err == nil {
			if tag, ok := matchLanguage(c.Value, speaking); ok {
				return tag
			}
		}
	}
	for _, tag := range parseAcceptLanguage(req.Header.Get("Accept-Language")) {
		if match, ok := matchLanguage(tag, speaking); ok {
			return match
		}
	}
	return ""
}

// matchLanguage answers the tenant's own spelling of a requested tag, or nothing.
// Comparison ignores case, because "pt-pt" and "PT-PT" are one tag, and a
// response that stored the caller's casing would be storing a tag the tenant
// never declared.
func matchLanguage(tag string, speaking []string) (string, bool) {
	tag = strings.TrimSpace(tag)
	if tag == "" || strings.EqualFold(tag, "*") {
		return "", false
	}
	for _, has := range speaking {
		if strings.EqualFold(has, tag) {
			return has, true
		}
	}
	return "", false
}

// parseAcceptLanguage is the header in the caller's ranked order: q descending,
// ties in the order they were written, and an absent q treated the way the
// specification treats it. A malformed q ends the parse rather than being
// skipped: what is left of a header nobody can rank is not a preference.
func parseAcceptLanguage(header string) []string {
	if header == "" {
		return nil
	}
	var prefs []languagePreference
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(part, ";")
		tag := strings.TrimSpace(fields[0])
		if tag == "" {
			return rankedTags(prefs)
		}
		q := 1.0
		for _, param := range fields[1:] {
			param = strings.TrimSpace(param)
			if !strings.HasPrefix(param, "q=") {
				continue
			}
			value, err := strconv.ParseFloat(strings.TrimPrefix(param, "q="), 64)
			if err != nil || value < 0 || value > 1 {
				return rankedTags(prefs)
			}
			q = value
		}
		prefs = append(prefs, languagePreference{tag: tag, q: q})
	}
	return rankedTags(prefs)
}

// languagePreference is one entry of an Accept-Language header, and rankedTags
// is the whole of turning the list into the caller's order — sorting whatever
// was parsed before a malformed entry, which is why the parse's early returns
// run through it too.
type languagePreference struct {
	tag string
	q   float64
}

func rankedTags(prefs []languagePreference) []string {
	slices.SortStableFunc(prefs, func(a, b languagePreference) int { return cmp.Compare(b.q, a.q) })
	out := make([]string, len(prefs))
	for i, p := range prefs {
		out[i] = p.tag
	}
	return out
}

// setString writes one translated value back into the entity by the field index
// the schema derived — the same index the PATCH merge writes through, and the
// only reflection on the read side. A field it cannot reach (absent, not a
// string, nil pointer with nothing to allocate into) keeps the source text, which
// is the honest answer rather than an empty one.
func setString(v reflect.Value, index []int, value string) bool {
	fv := fieldAt(v, index)
	if !fv.IsValid() || !fv.CanSet() || fv.Kind() != reflect.String {
		return false
	}
	fv.SetString(value)
	return true
}
