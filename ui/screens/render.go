// Package screens generates the screens of any resource kit/rest registered,
// for any shell that mounts them: the list, the detail, the two forms, the two
// writes and the delete. Nothing here names a task or a user, which is the
// claim behind docs/adr/0007: adding an entity adds seven screens and no code.
//
// The renderers are pure functions of a resource and what a handler read; Mount
// is the fold that puts them behind page.Serve. Describe is the same knowledge
// as a document, for a shell that is not a browser.
package screens

import (
	"net/http"
	"strconv"
	"strings"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/forms"
	"github.com/septagon-oss/platformkit/ui/page"
)

// Options is what a shell decides about its generated screens.
type Options struct {
	// Root is where the screens live: Root + the API path without /api/v1.
	Root string
	// Home is the breadcrumb's first entry, linking to Root.
	Home string
}

// Path is where a resource's list screen lives: /api/v1/task/tasks is served at
// Root/task/tasks, which is what every module's nav entry already says.
func Path(r httpx.Resource, o Options) string {
	if rest, ok := strings.CutPrefix(r.Path, "/api/v1"); ok {
		return o.Root + rest
	}
	return o.Root + "/" + r.Module + "/" + r.Entity
}

// List is the list screen: a toolbar with the count, the rows, the pager. The
// New button is drawn only for a caller who may write — a person who may not is
// not shown a form that would refuse them.
func List(r httpx.Resource, o Options, rows []map[string]any, total int64, pageNo int, sort string, writable bool) page.View {
	at, title := Path(r, o), rest.Humanize(r.Entity)
	var actions []g.Node
	if writable {
		actions = []g.Node{components.Button(components.ButtonProps{Label: "New " + r.Entity, Href: at + "/new"})}
	}
	return page.View{Title: title + "s", Body: []g.Node{
		components.Toolbar(components.ToolbarProps{Title: title + "s", Subtitle: plural(total, title)}, actions...),
		table(r, at, rows, sort),
		components.Pagination(components.PaginationProps{
			HTMXProps:   components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
			CurrentPage: pageNo, TotalPages: pages(total), BaseURL: at + "?sort=" + sort,
		}),
	}}
}

// Detail is one row: every field, in schema order, and the write affordances
// for a caller who may. The document's title is the row's own name, not the
// entity's: a browser tab, a bookmark and a history entry all read it, and
// eleven of them saying "Task" is eleven of them saying nothing.
func Detail(r httpx.Resource, o Options, row map[string]any, writable bool) page.View {
	at := Path(r, o)
	item := at + "/" + rest.Text(row["id"])
	named := label(row, r.Schema.Fields)
	var actions []g.Node
	if writable {
		actions = []g.Node{
			components.Button(components.ButtonProps{Label: "Edit", Href: item + "/edit"}),
			deleteForm(item, r.Entity),
		}
	}
	return page.View{Title: named, Body: []g.Node{
		breadcrumb(o, rest.Humanize(r.Entity)+"s", at, named),
		components.Toolbar(components.ToolbarProps{Title: named}, actions...),
		details(r, row),
	}}
}

// Form is the create and edit screen: one control per writable field, derived
// from the field's type and its ui:"widget:" tag, with the fields a command
// owns shown read-only so a person can see them and not change them here. errs
// and detail are a refused write's, and a form carrying a detail is a 422: the
// kernel rolls the transaction back past 400, and htmx swaps a 422 in place
// rather than treating it as an error nobody sees.
func Form(r httpx.Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) page.View {
	at := Path(r, o)
	status := 0
	if detail != "" {
		status = http.StatusUnprocessableEntity
	}
	return page.View{Title: title, Status: status, Body: []g.Node{
		breadcrumb(o, rest.Humanize(r.Entity)+"s", at, title),
		components.Toolbar(components.ToolbarProps{Title: title}),
		FormExample("screen-form", r, o, action, title, row, errs, detail, create).Node,
	}}
}

// FormExample captures the same form body that Form serves, without its page
// chrome. Supply an owner-local example ID and synthetic row/error inputs for
// design export. Children retain Core's typed contracts and named slots; fields
// use "field/" plus their schema name so actions and errors cannot shadow them.
// Property edits produce presentation candidates, not schema or database writes.
// Native editor support and interactive flow behavior require separate evidence.
func FormExample(id string, r httpx.Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) examples.Example {
	fields := make([]forms.Field, 0, len(r.Schema.Fields))
	for _, field := range r.Schema.Fields {
		fields = append(fields, formField(field))
	}
	values := make(map[string]string, len(row))
	for name, value := range row {
		values[name] = rest.Text(value)
	}
	return forms.LegacyExample(id, forms.Model{Fields: fields, Values: values, Errors: errs,
		Immutable: r.Immutable, Detail: detail, Create: create}, forms.Options{
		Action: action, CancelURL: Path(r, o), Title: title,
	})
}

