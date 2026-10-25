package resource

import (
	"strings"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/document"
)

// List is the list screen: a toolbar with the count, the rows, the pager. The
// New button is drawn only for a caller who may write — a person who may not is
// not shown a form that would refuse them.
//
// A singleton has no list screen, and this renderer is not called for one:
// ui/screens mounts the screens the mounted routes answer, and a singleton's
// routes are one read and one write on its own path. The renderers do not
// duplicate that refusal, because a screen defending against a caller who would
// have to be the adapter is a second answer to one question, and only one of the
// two is checked by anyone.
func List(r Resource, o Options, rows []map[string]any, total int64, pageNo int, sort string, writable bool) document.View {
	at := r.Screen
	reading := r.reading(o)
	title := display.Humanize(reading.Plural)
	noun := reading.Plural
	if total == 1 {
		noun = reading.Singular
	}
	var actions []g.Node
	// The New button is drawn for a caller who may write *and* a resource that
	// offers a create: the two questions are different, and the second is
	// answered by the mounted routes rather than by anybody's permissions.
	if hasControl(r.resolvedView("list", writable), "action.create") {
		actions = []g.Node{components.Button(components.ButtonProps{ComponentProps: components.ComponentProps{Attrs: r.anchor("list", "action.create")}, Label: o.Text("screens.new", "New %s", reading.Singular), Href: at + "/new"})}
	}
	pager := components.PaginationProps{
		HTMXProps:   components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
		CurrentPage: pageNo, TotalPages: pages(total), BaseURL: at + "?sort=" + sort,
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
		Footer: commandForms(o, o.Words(r.Schema), r.Commands, at, true),
	})}
	return document.View{Title: title, Body: r.viewBody("list", body)}
}

// The id is not a column. It is the row's identity and it is already the link's
// href; a table that leads with a UUID is a table nobody can read.
func table(o Options, r Resource, at, title string, rows []map[string]any, sort, count string, pager *components.PaginationProps, slots components.DataListSlots) g.Node {
	// The identity column is chosen from the fields this list shows, not from the
	// schema: `known` answers without any declaration, so the gate that refuses an
	// author pointing `primaryField` at hidden plumbing says nothing about this path,
	// and a table that led with a value its author kept off every row would put that
	// value back through the one column every row links on.
	listed := onList(r.Schema.Fields)
	primary := r.primary(listed)
	shown := r.listFields()
	words := o.Words(r.Schema)
	columns := listColumns(r, words, shown)
	data := listRows(r, words, rows, shown, listed, primary)
	state, groups := listContentState(o, r, title, data.rows)
	p := components.DataListProps{
		HTMXProps: components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
		Columns:   columns, Groups: groups, Label: title, State: state, ResultCountText: count, Pagination: pager,
	}
	slots.TableSlots = listTableSlots(o, r, at, sort, primary, data)
	return components.DataListWithSlots(p, slots)
}

func listColumns(r Resource, words Words, shown []entity.Field) []components.TableColumn {
	columns := make([]components.TableColumn, 0, len(shown))
	// A row whose author named its summary is, on a phone, its name and its status with that
	// summary stacked under the name rather than widening the table past the screen. A list
	// with no summary shows every field it lists, which stacked would be the whole record on
	// every row, so its table keeps scrolling inside its own region.
	summarised := len(r.Present.SummaryFields) > 0
	for i, f := range shown {
		columns = append(columns, components.TableColumn{
			Key: f.Name, Label: words.FieldLabel(f), Sortable: r.sortable(f), Primary: i == 0,
			Stacked: summarised && i > 0 && f.Name != r.Present.StatusField,
		})
	}
	return columns
}

func listRows(r Resource, words Words, rows []map[string]any, shown, listed []entity.Field, primary entity.Field) listRowData {
	out := make([]components.DataRow, 0, len(rows))
	// named is what a row whose leading column is blank is called by, kept beside the cells
	// rather than written into them: a cell is a field's own value, and the presentation a
	// field names composes from it (see compose) — borrowing another field's value would put
	// an address in a person's disc. What reads this is the row's link, which is the one thing
	// on the screen that is not a field: it is the row, and so it takes the row's name.
	named := make(map[string]string, len(rows))
	// instants holds what each time cell actually *is*, beside the text it is shown as: the cell's
	// own words come from display.Display and say "2026-07-01 14:12" in no zone at all, which a
	// browser cannot translate into the reader's own time. The instant is kept here, keyed the way
	// `named` is keyed, because a components.TableRow carries cells as text and nothing else.
	instants := make(map[string]map[string]components.TimeText, len(rows))
	tones := make(map[string]string, len(rows))
	for _, row := range rows {
		if f, ok := fieldFor(shown, r.Present.StatusField); ok {
			tones[display.Text(row["id"])] = f.Presentation.EnumTones[display.Text(row[f.Name])]
		}
		cells := map[string]any{}
		for _, f := range shown {
			text := words.Value(f, row[f.Name])
			cells[f.Name] = text
			if f.Type != entity.TypeTime {
				continue
			}
			if when, ok := display.Instant(row[f.Name]); ok {
				id := display.Text(row["id"])
				if instants[id] == nil {
					instants[id] = make(map[string]components.TimeText, 1)
				}
				instants[id][f.Name] = components.TimeText{AtUTC: when, Text: text}
			}
		}
		// The leading cell is the row's only link, so its text is that link's accessible name. A
		// blank there means this row has nothing in the column the entity leads with, and an empty
		// link is a row a screen reader reads as "link" — so the row names itself, by the next
		// candidate it has a value for rather than by its id. See label. The blank is read off the
		// row, not off the cell above: display.Display spells "nothing" as a dash, and a dash is
		// not empty, so a cell tested here could never fall back.
		if display.Text(row[primary.Name]) == "" {
			if name := label(row, listed); name != "" {
				named[display.Text(row["id"])] = name
			}
		}
		out = append(out, components.DataRow{TableRow: components.TableRow{ID: display.Text(row["id"]), Cells: cells}})
	}
	return listRowData{out, named, instants, tones}
}

