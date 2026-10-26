package resource

import (
	"fmt"
	"strings"
)

func (r Resource) validatePolicy() error {
	fields := map[string]bool{}
	for _, f := range r.Resolved.Fields {
		if f.Name == "" || fields[f.Name] {
			return fmt.Errorf("resource: duplicate or empty policy field %q", f.Name)
		}
		if _, exists := fieldFor(r.Schema.Fields, f.Name); !exists {
			return fmt.Errorf("resource: policy field %q has no schema definition", f.Name)
		}
		if strings.TrimSpace(f.Label) == "" {
			return fmt.Errorf("resource: policy field %q has no label", f.Name)
		}
		fields[f.Name] = true
	}
	p := r.Resolved
	groups := [][]string{p.Layout.Columns, p.Layout.Overview, p.Layout.Information, p.Layout.Create, p.Layout.Update, p.Titles.ListCandidates, p.Titles.RecordCandidates, p.Titles.Summary}
	for _, section := range p.Layout.Sections {
		groups = append(groups, section.Fields)
	}
	for _, choice := range p.List.Sorts {
		groups = append(groups, []string{choice.Field})
	}
	for _, filter := range p.List.Filters {
		groups = append(groups, []string{filter.Field})
	}
	for _, name := range []string{p.Titles.Preview, p.Titles.Status, p.Titles.Cover, p.Titles.Thumbnail} {
		if name != "" {
			groups = append(groups, []string{name})
		}
	}
	for _, group := range groups {
		for _, name := range group {
			if !fields[name] {
				return fmt.Errorf("resource: policy references absent field %q", name)
			}
		}
	}
	for _, f := range p.Fields {
		if f.Reading.Money != nil && !fields[f.Reading.Money.CurrencyField] {
			return fmt.Errorf("resource: money references absent currency field")
		}
		if f.Reading.Time != nil && f.Reading.Time.Kind != "date" && f.Reading.Time.Kind != "instant" {
			return fmt.Errorf("resource: unsupported time kind %q", f.Reading.Time.Kind)
		}
	}
	return nil
}
