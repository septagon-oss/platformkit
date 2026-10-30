package present

import (
	"cmp"
	"slices"

	"github.com/septagon-oss/platformkit/kit/entity"
)

func visible(f entity.Field) bool { return !f.Presentation.Hidden() }

// names collects distinct field names that exist and pass keep, in the order
// added. An unknown name names nothing and is skipped.
type names struct {
	fields []entity.Field
	keep   func(entity.Field) bool
	out    []string
}

func newNames(fields []entity.Field, keep func(entity.Field) bool) *names {
	return &names{fields: fields, keep: keep, out: []string{}}
}

func (n *names) add(name string) {
	f, ok := fieldNamed(n.fields, name)
	if ok && name != "" && n.keep(f) && !slices.Contains(n.out, name) {
		n.out = append(n.out, name)
	}
}

func titles(in Input) ResolvedTitles {
	h := in.Hints
	fs := in.Schema.Fields
	t := ResolvedTitles{
		ListCandidates:   candidates(fs, h.PrimaryField, entity.Field.OnList),
		RecordCandidates: candidates(fs, h.PrimaryField, visible),
		EmptyTitle:       in.word("present.emptyTitle", "—"),
		Preview:          existing(fs, h.PreviewField),
		Status:           existing(fs, h.StatusField),
		Cover:            existing(fs, h.CoverField),
		Thumbnail:        existing(fs, cmp.Or(h.ThumbnailField, h.CoverField)),
	}
	t.Summary = summary(fs, h.SummaryFields, first(t.ListCandidates))
	return t
}

func existing(fields []entity.Field, name string) string {
	if _, ok := fieldNamed(fields, name); ok {
		return name
	}
	return ""
}

func first(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}

// candidates are the fields that may name a row, best first: the field the
// entity marks as its display, the declared primary field, then every other
// writable text field in schema order.
func candidates(fields []entity.Field, primary string, keep func(entity.Field) bool) []string {
	n := newNames(fields, func(f entity.Field) bool { return f.Type != entity.TypeSketch && keep(f) })
	for _, f := range fields {
		if f.Display {
			n.add(f.Name)
			break
		}
	}
	n.add(primary)
	for _, f := range fields {
		if (f.Type == entity.TypeString || f.Type == entity.TypeText) && len(f.Enum) == 0 && !entity.ServerOwned(f) {
			n.add(f.Name)
		}
	}
	return n.out
}

// summary is the declared summary fields that read beside the title: not the
// id, not the title itself and only those the list shows.
func summary(fields []entity.Field, declared []string, title string) []string {
	n := newNames(fields, entity.Field.OnList)
	for _, name := range declared {
		if name != "id" && name != title {
			n.add(name)
		}
	}
	return n.out
}

func emptyLayout() ResolvedLayout {
	return ResolvedLayout{Columns: []string{}, Overview: []string{}, Information: []string{}, Sections: []ResolvedSection{}, Create: []string{}, Update: []string{}}
}

func layout(in Input, t ResolvedTitles) ResolvedLayout {
	fs := in.Schema.Fields
	l := emptyLayout()
	if !in.Singleton && in.offers("list") {
		l.Columns = columns(in, t)
	}
	l.Information = []string{}
	overview := newNames(fs, visible)
	overview.add(t.Status)
	for _, name := range t.Summary {
		overview.add(name)
	}
	title := first(t.RecordCandidates)
	l.Overview = slices.DeleteFunc(overview.out, func(name string) bool { return name == title })
	sectioned := map[string]bool{}
	for _, s := range in.Hints.Sections {
		rs := ResolvedSection{Key: s.Key, Label: s.Label, Fields: []string{}}
		for _, f := range fs {
			if f.Presentation.Section == s.Key && visible(f) {
				rs.Fields = append(rs.Fields, f.Name)
				sectioned[f.Name] = true
			}
		}
		l.Sections = append(l.Sections, rs)
	}
	for _, f := range fs {
		if visible(f) && f.Name != "id" && f.Name != title && f.Name != t.Cover && !sectioned[f.Name] && !slices.Contains(l.Overview, f.Name) {
			l.Information = append(l.Information, f.Name)
		}
	}
	l.Create = form(in, "create", func(f entity.Field) bool { return editableCreate(in, f) })
	l.Update = form(in, "update", func(f entity.Field) bool { return editableUpdate(in, f) })
	l.Forms = forms(in, l)
	return l
}

// editableCreate is false for a field a command owns: the create route refuses
// it, so a form offering it could never be submitted.
func editableCreate(in Input, f entity.Field) bool {
	return !entity.ServerOwned(f) && !slices.Contains(in.Immutable, f.Name)
}

func editableUpdate(in Input, f entity.Field) bool {
	return !entity.ServerOwned(f) && !slices.Contains(in.Immutable, f.Name)
}

// form lists the fields a create or update form takes: the editable ones the
// resource offers the operation for, minus the hidden ones unless a form could
// not be filled in without them.
func form(in Input, op string, editable func(entity.Field) bool) []string {
	out := []string{}
	if !in.offers(op) {
		return out
	}
	for _, f := range in.Schema.Fields {
		if editable(f) && (visible(f) || (f.Required && f.Default == "")) {
			out = append(out, f.Name)
		}
	}
	return out
}

// columns is the title field, then the list's other fields: the declared
// summary order when one exists, else every list field in schema order.
func columns(in Input, t ResolvedTitles) []string {
	n := newNames(in.Schema.Fields, entity.Field.OnList)
	skipID := func(name string) {
		if name != "id" {
			n.add(name)
		}
	}
	skipID(first(t.ListCandidates))
	if len(in.Hints.SummaryFields) == 0 {
		for _, f := range in.Schema.Fields {
			skipID(f.Name)
		}
		return n.out
	}
	skipID(in.Hints.PrimaryField)
	for _, name := range t.Summary {
		skipID(name)
	}
	skipID(t.Status)
	skipID(t.Thumbnail)
	return n.out
}
