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
	p.Plural = cmp.Or(p.Plural, p.Singular+"s")
	return p
}

func (r Resource) primary(fields []entity.Field) entity.Field {
	if f, ok := fieldFor(fields, r.Present.PrimaryField); ok {
		return f
	}
	return known(fields)
}

func (r Resource) rowName(row map[string]any, fields []entity.Field) string {
	if f, ok := fieldFor(fields, r.Present.PrimaryField); ok {
		if value := display.Text(row[f.Name]); value != "" {
			return value
		}
	}
	return label(row, fields)
}

func (r Resource) listFields() []entity.Field {
	listed := onList(r.Schema.Fields)
	primary := r.primary(listed)
	shown := []entity.Field{primary}
	names := r.Present.SummaryFields
	if len(names) > 0 {
		// The author's reading order is stable; hidden/detail-only fields stay
		// off this surface even for a direct render outside route validation.
		names = append(slices.Clone(names), r.Present.StatusField, r.Present.ThumbnailField)
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
