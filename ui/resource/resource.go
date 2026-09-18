// Package resource renders the screens of a schema-described resource — the
// list, the detail and the two forms — from plain values: the entity's schema,
// the rows a handler read, and the words the request's locale chose.
//
// It is the pure half of the generated screens behind docs/adr/0007. Nothing
// here names a task or a user, opens a transaction or reads a request; what a
// caller may do arrives as a boolean, and what they read arrives as maps. That
// is what lets the same screens be rendered for design export, for a native
// catalog's golden file or in a test with no database, and it is why the
// package gate holds this package to kit/entity, kit/entity/display,
// kit/locale, ui/forms, ui/document and the presentation packages beneath them.
// ui/screens is the adapter: it takes httpx.Resource, whose guarded closures do
// the reading and writing, and mounts these renderers behind page.Serve.
package resource

import (
	"fmt"
	"strings"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// Resource is what the screens know about one registered entity: its schema,
// with the module, name and API path the screens' own paths derive from, and
// the fields a command of its own writes, which a form shows read-only.
// httpx.Resource carries the same two beside its guarded closures; ui/screens
// hands the renderers this half.
type Resource struct {
	Schema    entity.Schema
	Immutable []string
	// Singleton says a tenant has one of these and its API has no id in the
	// path. A screen needs to know, because the doors a collection shows are
	// the doors this resource has no routes behind: New would post to a create
	// that answers 405, Delete would post to one that answers 409, and a row
	// link would name an id the row does not have. kit/httpx documents the same
	// duty on httpx.Resource.Singleton, and ui/screens/catalog.go has always
	// carried it to the native shell; ui/screens carries it here.
	Singleton bool
}

// PerPage is a screenful, and the page the list renders and links. kit/crud's
// default limit is the same number, so a page of a list and a page of the
// collection route are the same page; ui/screens, which imports both, checks
// that they agree.
const PerPage = 50

// Options is what a shell decides about its generated screens.
type Options struct {
	// Root is where the screens live: Root + the API path without /api/v1.
	Root string
	// Home is the breadcrumb's first entry, linking to Root.
	Home string
	// Locale is the request's selected language when the shell composes
	// page.Shell.Messages; the adapter sets it per request. The fixed labels
	// — New, Edit, Delete, the count, the pager and the empty state — are
	// looked up under screens.* keys with their English as the fallback, the
	// same seam the sign-in page uses. Nil keeps the English. Entity names
	// stay as the schema humanizes them.
	Locale *locale.Locale
}

// Text is one label through the seam: the catalog's translation of key, or
// fallback formatted with args when no catalog is composed or lacks the key.
// The adapter reads it for the titles its mounted routes name.
func (o Options) Text(key, fallback string, args ...any) string {
	if o.Locale == nil {
		return fmt.Sprintf(fallback, args...)
	}
	return o.Locale.Text(key, fallback, args...)
}

// count is the list's subtitle. The English fallback is already pluralized
// so a shell without a catalog reads exactly as before; a catalog receives
// the number and the noun and applies its own plural rules.
func (o Options) count(total int64, noun string) string {
	noun = strings.ToLower(noun)
	if total != 1 {
		noun += "s"
	}
	return o.Text("screens.count", "%d %s", total, noun)
}

// Path is where a resource's list screen lives: /api/v1/task/tasks is served at
// Root/task/tasks, which is what every module's nav entry already says.
func Path(r Resource, o Options) string {
	if rest, ok := strings.CutPrefix(r.Schema.Path, "/api/v1"); ok {
		return o.Root + rest
	}
	return o.Root + "/" + r.Schema.Module + "/" + r.Schema.Entity
}

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
	at, title := Path(r, o), display.Humanize(r.Schema.Entity)
	var actions []g.Node
	if writable {
		actions = []g.Node{components.Button(components.ButtonProps{Label: o.Text("screens.new", "New %s", r.Schema.Entity), Href: at + "/new"})}
	}
	return document.View{Title: title + "s", Body: []g.Node{
		components.Toolbar(components.ToolbarProps{Title: title + "s", Subtitle: o.count(total, title)}, actions...),
		table(o, r, at, rows, sort),
		components.Pagination(components.PaginationProps{
			HTMXProps:   components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
			CurrentPage: pageNo, TotalPages: pages(total), BaseURL: at + "?sort=" + sort,
			NavigationLabel: o.Text("screens.pagination", "Pagination"),
			PreviousLabel:   o.Text("screens.previous", "Previous page"),
			NextLabel:       o.Text("screens.next", "Next page"),
		}),
	}}
}

