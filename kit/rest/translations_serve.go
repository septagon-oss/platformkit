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
//   - A locale is an answer when every translatable field of the record is
//     reviewed against a source that still matches. A field whose only text is
//     an unreviewed draft is not an answer, and neither is one whose source has
//     moved since: the second is a page that says something its author no longer
//     says, and it is the one a search engine would index under a language flag
//     and keep for a year. Both are served to the workspace, which sees the
//     label, and neither to a reader, which sees none.
//   - A caller that asks for the source language costs no query: the row *is*
//     the translation of the default language.
package rest

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// Serving is one record's text as a door outside the generated routes may serve
// it: the language the text is in, that text, and every language of this tenant
// the record may be read in.
type Serving struct {
	// Language is the tag of the text in Values: the wanted language when the
	// record is reviewed in it, the tenant's default when it is not. A
	// document's lang attribute is this and nothing else — a page that declared
	// the language its reader asked for while printing the one it was written
	// in would tell a screen reader to read English with a Portuguese voice.
	Language string
	// Values is the text of each translatable field, keyed by field name: the
	// translation where the record has one to serve, the source where it does
	// not. Every translatable field has a value, so a renderer never prints "".
	Values map[string]string
	// Complete names every language this record may be read in, the source
	// language first. These are a document's hreflang alternates, and the list
	// is honest about what a reader would actually be given.
	Complete []string
}

// ServeTranslated answers one record in the language a reader asked for.
//
// `want` may be empty, or a tag the tenant is not served in; either way the
// answer is the source text and the source language, which is the same silence
// negotiateLang keeps for a caller who expressed no preference. The tenant's
// declared languages and its default come from the transaction, so no answer can
// name a language this tenant does not serve.
func ServeTranslated(ctx context.Context, tx db.Tx[db.Tenant], src TranslationSource,
	port Translations, id uuid.UUID, want string) (Serving, error) {
	if src == nil || port == nil {
		return Serving{}, fmt.Errorf("%w: a translated read needs the entity's own rows and the translation port",
			crud.ErrInvalid)
	}
	languages := db.TenantOf(tx).Languages
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
		return Serving{}, err
	}
	if len(rows) == 0 {
		return Serving{}, crud.ErrNotFound
	}
	own := rows[0].Values
	serving := Serving{Language: source, Values: maps.Clone(own), Complete: []string{source}}
	if want == "" || want == source || !slices.Contains(languages.Preferred(), want) {
		return serving, nil
	}
	rich := src.RichText()
	for _, locale := range languages.Preferred() {
		if locale == source {
			continue
		}
		records, err := port.Translated(ctx, tx, TranslatedQuery{
			Module: src.Module(), Entity: src.Entity(), Locale: locale, RecordIDs: []uuid.UUID{id},
			Sources:  map[uuid.UUID]map[string]string{id: own},
			RichText: rich, Public: true,
		})
		if err != nil {
			return Serving{}, err
		}
		var fields map[string]TranslatedField
		if len(records) == 1 {
			fields = records[0].Fields
		}
		// One pass, and the whole locale is judged by it: a page printed half in
		// Portuguese and half in English would carry the language attribute of
		// the first and be legible as neither.
		text := make(map[string]string, len(fields))
		complete := len(fields) > 0
		for _, name := range src.Fields() {
			f, ok := fields[name]
			if !ok || f.Status != "" {
				complete = false
				break
			}
			text[name] = f.Value
		}
		if !complete {
			continue
		}
		serving.Complete = append(serving.Complete, locale)
		if locale == want {
			maps.Copy(serving.Values, text)
			serving.Language = locale
		}
	}
	return serving, nil
}
