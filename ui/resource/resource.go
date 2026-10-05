// Package resource renders the screens of a schema-described resource — the
// list, the detail, the two forms, and one control per command the caller was
// given — from plain values: the entity's schema, the rows a handler read, and
// the words the request's locale chose.
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
	"time"

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
	Name      entity.Name
	Immutable []string
	// Screen is the workspace address of this resource's collection screen:
	// /app/<module>/<entity>, composed by the kernel at registration. Every
	// link a renderer writes is built from it, which is the whole reason the
	// renderer needs it and no reason it needs to know how it was made. A
	// screen rendered without a kernel behind it — a design export, a golden
	// file — names one itself.
	Screen string
	// Singleton says a tenant has one of these and its API has no id in the
	// path. A screen needs to know, because the doors a collection shows are
	// the doors this resource has no routes behind: New would post to a create
	// that answers 405, Delete would post to one that answers 409, and a row
	// link would name an id the row does not have. kit/httpx documents the same
	// duty on httpx.Resource.Singleton, and ui/screens/catalog.go has always
	// carried it to the native shell; ui/screens carries it here.
	Singleton bool
	// Commands are the lifecycle routes this caller may use, in the order the API
	// declared them, each with the path its route was mounted on. Empty for a
	// screen rendered without a caller — a design export, a golden file — because
	// "may I do this?" has no answer there, and a control that would be refused is
	// not worth drawing. ui/screens fills this in from httpx.Resource.
	Commands []Command
}

// PerPage is a screenful, and the page the list renders and links. kit/crud's
// default limit is the same number, so a page of a list and a page of the
// collection route are the same page; ui/screens, which imports both, checks
// that they agree.
const PerPage = 50

// Options is what a shell decides about its generated screens.
type Options struct {
	// Workspace is the address the breadcrumb's first entry links to: the
	// workspace's own root. It is not a prefix the renderer composes paths
	// from — a screen is mounted at an address the kernel derived, and the
	// renderer is handed it. There used to be a Root here, and every module's
	// nav entry derived its link from it, which is how a shell's location
	// became nine modules' business.
	Workspace string
	// Home is the breadcrumb's first entry's label, linking to Workspace.
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
// Label is Text with no arguments: the words a screen writes over something —
// a column, a control, a button — rather than a sentence that interpolates one.
// Its own name is the reason it exists: Text reads as a printf to the toolchain,
// and a label built from a field name is not a format string.
func (o Options) Label(key, fallback string) string {
	if o.Locale == nil {
		return fallback
	}
	return o.Locale.Text(key, fallback)
}

func (o Options) Text(key, fallback string, args ...any) string {
	if o.Locale == nil {
		return fmt.Sprintf(fallback, args...)
	}
	return o.Locale.Text(key, fallback, args...)
}

// count is the list's subtitle. The English fallback is already pluralized
// so a shell without a catalog reads exactly as before; a catalog receives
// the number and the noun and applies its own plural rules.
func (o Options) plural(key string, count int64, one, other string, args ...any) string {
	if o.Locale != nil {
		return o.Locale.Plural(key, count, one, other, args...)
	}
	return (locale.Locale{}).Plural(key, count, one, other, args...)
}

func (o Options) noun(r Resource, count int64) string {
	return o.Noun(r.Schema.Module, r.Schema.Entity, r.Name, count)
}

// Noun selects the declared resource name through the request's catalogue.
func (o Options) Noun(module, entityName string, name entity.Name, count int64) string {
	if name.One == "" {
		name.One = display.Humanize(entityName)
	}
	if name.Other == "" {
		name.Other = name.One
	}
	return o.plural("resource."+module+"."+entityName, count, name.One, name.Other)
}

func (o Options) count(total int64, r Resource) string {
	noun := strings.ToLower(o.noun(r, total))
	return o.plural("screens.count", total, "%d %s", "%d %s", noun)
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
	at := r.Screen
	one, title := o.noun(r, 1), o.noun(r, 2)
	var actions []g.Node
	if writable {
		actions = []g.Node{components.Button(components.ButtonProps{Label: o.Text("screens.new", "New %s", one), Href: at + "/new"})}
	}
	body := []g.Node{
		components.Toolbar(components.ToolbarProps{Title: title, Subtitle: o.count(total, r)}, actions...),
		table(o, r, at, title, rows, sort),
	}
	// A command over the whole collection posts beside the list it acts on, and
	// above the pager so a person who changes page has not lost the door.
	body = append(body, commandForms(o, r, at, true)...)
	body = append(body, components.Pagination(components.PaginationProps{
		HTMXProps:   components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
		CurrentPage: pageNo, TotalPages: pages(total), BaseURL: at + "?sort=" + sort,
		NavigationLabel: o.Text("screens.pagination", "Pagination"),
		PreviousLabel:   o.Text("screens.previous", "Previous page"),
		NextLabel:       o.Text("screens.next", "Next page"),
	}))
	return document.View{Title: title, Body: body}
}

// Detail is one row: every field, in schema order, and the write affordances
// for a caller who may. The document's title is the row's own name, not the
// entity's: a browser tab, a bookmark and a history entry all read it, and
// eleven of them saying "Task" is eleven of them saying nothing.
func Detail(r Resource, o Options, row map[string]any, writable bool) document.View {
	at := r.Screen
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
			actions = append(actions, deleteForm(o, item, o.noun(r, 1)))
		}
	}
	body := []g.Node{
		breadcrumb(o, o.noun(r, 2), at, named),
		components.Toolbar(components.ToolbarProps{Title: named}, actions...),
		details(o, r, row),
	}
	// Below the record rather than above it: what a person comes to read is the
	// row, and a command is what they may do to it once they have read it.
	body = append(body, commandForms(o, r, item, false)...)
	return document.View{Title: named, Body: body}
}