// Detail is one row: every field, in schema order, and the write affordances
// for a caller who may. The document's title is the row's own name, not the
// entity's: a browser tab, a bookmark and a history entry all read it, and
// eleven of them saying "Task" is eleven of them saying nothing.
func Detail(r Resource, o Options, row map[string]any, writable bool) document.View {
	at := Path(r, o)
	// A singleton is reached at its own path, which is the only place its API is
	// reached too: an id in this path would be an id nobody issued, and the row
	// map does not carry one.
	item := at
	if !r.Singleton {
		item = at + "/" + display.Text(row["id"])
	}
	named := label(row, r.Schema.Fields)
	var actions []g.Node
	if writable {
		actions = []g.Node{components.Button(components.ButtonProps{Label: o.Text("screens.edit", "Edit"), Href: item + "/edit"})}
		// A singleton is not created and not removed, whatever Create and Delete
		// answer when they are asked. kit/rest makes those two refuse rather than
		// nil because the generator calls all five closures — which is right for a
		// closure and wrong for a door: a refusal is a sentence about a mistake
		// somebody just made, and an absent door is the same truth told before the
		// mistake. See kit/rest/singleton.go.
		if !r.Singleton {
			actions = append(actions, deleteForm(o, item, r.Schema.Entity))
		}
	}
	return document.View{Title: named, Body: []g.Node{
		breadcrumb(o, display.Humanize(r.Schema.Entity)+"s", at, named),
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
func Form(r Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) document.View {
	at := Path(r, o)
	status := 0
	if detail != "" {
		status = statusUnprocessableEntity
	}
	return document.View{Title: title, Status: status, Body: []g.Node{
		breadcrumb(o, display.Humanize(r.Schema.Entity)+"s", at, title),
		components.Toolbar(components.ToolbarProps{Title: title}),
		FormExample(at, r, o, action, title, row, errs, detail, create).Node,
	}}
}

// statusUnprocessableEntity is the one status this package writes: the 422 a
// refused form answers with. It is spelled here rather than read from net/http
// because a renderer of values links no HTTP server.
const statusUnprocessableEntity = 422

// FormExample captures the same form body that Form serves, without its page
// chrome. Supply an owner-local example ID and synthetic row/error inputs for
// design export. Children retain Core's typed contracts and named slots; fields
// use "field/" plus their schema name so actions and errors cannot shadow them.
// Property edits produce presentation candidates, not schema or database writes.
// Native editor support and interactive flow behavior require separate evidence.
//
// The form's DOM scope comes from action, not from id: two screens of one entity
// (create and edit) post to different addresses and so own different scopes, and
// the refused POST that swaps errors back in is answered at the address the form
// was drawn from, so the swap still finds its target.
func FormExample(id string, r Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) examples.Example {
	fields := make([]forms.Field, 0, len(r.Schema.Fields))
	for _, field := range r.Schema.Fields {
		fields = append(fields, formField(field))
	}
	values := make(map[string]string, len(row))
	for name, value := range row {
		values[name] = display.Text(value)
	}
	return forms.MustExample(id, forms.Model{Fields: fields, Values: values, Errors: errs,
		Immutable: r.Immutable, Detail: detail, Create: create}, forms.Options{
		// The address the form posts to is its DOM scope: see forms.Namespace.
		Namespace: forms.Namespace(action),
		Action:    action, CancelURL: Path(r, o), Title: title,
	})
}

// Control adapts an entity field to the portable form control, preserving the
// generated screens' existing labels, choices and name-derived DOM identity.
func Control(f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return forms.Control(forms.ControlProps{Field: formField(f), Value: value, Error: fieldErr, Immutable: immutable})
}

// formField projects display words at the screen boundary. The portable form
// owns controls; the value words keep their owner in kit/entity/display.
func formField(f entity.Field) forms.Field {
	options := make([]components.SelectOption, 0, len(f.Enum))
	for _, value := range f.Enum {
		options = append(options, components.SelectOption{Label: display.Humanize(value), Value: value})
	}
	return forms.Field{Definition: f, Label: display.FieldLabel(f), Options: options}
}

// table is the list screen's rows: the field a row is known by first, as the
// link into it, then every other field the schema does not hide.
//
// The id is not a column. It is the row's identity and it is already the link's
// href; a table that leads with a UUID is a table nobody can read.
func table(o Options, r Resource, at string, rows []map[string]any, sort string) g.Node {
	primary := known(r.Schema.Fields)
	shown := []entity.Field{primary}
	for _, f := range r.Schema.Fields {
		if f.HideList || f.Name == primary.Name || f.Name == "id" {
			continue
		}
		shown = append(shown, f)
	}
	columns := make([]components.TableColumn, 0, len(shown))
	for i, f := range shown {
		columns = append(columns, components.TableColumn{
			Key: f.Name, Label: display.FieldLabel(f), Sortable: f.Type != entity.TypeList, Primary: i == 0,
		})
	}
	out := make([]components.TableRow, 0, len(rows))
	for _, row := range rows {
		cells := map[string]any{}
		for _, f := range shown {
			cells[f.Name] = display.Display(f, row[f.Name])
		}
		out = append(out, components.TableRow{ID: display.Text(row["id"]), Cells: cells})
	}
	return components.TableWithSlots(components.TableProps{
		HTMXProps: components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
		Sortable:  true, Columns: columns, Rows: out,
		EmptyText: o.Text("screens.empty", "No %ss yet.", r.Schema.Entity),
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
				Label: display.Text(row.Cells[c.Key]), Href: at + "/" + row.ID})
		},
	})
}

