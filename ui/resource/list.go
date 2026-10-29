package resource

import (
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/document"
)

// List renders the resolved collection layout, choices, count and pagination.
// Rows and total must describe the same scoped read; writable permits only
// mounted write operations. Invalid supported policy returns a render error.
func List(r Resource, o Options, rows []map[string]any, total int64, pageNo int, sort string, writable bool) document.View {
	r = r.policy(o)
	if err := r.validatePolicy(); err != nil {
		return document.View{Body: []g.Node{invalid(err)}}
	}
	at := r.Screen
	reading := r.Resolved.Identity
	title := r.collectionName()
	if sort == "" {
		sort = r.Resolved.List.DefaultSort
	}
	noun := reading.Plural
	if total == 1 {
		noun = reading.Singular
	}
	var actions []g.Node
	// The New button is drawn for a caller who may write *and* a resource that
	// offers a create: the two questions are different, and the second is
	// answered by the mounted routes rather than by anybody's permissions.
	if writable && r.offers("create") {
		actions = []g.Node{components.Button(components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: r.anchor("list", "action.create")}, Label: o.Text("screens.new", "New %s", reading.Singular), Href: at + "/new", Variant: r.primaryAction(true)})}
	}
	pager := components.PaginationProps{
		HTMXProps:   components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
		CurrentPage: pageNo, TotalPages: pages(total), BaseURL: listURL(at, sort, r.Selected),
		NavigationLabel:  o.Text("screens.pagination", "Pagination"),
		PreviousLabel:    o.Text("screens.previous", "Previous page"),
		NextLabel:        o.Text("screens.next", "Next page"),
		PageLabel:        o.Text("screens.page", "Go to page %s", "%d"),
		CurrentPageLabel: o.Text("screens.current_page", "Page %s, current page", "%d"),
	}
	body := []g.Node{table(o, r, at, title, rows, sort, o.Text("screens.count", "%d %s", total, noun), &pager, components.DataListSlots{
		Toolbar: []g.Node{components.Toolbar(components.ToolbarProps{Title: title, Subtitle: reading.Description}, actions...)},
		// Collection commands retain their native forms and precede the pager.
		// Generated CRUD does not gain selected-row writes from a UI control.
		Footer: CommandForms(r, o, at, true, nil),
	})}
	return document.View{Title: title, Language: r.Resolved.Language, Body: r.viewBody("list", body)}
}
func listURL(at, sort string, filters map[string]string) string {
	query := url.Values{}
	if sort != "" {
		query.Set("sort", sort)
	}
	for _, name := range slices.Sorted(maps.Keys(filters)) {
		query.Add("filter", name+":"+filters[name])
	}
	if len(query) == 0 {
		return at
	}
	return at + "?" + query.Encode()
}

func choices(r Resource, o Options, sort string) ([]components.ChoiceLink, []components.ChoiceLink) {
	sorts, filters := []components.ChoiceLink{}, []components.ChoiceLink{}
	for _, choice := range r.Resolved.List.Sorts {
		// A sort chip is a column's affordance, so the page offers the order of
		// a column it actually shows. The resolver answers what may be ordered;
		// `hide:list` is the author saying this column is not on this screen.
		// A row's own id is the one column ordered without being shown.
		if choice.Field != "id" && !slices.Contains(r.Resolved.Layout.Columns, choice.Field) {
			continue
		}
		sorts = append(sorts, components.ChoiceLink{Key: choice.Query, Label: choice.Label, Href: listURL(r.Screen, choice.Query, r.Selected), Selected: sort == choice.Query})
	}
	for _, filter := range r.Resolved.List.Filters {
		for _, choice := range filter.Values {
			selection := maps.Clone(r.Selected)
			if selection == nil {
				selection = map[string]string{}
			}
			selected := selection[filter.Field] == choice.Value
			selection[filter.Field] = choice.Value
			remove := maps.Clone(selection)
			delete(remove, filter.Field)
			link := components.ChoiceLink{Key: filter.Field + ":" + choice.Value, Label: filter.Label + ": " + choice.Label, Href: listURL(r.Screen, sort, selection), Selected: selected}
			if selected {
				link.RemoveHref, link.RemoveLabel = listURL(r.Screen, sort, remove), o.Text("screens.remove_filter", "Remove %s filter", filter.Label)
			}
			filters = append(filters, link)
		}
	}
	return sorts, filters
}

func fieldSorts(policy *present.Resolved, name string) []present.ResolvedSortOption {
	var out []present.ResolvedSortOption
	for _, choice := range policy.List.Sorts {
		if choice.Field == name {
			out = append(out, choice)
		}
	}
	return out
}

