// The one translated read that is not a generated route.
//
// A module that *renders* a record — the public site's page, a mail preview, an
// export — has the same need the `?lang=` read doors have, and no Spec to mount
// to get it. The rules must not be forked for it: which draft is withheld, which
// locale counts as an answer and which text falls back to the source are decided
// here, once, in the same port call the generated doors make, from the same
// `Public` flag the port withholds on.
//
// What is decided here besides the call:
//
//   - The source language is the tenant's default, because that is what the
//     record's own write door insists on — a translation of the tenant's own
//     language is refused as a mistake, so the row can only ever be in the
//     default. No caller passes a source language and none can disagree.
//   - One field is answered in the asked language when that field is reviewed
//     against a source that still matches. A field whose only text is an
//     unreviewed draft is not an answer, and neither is one whose source has
//     moved since: the second is text that says something its author no longer
//     says, and it is the one a search engine would index under a language flag
//     and keep for a year. Both are served to the workspace, which sees the
//     label, and neither to a reader, which sees that field in the source and
//     nothing else. The rule is per field and never per locale: a reader who
//     asked for Portuguese of a page whose title is translated and whose
//     paragraph is not is given the Portuguese title, and throwing it away
//     because the paragraph is missing answers a reader with less than the
//     installation has.
//   - A locale counts as one this record *may be read in* — its hreflang
//     alternate — only when every translatable field is reviewed in it. That is
//     the strict question, and it is a different question from what to print.
//   - The other locales are asked about whatever the reader named, including
//     when the reader named nothing: the page at the bare address is read by a
//     crawler that decides what to index from the alternates in its head, and a
//     translation made after the page was written is discoverable only if the
//     source page lists it.
package rest

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
)

// Serving is one record's text as a door outside the generated routes may serve
// it: the language of the document, that text, the language each piece of that
// text is really written in, and every language of this tenant the record may be
// read in whole.
type Serving struct {
	// Language is the tag of the document: the wanted language when the record
	// has any reviewed text of its own in it, the tenant's default when it has
	// none. A document's lang attribute is this and nothing else.
	//
	// It is not the language of every field in Values, and cannot be: a reader
	// who asked for Portuguese of a page with a translated title and an
	// untranslated paragraph is given a Portuguese document, because that is the
	// language they asked for and the page partly answers in. Every field whose
	// text is in another language carries Foreign's answer on the element that
	// prints it, which is the one way both halves get read aloud in the right
	// voice — a mixed page with one language attribute is a page whose English
	// paragraph a screen reader sounds out in Portuguese.
	Language string
	// Values is the text of each translatable field, keyed by field name: the
	// translation where the record has a reviewed one to serve, the source where
	// it does not. Every translatable field has a value, so a renderer never
	// prints "".
	Values map[string]string
	// FieldLanguage is the tag the text of each field in Values is written in,
	// keyed beside it. Every field has an entry, so a renderer never has to
	// guess which half it is printing.
	FieldLanguage map[string]string
	// Complete names every language this record may be read in whole, the source
	// language first. These are a document's hreflang alternates, and the list
	// is honest about what a reader following one would actually be given.
	Complete []string
}

// Foreign is the language one field's text has to declare for itself, or "" when
// that text is in the language the document already declares and needs no mark.
// A renderer that prints a field without asking this has a mixed-language page
// whose second language is invisible to anything that reads the document aloud.
func (s Serving) Foreign(field string) string {
	if tag := s.FieldLanguage[field]; tag != "" && tag != s.Language {
		return tag
	}
	return ""
}

// recordText is one record's own text beside every reviewed text of it, gathered
// once per translated read: what the record says in the tenant's own language, and
// what a public reader may take as each other language's own.
//
// Both translated reads run through it — the text a reader is served and the
// completeness a record screen shows — because which draft is withheld, which row
// still matches the source it was written from, and which languages are this
// tenant's are one answer, and two doors spelling it separately is two doors that
// drift.
type recordText struct {
	// source is the tenant's own language: the language the record is authored in,
	// and therefore the only one it is never behind.
	source string
	// declared is every language this tenant is served in, in its own order.
	declared []string
	// fields names the record's translatable fields in the order its own schema
	// declares them, which is the order a side-by-side view walks.
	fields []string
	// own is the current source text of each of those fields.
	own map[string]string
	// reviewed is each language's reviewed text by field name. A language with no
	// reviewed field of its own is absent rather than empty: nothing of that
	// language is in this record, which is a fact a reader is shown as the source
	// and a translator is shown as nothing translated yet.
	reviewed map[string]map[string]string
}