// Control adapts an entity field to the portable form control, preserving the
// generated screens' existing labels, choices and name-derived DOM identity.
func Control(f crud.Field, value, fieldErr string, immutable bool) g.Node {
	return forms.Control(forms.ControlProps{Field: formField(f), Value: value, Error: fieldErr, Immutable: immutable})
}

// formField projects display words at the screen boundary. The portable form
// owns controls, while REST's existing field formatting keeps its current owner.
func formField(f crud.Field) forms.Field {
	options := make([]components.SelectOption, 0, len(f.Enum))
	for _, value := range f.Enum {
		options = append(options, components.SelectOption{Label: rest.Humanize(value), Value: value})
	}
	return forms.Field{Definition: f, Label: rest.FieldLabel(f), Options: options}
}

// table is the list screen's rows: the field a row is known by first, as the
// link into it, then every other field the schema does not hide.
//
// The id is not a column. It is the row's identity and it is already the link's
// href; a table that leads with a UUID is a table nobody can read.
func table(r httpx.Resource, at string, rows []map[string]any, sort string) g.Node {
	primary := known(r.Schema.Fields)
	shown := []crud.Field{primary}
	for _, f := range r.Schema.Fields {
		if f.HideList || f.Name == primary.Name || f.Name == "id" {
			continue
		}
		shown = append(shown, f)
	}
	columns := make([]components.TableColumn, 0, len(shown))
	for i, f := range shown {
		columns = append(columns, components.TableColumn{
			Key: f.Name, Label: rest.FieldLabel(f), Sortable: f.Type != crud.TypeList, Primary: i == 0,
		})
	}
	out := make([]components.TableRow, 0, len(rows))
	for _, row := range rows {
		cells := map[string]any{}
		for _, f := range shown {
			cells[f.Name] = rest.Display(f, row[f.Name])
		}
		out = append(out, components.TableRow{ID: rest.Text(row["id"]), Cells: cells})
	}
	return components.TableWithSlots(components.TableProps{
		HTMXProps: components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
		Sortable:  true, Columns: columns, Rows: out,
		EmptyText: "No " + r.Entity + "s yet.",
	}, components.TableSlots{
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
		Cell: func(row components.TableRow, c components.TableColumn) g.Node {
			if !c.Primary {
				return nil
			}
			return components.Link(components.LinkProps{
				Label: rest.Text(row.Cells[c.Key]), Href: at + "/" + row.ID})
		},
	})
}

// details is the detail screen: every field, in schema order, as a description
// list. There is no hiding here — hide:list is about a table being readable,
// not about a field being secret, and a field a caller may not see is a field
// the entity's JSON does not carry.
func details(r httpx.Resource, row map[string]any) g.Node {
	items := make([]components.DetailItem, 0, len(r.Schema.Fields))
	for _, f := range r.Schema.Fields {
		items = append(items, components.DetailItem{Label: rest.FieldLabel(f), Value: rest.Display(f, row[f.Name])})
	}
	return components.DetailList(components.DetailListProps{Items: items})
}

// deleteForm is the destructive action: a real form, so it works without
// JavaScript, carrying the attribute confirm.js opens the dialog on.
func deleteForm(item, entity string) g.Node {
	return components.Form(components.FormProps{Action: item + "/delete", Label: "Delete this " + entity},
		components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"data-confirm":       "This deletes the " + entity + ". It cannot be undone.",
				"data-confirm-label": "Delete",
			}},
			Label: "Delete", Type: "submit", Tone: "danger", Size: "md",
		}))
}

func breadcrumb(o Options, collection, at, here string) g.Node {
	return components.Breadcrumb(components.BreadcrumbProps{Items: []components.BreadcrumbItem{
		{Label: o.Home, Href: o.Root}, {Label: collection, Href: at}, {Label: here},
	}})
}

func pages(total int64) int { return int((total + perPage - 1) / perPage) }

func plural(total int64, noun string) string {
	if total == 1 {
		return "1 " + strings.ToLower(noun)
	}
	return strconv.FormatInt(total, 10) + " " + strings.ToLower(noun) + "s"
}

func direction(sort string) string {
	if strings.HasPrefix(sort, "-") {
		return "desc"
	}
	return "asc"
}

// known is the field a row is recognised by: the first writable string the
// entity declares. A schema has no "this is the title" flag, and inventing one
// would be a tag every entity would have to remember; the field an entity leads
// with is the one it leads with.
func known(fields []crud.Field) crud.Field {
	for _, f := range fields {
		if !f.ReadOnly && f.Type == crud.TypeString {
			return f
		}
	}
	if len(fields) > 0 {
		return fields[0]
	}
	return crud.Field{Name: "id"}
}

// label is what one row is called.
func label(row map[string]any, fields []crud.Field) string {
	if v := rest.Text(row[known(fields).Name]); v != "" {
		return v
	}
	return rest.Text(row["id"])
}

// next is the sort a header click asks for: the same field the other way round,
// or a new field ascending.
func next(sort, field string) string {
	if sort == field {
		return "-" + field
	}
	return field
}