func listContentState(o Options, r Resource, title string, out []components.DataRow) (components.ContentState, []components.DataGroup) {
	state := components.ContentState{}
	groups := []components.DataGroup{{Key: "rows", Rows: out}}
	if len(out) == 0 {
		description := r.reading(o).EmptyDescription
		if description == "" {
			if r.Present.Plural != "" {
				description = o.Text("screens.empty_collection", "No %s yet.", r.reading(o).Plural)
			} else {
				description = o.Text("screens.empty", "No %s yet.", display.Plural(r.Schema.Entity))
			}
		}
		state = components.ContentState{Status: components.MediaEmpty, Title: title, Text: description}
		groups = nil
	}
	return state, groups
}

func listTableSlots(o Options, r Resource, at, sort string, primary entity.Field, data listRowData) components.TableSlots {
	return components.TableSlots{
		// Sorting is a link the server answers, not a script that reorders what
		// is on the page: page two of a table sorted in the browser is page two
		// of the wrong order.
		SortURL: func(c components.TableColumn) string { return at + "?sort=" + next(sort, c.Key) },
		SortState: func(c components.TableColumn) string {
			if strings.TrimPrefix(sort, "-") != c.Key {
				return "none"
			}
			if direction(sort) == "desc" {
				return "descending"
			}
			return "ascending"
		},
		// The first column is the way in. A whole row that is a link cannot
		// hold a link of its own, and a row that is a click handler is not a
		// row a keyboard can reach.
		// The read axis is answered in this one slot rather than beside it: two slots
		// for one cell would be one slot silently winning. A field that names a
		// presentation composes, and the identity column composes *as* the link
		// instead of instead of losing it.
		Cell: func(row components.TableRow, c components.TableColumn) g.Node {
			return listCell(o, r, at, primary, data, row, c)
		},
	}
}

func listCell(o Options, r Resource, at string, primary entity.Field, data listRowData, row components.TableRow, c components.TableColumn) g.Node {
	if f, known := fieldFor(r.Schema.Fields, c.Key); known {
		// The row's own name, which is what a picture that is the row's way
		// in is called by: the same words the plain cell would have put in
		// the link, read off the leading column and its fallback.
		name := display.Text(row.Cells[primary.Name])
		if named, ok := data.named[row.ID]; ok {
			name = named
		}
		if node := compose(f, row, c.Primary, at, o, name); node != nil {
			return node
		}
	}
	if !c.Primary {
		// A time cell is an element with an instant in it, not a string: the reader's own
		// engine says it in the reader's own zone. A time that is not an instant keeps the
		// plain text the cell already holds — no element, no invented date.
		if byField, ok := data.instants[row.ID]; ok {
			if when, ok := byField[c.Key]; ok {
				return timeElement(when)
			}
		}
		if c.Key == r.Present.StatusField {
			return components.Badge(components.BadgeProps{Label: display.Text(row.Cells[c.Key]), Tone: data.tones[row.ID], Dot: true})
		}
		return nil
	}
	text := display.Text(row.Cells[c.Key])
	if name, ok := data.named[row.ID]; ok {
		text = name
	}
	return components.RowLink(components.LinkProps{Label: text, Href: at + "/" + row.ID})
}

type listRowData struct {
	rows     []components.DataRow
	named    map[string]string
	instants map[string]map[string]components.TimeText
	tones    map[string]string
}

func pages(total int64) int { return int((total + PerPage - 1) / PerPage) }

func direction(sort string) string {
	if strings.HasPrefix(sort, "-") {
		return "desc"
	}
	return "asc"
}

// sortable reports whether this column's header offers an order. When the entry
// names its sortable fields, that list is the answer and nothing outside it is
// clickable; when it names none, every field that is not a list is, which is what
// every resource drawn before the declaration existed gets.
func (r Resource) sortable(f entity.Field) bool {
	if f.Type == entity.TypeList || f.Type == entity.TypePicture {
		return false
	}
	if len(r.Present.Sortable) == 0 {
		return true
	}
	for _, name := range r.Present.Sortable {
		if name == f.Name {
			return true
		}
	}
	return false
}

// next is the sort a header click asks for: the same field the other way round,
// or a new field ascending.
func next(sort, field string) string {
	if sort == field {
		return "-" + field
	}
	return field
}