// readRecordText lifts one record's source text and asks the port for it in every
// language the tenant speaks besides its own, under the caller's transaction and
// with the same `Public` withholding every other public read gets: an unreviewed
// machine draft and a translation whose source has moved since are not this
// locale's own text, and neither is a field with no row.
func readRecordText(ctx context.Context, tx db.Tx[db.Tenant], src TranslationSource,
	port Translations, id uuid.UUID) (recordText, error) {
	if src == nil || port == nil {
		return recordText{}, fmt.Errorf("%w: a translated read needs the entity's own rows and the translation port",
			crud.ErrInvalid)
	}
	languages := db.TenantOf(tx).Languages
	declared := languages.Preferred()
	// The tenant's own language, read through the nil: a tenant that declared
	// nothing has no default to name, which is the same answer Preferred() gives
	// for the same absence. Preferred is nil-safe and the field is not, and a page
	// handler that read the field directly would be a page that panics on the
	// installation that never set a language — which is every installation before
	// the control plane's locale door is first pressed.
	source := ""
	if languages != nil {
		source = languages.Default
	}
	rows, err := src.Rows(ctx, tx, []uuid.UUID{id})
	if err != nil {
		return recordText{}, err
	}
	if len(rows) == 0 {
		return recordText{}, crud.ErrNotFound
	}
	rec := recordText{source: source, declared: declared, fields: src.Fields(), own: rows[0].Values,
		reviewed: make(map[string]map[string]string, len(declared))}
	rich := src.RichText()
	for _, locale := range declared {
		if locale == source {
			continue
		}
		records, err := port.Translated(ctx, tx, TranslatedQuery{
			Module: src.Module(), Entity: src.Entity(), Locale: locale, RecordIDs: []uuid.UUID{id},
			Sources:  map[uuid.UUID]map[string]string{id: rec.own},
			RichText: rich, Public: true,
		})
		if err != nil {
			return recordText{}, err
		}
		if len(records) != 1 {
			continue
		}
		text := make(map[string]string, len(rec.fields))
		for _, name := range rec.fields {
			f, ok := records[0].Fields[name]
			if !ok || f.Status != "" {
				continue
			}
			text[name] = f.Value
		}
		if len(text) == 0 {
			continue
		}
		rec.reviewed[locale] = text
	}
	return rec, nil
}

// ServeTranslated answers one record in the language a reader asked for.
//
// `want` may be empty, or a tag the tenant is not served in; either way nothing is
// overlaid, which is the same silence negotiateLang keeps for a caller who
// expressed no preference, and the answer is the source text in the source
// language. The languages this record could be read in whole are answered either
// way, because they are a fact about the record and not about the request: the
// page at the bare address is the page a crawler reads the alternates from, and a
// translation made after it was written is discoverable only if that page lists
// it. The tenant's declared languages and its default come from the transaction,
// so no answer can name a language this tenant does not serve.
func ServeTranslated(ctx context.Context, tx db.Tx[db.Tenant], src TranslationSource,
	port Translations, id uuid.UUID, want string) (Serving, error) {
	rec, err := readRecordText(ctx, tx, src, port, id)
	if err != nil {
		return Serving{}, err
	}
	serving := Serving{Language: rec.source, Values: maps.Clone(rec.own),
		Complete: []string{rec.source}, FieldLanguage: make(map[string]string, len(rec.fields))}
	for _, name := range rec.fields {
		serving.FieldLanguage[name] = rec.source
	}
	for _, locale := range rec.declared {
		text, ok := rec.reviewed[locale]
		if !ok {
			continue
		}
		// Every field or none: an address that promises this language and answers
		// it halfway is a promise a crawler holds for a year, so only a locale the
		// record is whole in becomes an alternate. What to *print* is the looser
		// question below, and it is a different one.
		if len(text) == len(rec.fields) {
			serving.Complete = append(serving.Complete, locale)
		}
		// A reader who asked for the source language is asking for what the row
		// itself says, and a reader who asked for a language the tenant does not
		// serve is asking for nothing; both are answered by overlaying nothing.
		if locale != want || !slices.Contains(rec.declared, want) || want == rec.source {
			continue
		}
		for name, value := range text {
			serving.Values[name] = value
			serving.FieldLanguage[name] = locale
		}
		// The document is in the language its reader asked for and partly got;
		// every field still in the source says so on its own element, which is
		// what Foreign is for.
		serving.Language = locale
	}
	return serving, nil
}

// LocaleStates answers how much of one record exists in each language this tenant
// speaks besides its own: how many of its translatable fields are reviewed against
// a source that still matches, out of how many the record has.
//
// The tenant's own language is absent, and that is the answer rather than an
// omission: a record is authored in it, so it is complete in it by definition, and
// a badge that reports 100% of a language nothing can be behind in tells a
// translator nothing they could act on. A language with nothing reviewed in it is
// answered with zero and not with silence, because an untranslated language is the
// case the badge exists to point at.
//
// It is the read the generated record screen makes for itself: the same rows, the
// same withholding and the same staleness rule the public read answers with, so a
// screen cannot report a translation a public reader would never be given.
func LocaleStates(ctx context.Context, tx db.Tx[db.Tenant], src TranslationSource,
	port Translations, id uuid.UUID) ([]entity.LocaleState, error) {
	rec, err := readRecordText(ctx, tx, src, port, id)
	if err != nil {
		return nil, err
	}
	var out []entity.LocaleState
	for _, locale := range rec.declared {
		if locale == rec.source {
			continue
		}
		out = append(out, entity.LocaleState{Locale: locale, Reviewed: len(rec.reviewed[locale]),
			Fields: len(rec.fields)})
	}
	return out, nil
}