func sortSlots(r Resource, o Options, sort string) components.TableSlots {
	return components.TableSlots{
		SortURL: func(c components.TableColumn) string {
			choices := fieldSorts(r.Resolved, c.Key)
			if len(choices) == 0 {
				return ""
			}
			chosen := choices[0]
			for i, choice := range choices {
				if choice.Query == sort {
					chosen = choices[(i+1)%len(choices)]
				}
			}
			return listURL(r.Screen, chosen.Query, r.Selected)
		},
		SortState: func(c components.TableColumn) string {
			for _, choice := range fieldSorts(r.Resolved, c.Key) {
				if choice.Query == sort {
					if choice.Desc {
						return "descending"
					}
					return "ascending"
				}
			}
			return "none"
		},
	}
}

func table(o Options, r Resource, at, title string, rows []map[string]any, sort, count string, pager *components.PaginationProps, slots components.DataListSlots) g.Node {
	columns := make([]components.TableColumn, 0, len(r.Resolved.Layout.Columns))
	for i, name := range r.Resolved.Layout.Columns {
		f := r.field(name)
		columns = append(columns, components.TableColumn{Key: name, Label: f.Label, Primary: i == 0, Sortable: len(fieldSorts(r.Resolved, name)) > 0,
			Stacked: len(r.Resolved.Titles.Summary) > 0 && i > 0 && name != r.Resolved.Titles.Status})
	}
	out := make([]components.DataRow, 0, len(rows))
	source := map[string]map[string]any{}
	for _, row := range rows {
		id := display.Text(row["id"])
		source[id] = row
		cells := map[string]any{}
		for _, name := range r.Resolved.Layout.Columns {
			cells[name] = value(r.field(name), row, o).text
		}
		out = append(out, components.DataRow{TableRow: components.TableRow{ID: id, Cells: cells}})
	}
	sortChoices, filters := choices(r, o, sort)
	p := components.DataListProps{HTMXProps: components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"}, Columns: columns,
		Groups: []components.DataGroup{{Key: "rows", Rows: out}}, Label: title, ResultCountText: count, Pagination: pager, SortChoices: sortChoices, Filters: filters}
	if len(rows) == 0 {
		description := r.Resolved.Identity.EmptyDescription
		if description == "" {
			description = o.emptyCollection(r.Resolved.Identity.Plural)
		}
		p.State = components.ContentState{Status: components.MediaEmpty, Title: title, Text: description}
		p.Groups = nil
	}
	slots.TableSlots = sortSlots(r, o, sort)
	slots.Cell = func(row components.TableRow, c components.TableColumn) g.Node {
		return listCell(r, o, source[row.ID], c)
	}
	if len(columns) == 0 && len(rows) > 0 {
		return columnless(p, slots)
	}
	return components.DataListWithSlots(p, slots)
}

func listCell(r Resource, o Options, row map[string]any, c components.TableColumn) g.Node {
	f := r.field(c.Key)
	read := value(f, row, o)
	if read.err != nil {
		return invalid(read.err)
	}
	title := present.Title(r.Resolved.Titles.ListCandidates, row, display.Text(row["id"]), r.Resolved.Titles.EmptyTitle)
	definition, _ := fieldFor(r.Schema.Fields, c.Key)
	definition.Present = f.Kind
	// A choice reads its selection whatever the value, an empty one included; a
	// media kind is drawn only where the author left something to draw.
	if raw := row[c.Key]; definition.Choices != nil || (raw != nil && display.Text(raw) != "") {
		if node := compose(definition, components.TableRow{ID: display.Text(row["id"]), Cells: row}, c.Primary, r.recordAddress(), o, title, f.Reading.Missing); node != nil {
			return node
		}
	}
	if c.Primary {
		// The row's name is the way into the record wherever a record page exists.
		// Where none was mounted, the same words stay plain text.
		if at := r.recordAddress(); at != "" {
			return components.RowLink(components.LinkProps{Label: title, Href: at + "/" + display.Text(row["id"])})
		}
		return g.Text(title)
	}
	if read.node != nil {
		return read.node
	}
	return g.Text(read.text)
}

func columnless(p components.DataListProps, slots components.DataListSlots) g.Node {
	nodes := append([]g.Node{}, slots.Toolbar...)
	for _, list := range [][]components.ChoiceLink{p.Filters, p.SortChoices} {
		for _, choice := range list {
			nodes = append(nodes, components.Link(components.LinkProps{Label: choice.Label, Href: choice.Href}))
		}
	}
	nodes = append(nodes, components.FormSentence(components.FormSentenceProps{Text: p.ResultCountText}))
	nodes = append(nodes, slots.Footer...)
	if p.Pagination != nil {
		nodes = append(nodes, components.Pagination(*p.Pagination))
	}
	return components.Stack(components.StackProps{Gap: "4"}, nodes...)
}

func (o Options) emptyCollection(plural string) string {
	if o.Locale != nil {
		for _, key := range []string{"screens.empty_collection", "screens.empty"} {
			translated := o.Locale.Text(key, "\x00%s", plural)
			if !strings.HasPrefix(translated, "\x00") {
				return translated
			}
		}
	}
	return fmt.Sprintf("No %s yet.", plural)
}
func pages(total int64) int { return int((total + PerPage - 1) / PerPage) }