// details is the detail screen: every field, in schema order, as a description
// list. There is no hiding here — hide:list is about a table being readable,
// not about a field being secret, and a field a caller may not see is a field
// the entity's JSON does not carry.
func details(r Resource, row map[string]any) g.Node {
	items := make([]components.DetailItem, 0, len(r.Schema.Fields))
	for _, f := range r.Schema.Fields {
		items = append(items, components.DetailItem{Label: display.FieldLabel(f), Value: display.Display(f, row[f.Name])})
	}
	return components.DetailList(components.DetailListProps{Items: items})
}

// deleteForm is the destructive action: a real form, so it works without
// JavaScript, carrying the attribute confirm.js opens the dialog on.
func deleteForm(o Options, item, entity string) g.Node {
	remove := o.Text("screens.delete", "Delete")
	return components.Form(components.FormProps{Action: item + "/delete", Label: o.Text("screens.delete_this", "Delete this %s", entity)},
		components.Button(components.ButtonProps{
			ComponentProps: components.ComponentProps{Attrs: map[string]string{
				"data-confirm":       o.Text("screens.delete_confirm", "This deletes the %s. It cannot be undone.", entity),
				"data-confirm-label": remove,
			}},
			Label: remove, Type: "submit", Tone: "danger", Size: "md",
		}))
}

func breadcrumb(o Options, collection, at, here string) g.Node {
	return components.Breadcrumb(components.BreadcrumbProps{
		NavigationLabel: o.Text("screens.breadcrumb", "Breadcrumb"),
		Items: []components.BreadcrumbItem{
			{Label: o.Home, Href: o.Root}, {Label: collection, Href: at}, {Label: here},
		}})
}

func pages(total int64) int { return int((total + PerPage - 1) / PerPage) }

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
func known(fields []entity.Field) entity.Field {
	for _, f := range fields {
		if !f.ReadOnly && f.Type == entity.TypeString {
			return f
		}
	}
	if len(fields) > 0 {
		return fields[0]
	}
	return entity.Field{Name: "id"}
}

// label is what one row is called.
func label(row map[string]any, fields []entity.Field) string {
	if v := display.Text(row[known(fields).Name]); v != "" {
		return v
	}
	return display.Text(row["id"])
}

// next is the sort a header click asks for: the same field the other way round,
// or a new field ascending.
func next(sort, field string) string {
	if sort == field {
		return "-" + field
	}
	return field
}
