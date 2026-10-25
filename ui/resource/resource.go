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

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/ui/components"
)

// Resource is what the screens know about one registered entity: its schema,
// with the module, name and API path the screens' own paths derive from, and
// the fields a command of its own writes, which a form shows read-only.
// httpx.Resource carries the same two beside its guarded closures; ui/screens
// hands the renderers this half.
type Resource struct {
	Schema    entity.Schema
	Immutable []string
	// API is the resource's own read address as the kernel composed it — the path
	// its JSON routes answer at — empty for a screen rendered without a kernel
	// behind it. A renderer builds an autosave target from it and from nothing
	// else: the screen's own /app/... address is where a person is, not where the
	// record lives, and a browser that guessed an API path would be guessing which
	// surface its caller may read on.
	API string
	// Present supplies the resource names, reading order, status, empty-state
	// guidance, visibility, sections and supported sorts. The catalogue and web
	// screens resolve the same declarations through Words.
	Present entity.EntryHints
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
	// Operations names the routes this resource's API actually mounted — "list",
	// "read", "create", "update", "delete" — in that order. Empty means all
	// five, which is what every resource written before a Spec could name its
	// operations offers, and what a screen rendered without a kernel behind it
	// (a design export, a golden file) renders.
	//
	// The renderers draw a door only for a verb named here, because a door behind
	// which no route answers is not a door: it is a refusal with a button on it.
	// The value is plain strings rather than httpx.CRUD because this package is
	// pure by docs/adr/0007 and imports no transport; ui/screens carries it across.
	Operations []string
	// Commands are the lifecycle routes this caller may use, in the order the API
	// declared them, each with the path its route was mounted on. Empty for a
	// screen rendered without a caller — a design export, a golden file — because
	// "may I do this?" has no answer there, and a control that would be refused is
	// not worth drawing. ui/screens fills this in from httpx.Resource.
	Commands []Command
	// History is this record's versions, read for the caller the page is rendered
	// for, and nil for a resource whose Spec keeps none. The command it carries is
	// left out of Commands by the adapter, so the panel is the one place a person
	// restores from. ui/screens fills this in from httpx.Resource.History.
	History *HistoryPanel
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
	// PictureSource is the address template a `present:image` cell fetches its
	// bytes from, `{id}` standing for the file id the field holds: the same
	// template the workspace catalogue publishes as `pictures.source`, and for the
	// same reason — the door belongs to a module this package may not name, so the
	// composition that has met both writes the address once here rather than a
	// renderer guessing at it. Empty means this installation names no such door,
	// and every picture cell then draws the plain value it drew before this field
	// existed.
	PictureSource string
	// ComposedSource substitutes {reference} with a validated composed-picture token.
	ComposedSource string
	// Locale is the request's selected language when the shell composes
	// page.Shell.Messages; the adapter sets it per request. The fixed labels
	// — New, Edit, Delete, the count, the pager and the empty state — are
	// looked up under screens.* keys with their English as the fallback, the
	// same seam the sign-in page uses. Nil keeps the English. Entity names
	// stay as the schema humanizes them.
	Locale *locale.Locale
	// Refusal is the verdict a refused write came back with, worded by the layer that
	// renders refusals (ui/page) rather than by the code that caught the error. The
	// render that redraws a form after a refusal shows its heading and tone from here
	// and keeps the sentence it was handed as `detail`; nil is every render that names
	// no verdict — a design export, a shell drawing the writer's own line — and draws
	// what it drew before this field existed.
	Refusal *Refusal
}

// Refusal is one verdict as a form shows it: what the refusal page calls it, and how
// loudly it is announced. Both arrive worded and classified by whoever owns refusals;
// this package renders them and decides neither.
type Refusal struct {
	Heading string
	Tone    string
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

// chromeFormatter is a render's locale as a form's seam for its chrome. A nil
// *locale.Locale sitting in a locale.Formatter interface is not nil to the code
// that asks it, so a render that composed no locale composes no formatter and
// answers its chrome with source text instead of dying on the way there.
func chromeFormatter(selected *locale.Locale) locale.Formatter {
	if selected == nil {
		return nil
	}
	return selected
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
			{Label: o.Home, Href: o.Workspace}, {Label: collection, Href: at}, {Label: here},
		}})
}

// offers reports whether this resource mounted the route for a verb. An empty
// Operations means all five; the same question, stated once on httpx.Resource,
// decides the route table, and this is its answer at the door.
func (r Resource) offers(verb string) bool {
	if len(r.Operations) == 0 {
		return true
	}
	for _, o := range r.Operations {
		if o == verb {
			return true
		}
	}
	return false
}

// known is the field a row is recognised by, for the whole table at once: the field the entity marked
// `ui:"display"` if it declared one, and otherwise the first writable text-shaped field it declares.
//
// A `text` column counts beside a `string` one: both hold a name, and the reference app's own users
// are a `text` table — an entity of text columns that has not marked a field used to answer its own
// heading and its row's only link with a UUID. The mark still wins when both apply; see candidates.
//
// It reads the fields the asking screen shows, which is what callers pass it: an identity selection
// over a whole schema would pick a field that screen does not draw, and the value would come back on
// the screen anyway — in the leading cell, the row's link and the tab title.
func known(fields []entity.Field) entity.Field {
	if c := candidates(fields); len(c) > 0 {
		return c[0]
	}
	if len(fields) > 0 {
		return fields[0]
	}
	return entity.Field{Name: "id"}
}

// candidates is the order a row is named in, in the one place that order is written down: the marked
// field, then every other writable string or text field in schema order. known takes its head for a
// whole table; label walks it for one row, because the field an entity calls its name can be blank in
// a row that has not been filled in yet.
func candidates(fields []entity.Field) []entity.Field {
	var out []entity.Field
	for _, f := range fields {
		if f.Display {
			out = append(out, f)
		}
	}
	for _, f := range fields {
		if f.Display || f.ReadOnly {
			continue // the marked ones are already at the head, and a caller may not write this one
		}
		if f.Type == entity.TypeString || f.Type == entity.TypeText {
			out = append(out, f)
		}
	}
	return out
}

// onList is the schema as a list of rows reads it: every field whose reading puts it on a row, in
// schema order. entity.Field.OnList resolves the two tags that say so (`visibility`, and the older
// `hide:list` a declared visibility overrides); this is where the list asks, once, so the columns it
// draws and the field it leads with cannot disagree about which fields a row has.
func onList(fields []entity.Field) []entity.Field {
	out := make([]entity.Field, 0, len(fields))
	for _, f := range fields {
		if f.OnList() {
			out = append(out, f)
		}
	}
	return out
}

// readable is the schema as the record reads it: `hidden` is the only visibility off this screen,
// `detail` being the field a record answers and a row does not need. A field that is not drawn here
// cannot name the row here either — the record's title is its toolbar, its breadcrumb and the browser
// tab a person bookmarks.
func readable(fields []entity.Field) []entity.Field {
	out := make([]entity.Field, 0, len(fields))
	for _, f := range fields {
		if !f.Presentation.Hidden() {
			out = append(out, f)
		}
	}
	return out
}

// label is what one row is called, for one row: the first of those candidates this row has a value
// for, and the id only when it has nothing else — today's last resort, kept for an entity of no
// text-shaped fields at all.
func label(row map[string]any, fields []entity.Field) string {
	for _, f := range candidates(fields) {
		if v := display.Text(row[f.Name]); v != "" {
			return v
		}
	}
	return display.Text(row["id"])
}
