package view

import (
	"cmp"
	"slices"

	"github.com/septagon-oss/platformkit/kit/entity/present"
)

// Build assembles one Page. It reads every argument and writes none of them,
// performs no I/O, emits nothing and is safe for concurrent use. A refused build
// returns the zero Page and an error naming what was wrong, and never a half page: a
// half page is a screen with a control missing from it.
func Build(in Input) (Page, error) {
	if err := refuse(in); err != nil {
		return Page{}, err
	}
	b := builder{fieldReader: newReader(in), in: in}
	row := firstRecord(in)
	page := Page{
		PageVersion: PageVersion,
		Kind:        in.State.Kind,
		BuiltAt:     in.State.Now.UTC(),
		Language:    cmp.Or(in.State.Language, in.Policy.Language),
		Zone:        b.zone.String(),
		Ref:         b.ref(in.State.Kind, row),
		Title:       b.title(row),
	}
	page.Breadcrumbs = namesOrNil(b.crumbs(page.Title))
	page.Actions = namesOrNil(b.actions(row))
	switch in.State.Kind {
	case KindList:
		page.Table = b.table()
		if len(page.Table.Rows) == 0 {
			page.Empty = in.Policy.Identity.EmptyDescription
		}
	case KindRecord:
		page.Sections = namesOrNil(b.sections(row))
	case KindForm:
		page.Form = b.form(row)
	}
	return page, nil
}

// builder holds what every member of one Page needs: the input, and the reader that
// resolves a field name into its words, its type and its value.
type builder struct {
	fieldReader
	in Input
}

// firstRecord is the row a record or a form page shows. A list keeps its own order
// and Build never re-sorts a read.
func firstRecord(in Input) map[string]any {
	if in.State.Kind == KindList || len(in.Data.Records) == 0 {
		return nil
	}
	return in.Data.Records[0]
}

// ref is what the page is about: the collection for a list and a create form, the
// record for everything else. A singleton's row has no id of its own to carry.
func (b builder) ref(kind Kind, row map[string]any) Ref {
	ref := Ref{Resource: b.in.Resource.Resource}
	if kind == KindList || b.isCreate(kind) {
		return ref
	}
	ref.ID = idOf(row)
	return ref
}

// isCreate says this page is the create surface: a create form, which points at the
// collection rather than at a record.
func (b builder) isCreate(kind Kind) bool {
	return kind == KindForm && b.in.State.Form.Create
}

// title is the page's own heading: the policy's plural name for a collection, the
// row's semantic title for a record, and the caller's words for a form, which this
// package has none of.
func (b builder) title(row map[string]any) string {
	switch b.in.State.Kind {
	case KindRecord:
		return b.rowTitle(b.in.Policy.Titles.RecordCandidates, row)
	case KindForm:
		return b.in.State.Form.Title
	}
	return b.in.Policy.Identity.Plural
}

// rowTitle is what a row is called: present.Title over the policy's candidates, so
// the id is the last answer and the empty title the last one after that.
func (b builder) rowTitle(candidates []string, row map[string]any) string {
	return present.Title(candidates, row, idOf(row), b.in.Policy.Titles.EmptyTitle)
}

// crumbs is the path back: the caller's root when it named one, the collection, and
// the record when the page is one.
func (b builder) crumbs(title string) []Crumb {
	resource := b.in.Resource.Resource
	crumbs := []Crumb{}
	if b.in.State.Home != "" {
		crumbs = append(crumbs, Crumb{Label: b.in.State.Home})
	}
	if resource != "" {
		crumbs = append(crumbs, Crumb{Label: b.in.Policy.Identity.Plural, Ref: Ref{Resource: resource}})
	}
	if title != "" && b.in.State.Kind != KindList && !b.isCreate(b.in.State.Kind) {
		crumbs = append(crumbs, Crumb{Label: title, Ref: b.ref(b.in.State.Kind, firstRecord(b.in))})
	}
	return crumbs
}

// table is the list: its columns in policy order, one cell per column per row in the
// read's own order, and the window, counts, orders and narrowings the read and the
// policy carried.
func (b builder) table() Table {
	columns := b.columns()
	return Table{
		Columns: columns, Rows: b.rows(columns), Window: b.in.Data.Window,
		Total: b.in.Data.Total, Counted: b.in.Data.Counted, Sort: b.in.Data.Sort,
		DefaultSort: b.in.Policy.List.DefaultSort,
		Sorts:       namesOrNil(b.in.Policy.List.Sorts),
		Filters:     namesOrNil(b.in.Policy.List.Filters),
	}
}

// columns are the policy's layout, each labelled by the policy and marked with the
// order a second click on its header asks for.
func (b builder) columns() []Column {
	current := cmp.Or(b.in.Data.Sort, b.in.Policy.List.DefaultSort)
	out := make([]Column, 0, len(b.in.Policy.Layout.Columns))
	for _, name := range b.in.Policy.Layout.Columns {
		rf, sf, ok := b.lookup(name)
		if !ok {
			continue
		}
		sort, sortable, ordered := columnOrder(b.in.Policy.List.Sorts, sf.Name, current)
		out = append(out, Column{Field: sf.Name, Label: rf.Label,
			Sort: sort, Sortable: sortable, Current: ordered})
	}
	return namesOrNil(out)
}

