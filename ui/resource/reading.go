package resource

import (
	"cmp"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
)

// reading uses the same translated declarations as the discovery catalogue.
// Older resources retain their inferred names and columns.
func (r Resource) reading(o Options) entity.EntryHints {
	p := o.Words(r.Schema).Entry(r.Present)
	p.Singular = cmp.Or(p.Singular, strings.ToLower(display.Humanize(r.Schema.Entity)))
	// The plural word belongs to the language the reader asked in, so it is asked of
	// that language's catalogue under the resource's own key rather than built here by
	// appending a letter: a resource with no plural word — declared, keyed or
	// catalogued — is answered with its singular, which is the rule this package
	// follows everywhere a count meets a noun.
	p.Plural = cmp.Or(p.Plural, o.plural("resource."+r.Schema.Module+"."+r.Schema.Entity, 2, p.Singular, p.Singular))
	return p
}

// primary is the column a row links on, chosen from fields (the ones this
// surface shows): the field the entity marks `ui:"display"`, else the declared
// primaryField, else the first name candidate. The mark wins on the web because
// it may be blank, which a row's link survives through rowName and a catalogue
// reader titling by primaryField does not.
func (r Resource) primary(fields []entity.Field) entity.Field {
	if f, ok := displayField(fields); ok {
		return f
	}
	if f, ok := fieldFor(fields, r.Present.PrimaryField); ok {
		return f
	}
	return known(fields)
}

// rowName is what a row is called on the web, among fields only: its
// `ui:"display"` value when filled, then its primaryField value, then the
// first candidate with a value (see label), then its id.
func (r Resource) rowName(row map[string]any, fields []entity.Field) string {
	for _, f := range []string{displayName(fields), r.Present.PrimaryField} {
		if f, ok := fieldFor(fields, f); ok {
			if value := display.Text(row[f.Name]); value != "" {
				return value
			}
		}
	}
	return label(row, fields)
}

func displayField(fields []entity.Field) (entity.Field, bool) {
	i := slices.IndexFunc(fields, func(f entity.Field) bool { return f.Display })
	if i < 0 {
		return entity.Field{}, false
	}
	return fields[i], true
}

func displayName(fields []entity.Field) string {
	f, _ := displayField(fields)
	return f.Name
}

func (r Resource) listFields() []entity.Field {
	listed := onList(r.Schema.Fields)
	primary := r.primary(listed)
	shown := []entity.Field{primary}
	names := r.Present.SummaryFields
	if len(names) > 0 {
		// The author's reading order is stable; hidden/detail-only fields stay
		// off this surface even for a direct render outside route validation.
		// The declared primaryField leads it when the display mark took the
		// link column, so the field every row has stays on the list.
		names = append(append([]string{r.Present.PrimaryField}, names...), r.Present.StatusField, r.Present.ThumbnailField)
		for _, name := range names {
			if name == "id" || name == primary.Name {
				continue
			}
			if f, ok := fieldFor(listed, name); ok && !slices.ContainsFunc(shown, func(existing entity.Field) bool { return existing.Name == name }) {
				shown = append(shown, f)
			}
		}
		return shown
	}
	for _, f := range listed {
		if f.Name != primary.Name && f.Name != "id" {
			shown = append(shown, f)
		}
	}
	return shown
}
