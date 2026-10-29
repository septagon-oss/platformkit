package present

import (
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
)

func list(in Input) ResolvedList {
	l := ResolvedList{Sorts: []ResolvedSortOption{}, Filters: []ResolvedFilter{}}
	if in.Singleton || !in.offers("list") {
		return l
	}
	if _, ok := fieldNamed(in.Schema.Fields, "createdAt"); ok {
		l.DefaultSort = "-createdAt"
	}
	for _, name := range sortable(in) {
		f, _ := fieldNamed(in.Schema.Fields, name)
		label := display.FieldLabel(f)
		l.Sorts = append(l.Sorts,
			ResolvedSortOption{Query: name, Label: label, Field: name},
			ResolvedSortOption{Query: "-" + name, Label: label, Field: name, Desc: true})
	}
	for _, f := range in.Schema.Fields {
		if f.OnList() {
			l.Filters = appendFilter(in, l.Filters, f)
		}
	}
	return l
}

// sortable is the declared sortable fields that exist, or every scalar field
// when the author declared none.
func sortable(in Input) []string {
	n := newNames(in.Schema.Fields, func(entity.Field) bool { return true })
	if in.Hints.Sortable == nil {
		n.keep = func(f entity.Field) bool {
			return f.Type != entity.TypeList && f.Type != entity.TypeMap && f.Type != entity.TypePicture
		}
		for _, f := range in.Schema.Fields {
			n.add(f.Name)
		}
		return n.out
	}
	for _, name := range in.Hints.Sortable {
		n.add(name)
	}
	return n.out
}

func appendFilter(in Input, out []ResolvedFilter, f entity.Field) []ResolvedFilter {
	switch {
	case len(f.Enum) > 0:
		return append(out, ResolvedFilter{Field: f.Name, Label: display.FieldLabel(f), Kind: "enum", Values: options(f, false)})
	case f.Type == entity.TypeBool:
		return append(out, ResolvedFilter{Field: f.Name, Label: display.FieldLabel(f), Kind: "bool", Values: []ResolvedOption{
			{Value: "true", Label: in.word("present.true", "Yes"), Tone: "neutral"},
			{Value: "false", Label: in.word("present.false", "No"), Tone: "neutral"}}})
	}
	return out
}