// columnOrder is what one header offers: the order a second click asks for — the
// ascending spelling while the list is not ordered ascending by this column, the
// descending one when it is — whether it offers an order at all, and whether this
// column is the order the list is in. The spellings are the policy's, so the value a
// shell sends back is the one the read already accepts.
func columnOrder(sorts []present.ResolvedSortOption, field, current string) (next string, sortable, ordered bool) {
	ascending, descending := "", ""
	for _, s := range sorts {
		if s.Field != field {
			continue
		}
		if s.Desc {
			descending = s.Query
		} else {
			ascending = s.Query
		}
	}
	if next = ascending; current == ascending && ascending != "" {
		next = descending
	}
	return next, next != "", current != "" && (current == ascending || current == descending)
}

// rows are the read's own records, in the read's own order, each with one cell per
// column so a shell never looks a value up by name.
func (b builder) rows(columns []Column) []Row {
	out := make([]Row, 0, len(b.in.Data.Records))
	for _, record := range b.in.Data.Records {
		row := Row{ID: idOf(record), Title: b.rowTitle(b.in.Policy.Titles.ListCandidates, record)}
		for _, column := range columns {
			if cell, ok := b.cell(column.Field, record); ok {
				row.Cells = append(row.Cells, cell)
			}
		}
		row.Cells = namesOrNil(row.Cells)
		out = append(out, row)
	}
	return namesOrNil(out)
}

// sections are the three blocks of a record, in reading order: the status-and-summary
// block, each declared block in policy order, and the fields that belong to no block.
// Membership is the layout's, never re-derived, and a block with no fields is not
// written.
func (b builder) sections(row map[string]any) []Section {
	out := []Section{}
	if fields := b.fields(row, b.in.Policy.Layout.Overview); len(fields) > 0 {
		out = append(out, Section{Key: "overview", Fields: fields})
	}
	for _, s := range b.in.Policy.Layout.Sections {
		if fields := b.fields(row, s.Fields); len(fields) > 0 {
			out = append(out, Section{Key: s.Key, Label: s.Label, Fields: fields})
		}
	}
	if fields := b.fields(row, b.in.Policy.Layout.Information); len(fields) > 0 {
		out = append(out, Section{Key: "information", Fields: fields})
	}
	return out
}

// fields are the named fields of one block, in the layout's order, skipping a name
// the schema does not carry.
func (b builder) fields(row map[string]any, names []string) []Field {
	out := make([]Field, 0, len(names))
	for _, name := range names {
		if f, ok := b.field(name, row); ok {
			out = append(out, f)
		}
	}
	return namesOrNil(out)
}

// form is one create or update surface: the layout's own controls in order, the
// refused write's verdict, and the row's write counter the page was drawn from, so
// the write that carries it back is refused when the row has moved.
func (b builder) form(row map[string]any) Form {
	names := b.in.Policy.Layout.Update
	if b.in.State.Form.Create {
		names = b.in.Policy.Layout.Create
	}
	return Form{
		Create: b.in.State.Form.Create, Title: b.in.State.Form.Title, Detail: b.in.Data.Detail,
		ExpectedRevision: revisionOf(row), Controls: namesOrNil(b.controls(names, row)),
		Violations: namesOrNil(b.violations()),
	}
}

// controls are the fields a form takes, in the layout's order, starting from the
// row's value where there is one and the schema's declared default where there is
// not, so a form starts where the record left off rather than where a shell guessed.
// A field the caller may not write on this surface is shown read-only, as is one a
// command owns.
func (b builder) controls(names []string, row map[string]any) []Control {
	out := make([]Control, 0, len(names))
	for _, name := range names {
		rf, sf, ok := b.lookup(name)
		if !ok {
			continue
		}
		editable := rf.Editable.Update
		if b.in.State.Form.Create {
			editable = rf.Editable.Create
		}
		start := startOf(row, sf.Name, sf.Default)
		kind, elem, value := b.value(rf, sf, start)
		out = append(out, Control{
			Name: sf.Name, Label: rf.Label, Help: rf.Help, Control: rf.Control,
			ReadOnly:  !editable || slices.Contains(b.in.Resource.Immutable, sf.Name),
			Required:  sf.Required,
			MaxLength: sf.MaxLength, Default: sf.Default,
			ValueKind: kind, Elem: elem, Value: value,
			Options: namesOrNil(rf.Options), Keys: namesOrNil(sf.Keys),
		})
	}
	return out
}

// violations are the refused write's, one per field, in lexicographic order: the only
// order a map may reach a Page through.
func (b builder) violations() []Violation {
	out := make([]Violation, 0, len(b.in.Data.Violations))
	for _, name := range sortedKeys(b.in.Data.Violations) {
		out = append(out, Violation{Field: name, Message: b.in.Data.Violations[name]})
	}
	return out
}

// startOf is the value a control starts from: the row's where the row holds one, the
// schema's declared default where it does not, and nothing where the default is empty
// too, so an absent value stays absent rather than becoming an empty string.
func startOf(row map[string]any, name, def string) map[string]any {
	if v, ok := row[name]; ok && v != nil {
		return row
	}
	if def == "" {
		return nil
	}
	return map[string]any{name: def}
}