// Form is the create and edit screen: one control per writable field, derived
// from the field's type and its ui:"widget:" tag, with the fields a command
// owns shown read-only so a person can see them and not change them here. errs
// and detail are a refused write's, and a form carrying a detail is a 422: the
// kernel rolls the transaction back past 400, and htmx swaps a 422 in place
// rather than treating it as an error nobody sees.
func Form(r Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) document.View {
	at := r.Screen
	status := 0
	if detail != "" {
		status = statusUnprocessableEntity
	}
	return document.View{Title: title, Status: status, Body: []g.Node{
		breadcrumb(o, o.noun(r, 2), at, title),
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
		fields = append(fields, formField(field, o))
	}
	values := make(map[string]string, len(row))
	for name, value := range row {
		values[name] = display.Text(value)
	}
	return forms.MustExample(id, forms.Model{Fields: fields, Values: values, Errors: errs,
		Immutable: r.Immutable, Detail: detail, Create: create}, forms.Options{
		// The address the form posts to is its DOM scope: see forms.Namespace.
		Namespace: forms.Namespace(action),
		Action:    action, CancelURL: r.Screen, Title: title,
		CancelLabel: o.Label("screens.cancel", "Cancel"),
		SubmitLabel: o.Label("screens.save", "Save"),
		ChooseLabel: o.Label("screens.choose", "Choose…"),
	})
}

// Control adapts an entity field to the portable form control, preserving the
// generated screens' existing labels, choices and name-derived DOM identity. It is the
// answer a shell with no catalogue gives: a caller that has a request's language asks
// Options for the same control instead, and the labels, choices and help text come
// through it.
func Control(f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return Options{}.Control(f, value, fieldErr, immutable)
}

// Control is Control with this request's language behind it: the field's label, its
// note and the words of its choices are the catalogue's, and a control drawn on a
// translated page in a language the catalogue speaks is not drawn in English.
func (o Options) Control(f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return forms.Control(forms.ControlProps{Field: formField(f, o), Value: value, Error: fieldErr, Immutable: immutable})
}

// formField projects display words at the screen boundary. The portable form
// owns controls; the value words keep their owner in kit/entity/display.
func formField(f entity.Field, o Options) forms.Field {
	options := make([]components.SelectOption, 0, len(f.Enum))
	for _, value := range f.Enum {
		options = append(options, components.SelectOption{Label: o.Label("enum."+value, display.Humanize(value)), Value: value})
	}
	return forms.Field{Definition: f, Label: o.Label("field."+f.Name, display.FieldLabel(f)),
		// A field's own note is the sentence its owner wrote beside it, and a
		// person reading the form in another language reads that note too.
		Help: o.Label("field."+f.Name+".help", f.Doc), Options: options}
}

// table is the list screen's rows: the field a row is known by first, as the
// link into it, then every other field the schema does not hide.
//
// The id is not a column. It is the row's identity and it is already the link's
// href; a table that leads with a UUID is a table nobody can read.
func table(o Options, r Resource, at, title string, rows []map[string]any, sort string) g.Node {
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
			Key: f.Name, Label: o.Label("field."+f.Name, display.FieldLabel(f)), Sortable: f.Type != entity.TypeList, Primary: i == 0,
		})
	}
	out := make([]components.TableRow, 0, len(rows))
	for _, row := range rows {
		cells := map[string]any{}
		for _, f := range shown {
			cells[f.Name] = fieldValue(o, f, row[f.Name])
		}
		out = append(out, components.TableRow{ID: display.Text(row["id"]), Cells: cells})
	}
	return components.TableWithSlots(components.TableProps{
		HTMXProps: components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
		Sortable:  true, Columns: columns, Rows: out,
		Label:     title,
		EmptyText: o.Text("screens.empty", "No %s yet.", strings.ToLower(o.noun(r, 2))),
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
		// The read axis is answered in this one slot rather than beside it: two slots
		// for one cell would be one slot silently winning. A field that names a
		// presentation composes, and the identity column composes *as* the link
		// instead of instead of losing it.
		Cell: func(row components.TableRow, c components.TableColumn) g.Node {
			if f, known := fieldFor(r.Schema.Fields, c.Key); known {
				if node := compose(f, row, c.Primary, at); node != nil {
					return node
				}
			}
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
//
// The words are the ones the table and the form already ask for, at the same keys:
// a field label is the catalogue's, and a value whose field enumerates is data
// wearing a word this package chose. Reading display.FieldLabel and
// display.Display here instead — which is what this function used to do — made the
// one screen that shows a whole row answer in the language the schema was written
// in, so a tenant served in Portuguese read its record in English while the list
// above it, which asks, read in Portuguese.
func details(o Options, r Resource, row map[string]any) g.Node {
	items := make([]components.DetailItem, 0, len(r.Schema.Fields))
	for _, f := range r.Schema.Fields {
		items = append(items, components.DetailItem{
			Label: o.Label("field."+f.Name, display.FieldLabel(f)),
			Value: fieldValue(o, f, row[f.Name])})
	}
	return components.DetailList(components.DetailListProps{Items: items})
}

// fieldValue is one field of one row as a reader of this request's language reads
// it. The value itself is data; the word for it may be vocabulary. A task whose
// state is "resolved" is data nobody wrote as copy, and the screen that prints
// "Resolved" wrote that word, so the screen is asked for it — at the same key the
// form's option list and the table's cell are asked, because the same value in the
// same tenant must not be two different words on two screens.
//
// An instant is the other value a language writes rather than names. Portuguese
// puts the day before the month and English puts it after, and a record page that
// prints "2026-10-02 13:05" at a tenant served in Portuguese has told that reader
// an order their own language does not use — so the layout is a key of the
// catalogue, and the day-first order is a translation rather than a branch in the
// renderer. kit/entity/display keeps the ISO layout as the source form, which is
// what a shell with no catalogue renders.
//
// A boolean and a measured number are the two other values the renderer, and not
// the row, writes: "Yes" is a word this package chose for `true`, and 12345.5 is a
// shape the request's language decides. Both are asked of the catalogue on the same
// terms as the enum above rather than answered here.
func fieldValue(o Options, f entity.Field, value any) string {
	if f.Type == entity.TypeTime {
		return o.moment(value)
	}
	if value == nil {
		// An absent field is nobody's word and nobody's number: the dash is what
		// every other empty value on this screen says.
		return display.Display(f, value)
	}
	switch f.Type {
	case entity.TypeBool:
		return o.boolean(value)
	case entity.TypeInt, entity.TypeFloat:
		return o.number(value)
	}
	shown := display.Display(f, value)
	if len(f.Enum) > 0 {
		shown = o.Label("enum."+display.Text(value), shown)
	}
	return shown
}

// boolean is the one word a screen writes over a value that has two answers. The
// record says yes or no; the page says it in the reader's language, which is the
// difference between data and copy — and "Yes" printed under pt-PT is copy that
// stopped being true when the page changed language.
func (o Options) boolean(value any) string {
	on, ok := value.(bool)
	if !ok {
		return display.Text(value)
	}
	if on {
		return o.Label("screens.boolean_true", "Yes")
	}
	return o.Label("screens.boolean_false", "No")
}

// number is one measured value in the layout this request's language writes numbers
// in: English counts 12,345.5 with a comma and a point, Portuguese counts it with a
// space and a comma, and the row carries neither. The key is the catalogue's hold on
// the layout, and "%g" the source layout — the same shape of answer a date gets from
// screens.datetime, so a language that groups differently says so in its copy and
// not in this file. With no catalogue the number is what the value itself says.
func (o Options) number(value any) string {
	switch n := value.(type) {
	case float64:
		return o.Text("screens.number", "%g", n)
	case float32:
		return o.Text("screens.number", "%g", float64(n))
	case int:
		return o.Text("screens.number", "%d", n)
	case int8:
		return o.Text("screens.number", "%d", n)
	case int16:
		return o.Text("screens.number", "%d", n)
	case int32:
		return o.Text("screens.number", "%d", n)
	case int64:
		return o.Text("screens.number", "%d", n)
	case uint:
		return o.Text("screens.number", "%d", n)
	case uint8:
		return o.Text("screens.number", "%d", n)
	case uint16:
		return o.Text("screens.number", "%d", n)
	case uint32:
		return o.Text("screens.number", "%d", n)
	case uint64:
		return o.Text("screens.number", "%d", n)
	default:
		return display.Text(value)
	}
}

// moment is one instant in the layout the request's language writes instants in.
// Unparseable is not a thing the screen invents a shape for: what the value says is
// what the screen prints, which is the answer kit/entity/display gives too.
func (o Options) moment(value any) string {
	raw := display.Text(value)
	at, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return raw
	}
	return at.UTC().Format(o.Label("screens.datetime", display.DateTimeLayout))
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
			// The way home is a word of the screen's, not the address: the
			// composition names where home is and the catalogue says what it is called.
			{Label: o.Label("screens.home", o.Home), Href: o.Workspace},
			{Label: collection, Href: at}, {Label: here},
		}})
}

func pages(total int64) int { return int((total + PerPage - 1) / PerPage) }

func direction(sort string) string {
	if strings.HasPrefix(sort, "-") {
		return "desc"
	}
	return "asc"
}

// known is the field a row is recognised by: the first writable string the entity
// declares other than its id. A schema has no "this is the title" flag, and
// inventing one would be a tag every entity would have to remember; the field an
// entity leads with is the one it leads with.
//
// The id is excluded because table() refuses to print it as a column: the id is
// what the row link already carries, and a register whose first column is a UUID is
// a register a person cannot read across. The two rules are one rule, so they are
// written once here rather than in the one place a column list happens to be built —
// a row's label is read off the same field the row leads with.
func known(fields []entity.Field) entity.Field {
	for _, f := range fields {
		if f.Name == "id" {
			continue
		}
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
