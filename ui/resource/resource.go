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
	"slices"
	"strconv"
	"strings"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

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
	if writable && r.offers("create") {
		actions = []g.Node{components.Button(components.ButtonProps{Label: o.Text("screens.new", "New %s", reading.Singular), Href: at + "/new"})}
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
	return document.View{Title: title, Body: body}
}

// Detail is one row: every field, in schema order, and the write affordances
// for a caller who may. The document's title is the row's own name, not the
// entity's: a browser tab, a bookmark and a history entry all read it, and
// eleven of them saying "Task" is eleven of them saying nothing.
func Detail(r Resource, o Options, row map[string]any, writable bool) document.View {
	return DetailRichText(r, o, row, writable, nil, nil)
}

// DetailRichText renders prose prepared by the request adapter in its tenant
// transaction, beside the record's standing in each language the tenant speaks
// besides its own.
func DetailRichText(r Resource, o Options, row map[string]any, writable bool, rendered map[string]string,
	locales []entity.LocaleState) document.View {
	at := r.Screen
	// A singleton is reached at its own path, which is the only place its API is
	// reached too: an id in this path would be an id nobody issued, and the row
	// map does not carry one.
	item := at
	if !r.Singleton {
		item = at + "/" + display.Text(row["id"])
	}
	named := r.rowName(row, readable(r.Schema.Fields))
	var actions []g.Node
	if writable {
		if r.offers("update") {
			actions = []g.Node{components.Button(components.ButtonProps{Label: o.Text("screens.edit", "Edit"), Href: item + "/edit"})}
		}
		// A singleton is not created and not removed, whatever Create and Delete
		// answer when they are asked. kit/rest makes those two refuse rather than
		// nil because the generator calls all five closures — which is right for a
		// closure and wrong for a door: a refusal is a sentence about a mistake
		// somebody just made, and an absent door is the same truth told before the
		// mistake. See kit/rest/singleton.go.
		if !r.Singleton && r.offers("delete") {
			actions = append(actions, deleteForm(o, item, r.Schema.Entity))
		}
	}
	body := []g.Node{
		breadcrumb(o, display.Humanize(r.reading(o).Plural), at, named),
		components.Toolbar(components.ToolbarProps{Title: named}, actions...),
		localeStrip(locales),
		details(r, o.Words(r.Schema), row, rendered),
	}
	// Below the record rather than above it: what a person comes to read is the
	// row, and a command is what they may do to it once they have read it.
	body = append(body, commandFormsIn(o, o.Words(r.Schema), r.offeredCommands(), item, false, locales)...)
	body = append(body, historyDisclosure(o, r.History))
	return document.View{Title: named, Body: body}
}

// offeredCommands is the commands to draw a form for: every one this caller may
// use, except the one the history panel carries. The panel is that command's
// control, per version, with the version it names in it; a second form above it
// asking for a version number by hand would be the same door twice, once usable
// and once a guess.
func (r Resource) offeredCommands() []Command {
	if r.History == nil || r.History.Carries == "" {
		return r.Commands
	}
	out := make([]Command, 0, len(r.Commands))
	for _, c := range r.Commands {
		if c.Verb != r.History.Carries {
			out = append(out, c)
		}
	}
	return out
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
	return document.View{Title: title, Status: status, Modules: richtextModules(r), Body: []g.Node{
		breadcrumb(o, display.Humanize(r.reading(o).Plural), at, title),
		components.Toolbar(components.ToolbarProps{Title: title}),
		FormExample(at, r, o, action, title, row, errs, detail, create).Node,
	}}
}

// richtextModule is the editor's browser half, as a path under the shell's asset
// prefix. It is a module and not a controller: a page that draws no rich text
// field carries no tag for it and downloads none of it.
const richtextModule = "js/richtext.js"

// richtextModule asks whether this form draws an editor at all: an editable rich
// text field, which is the only thing that needs 470 KB of editor. A record whose
// rich text is immutable is read-only chrome and no editor, and the page that
// draws one costs the same as the page that draws none.
func richtextModules(r Resource) []string {
	for _, f := range readable(r.Schema.Fields) {
		if f.Widget == "richtext" && !slices.Contains(r.Immutable, f.Name) {
			return []string{richtextModule}
		}
	}
	return nil
}

// fileUploadRoute is the file module's own upload address on the workspace
// surface, spelled rather than derived: the editor posts the bytes it is handed to
// this route and to no other, and never builds a path out of a record's own.
const fileUploadRoute = "/api/v1/file/files"

// fileImageSrc is the same module's own address for reading one stored file back,
// with {file} standing for the id an upload answered with. The editor substitutes
// an id an upload handed it and no other text, so the picture a person just
// uploaded is a picture they can look at while the document, the posted value and
// the line the server validates all keep the pk-file reference they always held.
// Spelled here beside the upload route rather than built from it, for the same
// reason: the browser is handed addresses, never a rule for making one.
const fileImageSrc = "/api/v1/file/files/{file}/content"

// richtextEditor is the server's answer to "what may this field's editor do, and
// where". An author who is writing has no record yet, so there is nothing to keep
// a draft under: the draft key appears with the record's id and nowhere else. No
// write address is offered beside it, because this form is the write: it posts the
// field and quotes the row's own write count as the condition on that post. A
// browser handed the same address would PATCH the field as the person types, move
// the row off the revision the form still carries, and refuse the Save pressed a
// moment later — keeping no draft in which unsaved words would then live.
//
// submitted says this render is the answer to a write that was refused: its
// controls carry the person's own post rather than the record's text. Nothing
// compares a kept draft against that text — a refused write saves nothing, and the
// copy on the device is the only place unsaved words live.
func richtextEditor(r Resource, row map[string]any, create bool, name string, submitted bool) forms.Editor {
	editor := forms.Editor{FileUpload: fileUploadRoute, ImageSrc: fileImageSrc, Submitted: submitted}
	if create || row == nil || r.Singleton || r.API == "" {
		return editor
	}
	id, _ := row["id"].(string)
	if id == "" || !r.offers("update") {
		return editor
	}
	editor.DraftKey = r.API + "/" + id + "/" + name
	return editor
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
	// The form asks for every field its author did not keep off every screen:
	// `hidden` is the one visibility off this screen too, because a control for a
	// field no screen may name is a question nobody may answer, and `detail` stays
	// on it — a required field a form cannot ask for is a record nobody can
	// create. See readable. The one field added after this loop is the edit form's
	// own write condition, below, so the capacity holds room for it.
	fields := make([]forms.Field, 0, len(r.Schema.Fields)+1)
	words := o.Words(r.Schema)
	// A render carrying a refusal line is the form redrawn over a write that was
	// refused, with the person's own words in its controls.
	refused := detail != ""
	for _, field := range readable(r.Schema.Fields) {
		f := formField(words, field)
		if f.Definition.Widget == "richtext" {
			f.Editor = richtextEditor(r, row, create, f.Definition.Name, refused)
		}
		fields = append(fields, f)
	}
	values := make(map[string]string, len(row))
	for name, value := range row {
		values[name] = display.Text(value)
	}
	// An edit carries the row's write count back as a hidden value, so the write it
	// posts can be refused against a row that moved since the person read it. It is a
	// hidden value and not a control for two reasons that are one: nobody may type the
	// number of a row's writes (kit/rest refuses a body that names the field), and a
	// form whose hidden machinery is drawn as a control is a form that invites people to
	// break its own safety. A create quotes nothing: there is no row to have read.
	//
	// The value is the number as the row has it, which is also what a refused write gives
	// back — the person who is told their deadline is invalid resubmits against the
	// revision they were already quoting, not against one they never saw.
	if condition, quoted := revisionCondition(r, row, create); quoted {
		fields = append(fields, forms.Field{Definition: entity.Field{
			Name: entity.RevisionCondition, Type: entity.TypeString, Widget: "hidden",
			Doc: "The revision this form was drawn from. The write is refused if the row has moved.",
		}})
		values[entity.RevisionCondition] = condition
	}
	return forms.MustExample(id, forms.Model{Fields: fields, Values: values, Errors: errs,
		Immutable: r.Immutable, Detail: detail, Create: create}, forms.Options{
		// The address the form posts to is its DOM scope: see forms.Namespace.
		Namespace: forms.Namespace(action),
		Action:    action, CancelURL: r.Screen, Title: title,
		Text: func(key, fallback string) string {
			if o.Locale == nil {
				return fallback
			}
			return o.Locale.Text(key, fallback)
		},
		// The verdict's own name and tone, from the caller that looked them up. A render
		// that names none keeps "That could not be saved" and the fault tone it kept
		// before either field existed.
		FailureTitle: refusalHeading(o), DetailTone: refusalTone(o),
	})
}

// refusalHeading and refusalTone are the two parts of the verdict a form draws, read
// from a render that named none as the empty pair they were drawn from before.
func refusalHeading(o Options) string {
	if o.Refusal == nil {
		return ""
	}
	return o.Refusal.Heading
}

func refusalTone(o Options) string {
	if o.Refusal == nil {
		return ""
	}
	return o.Refusal.Tone
}

// revisionCondition is the edit form's condition on the write: the row's own write count,
// and whether this form may quote one at all. A resource whose entity keeps no count,
// and a form that is not editing a row it read, quote nothing — and the door they post to
// writes as plainly as it always did. A singleton is the second case by declaration: its
// API is a PUT with no tag to quote (kit/rest carries If-Match on the patch and the delete
// alone), so its screen carries a condition its route would not honour.
func revisionCondition(r Resource, row map[string]any, create bool) (string, bool) {
	if create || r.Singleton || row == nil {
		return "", false
	}
	counts := false
	for _, f := range r.Schema.Fields {
		if f.Name == entity.RevisionField && f.Type == entity.TypeInt {
			counts = true
		}
	}
	if !counts {
		// A row that arrived with a revision no field of this schema declares is not this
		// renderer's to speak for; it quotes nothing, and the write is the plain one.
		return "", false
	}
	if count, has := row[entity.RevisionField]; has {
		return display.Text(count), true
	}
	if carried, has := row[entity.RevisionCondition]; has {
		return display.Text(carried), true
	}
	return "", false
}

// Control adapts an entity field to the portable form control, preserving the
// generated screens' existing labels, choices and name-derived DOM identity.
func Control(f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return controlWords(Words{}, f, value, fieldErr, immutable)
}

// controlWords is Control with one thing added: the words a request's language
// chose, which is what a command's argument controls need and what the exported
// Control has no request to read. ui/forms stays this package's only owner of the
// control itself.
func controlWords(words Words, f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return forms.Control(forms.ControlProps{Field: formField(words, f), Value: value, Error: fieldErr, Immutable: immutable})
}

// formField projects display words at the screen boundary. The portable form
// owns controls; the value words keep their owner in kit/entity/display, and
// `words` is what puts them in the request's language — the zero Words reads
// each of them exactly as written.
func formField(words Words, f entity.Field) forms.Field {
	options := make([]components.SelectOption, 0, len(f.Enum))
	for _, value := range f.Enum {
		options = append(options, components.SelectOption{Label: words.EnumWord(f, value), Value: value})
	}
	return forms.Field{Definition: f, Label: words.FieldLabel(f), Help: words.FieldHelp(f), Options: options}
}

// table is the list screen's rows: the field a row is known by first, as the
// link into it, then every other field the schema does not hide.
//
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
	columns := make([]components.TableColumn, 0, len(shown))
	words := o.Words(r.Schema)
	for i, f := range shown {
		columns = append(columns, components.TableColumn{
			Key: f.Name, Label: words.FieldLabel(f), Sortable: r.sortable(f), Primary: i == 0,
		})
	}
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
	state := components.ContentState{}
	groups := []components.DataGroup{{Key: "rows", Rows: out}}
	if len(rows) == 0 {
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
	p := components.DataListProps{
		HTMXProps: components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
		Columns:   columns, Groups: groups, Label: title, State: state, ResultCountText: count, Pagination: pager,
	}
	slots.TableSlots = components.TableSlots{
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
				// The row's own name, which is what a picture that is the row's way
				// in is called by: the same words the plain cell would have put in
				// the link, read off the leading column and its fallback.
				name := display.Text(row.Cells[primary.Name])
				if named, ok := named[row.ID]; ok {
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
				if byField, ok := instants[row.ID]; ok {
					if when, ok := byField[c.Key]; ok {
						return timeElement(when)
					}
				}
				if c.Key == r.Present.StatusField {
					return components.Badge(components.BadgeProps{Label: display.Text(row.Cells[c.Key]), Tone: tones[row.ID], Dot: true})
				}
				return nil
			}
			text := display.Text(row.Cells[c.Key])
			if name, ok := named[row.ID]; ok {
				text = name
			}
			return components.RowLink(components.LinkProps{Label: text, Href: at + "/" + row.ID})
		},
	}
	return components.DataListWithSlots(p, slots)
}

// localeStrip is the record's translation standing: one badge per language this
// tenant speaks besides its own, each naming how much of this record exists in it
// and what it is behind by.
//
// It is the first thing a translator needs on a record — what is reviewed and what
// somebody still owes — and the numbers come from the same read a public page
// answers with, so the badge cannot report a translation a reader would never be
// served. A language nobody has touched reads 0% rather than disappearing: the
// language that needs work is the one worth naming.
//
// The word beside the number is the point of the badge. "50%" asks the reader to
// work out whether somebody owes a first translation or owes a second look at one
// that went stale, and those are different jobs; "50% · Outdated" answers it. The
// tone stays the one the strip has always worn — success when the language is whole,
// warning when it is not — because the word, not the colour, carries which of the
// two it is, and a badge that meant "stale" one release and "never started" the
// next by colour alone would be a colour doing a word's work.
//
// Nothing at all when there is no language to report — an entity with no
// translatable field, or a tenant served in one language — because a badge about a
// language nobody can switch to is noise, and every screen of every entity that was
// never translated would be wearing one.
func localeStrip(locales []entity.LocaleState) g.Node {
	if len(locales) == 0 {
		return g.Raw("")
	}
	badges := make([]g.Node, 0, len(locales))
	for _, state := range locales {
		tone := "warning"
		if state.Percent() == 100 {
			tone = "success"
		}
		badges = append(badges, components.Badge(components.BadgeProps{
			Label: state.Locale + " " + strconv.Itoa(state.Percent()) + "% · " +
				display.Humanize(state.State()),
			Tone: tone, Size: "sm",
		}))
	}
	return components.Flex(components.FlexProps{Gap: "2", Wrap: true}, badges...)
}

// details is the detail screen: every field the author kept on a record, in schema
// order, as a description list. `hide:list` hides nothing here — it is about a table
// being readable, not about a field being secret — and `visibility:hidden` is the one
// declaration that is off this screen too, which readable applies.
func details(r Resource, words Words, row map[string]any, rendered map[string]string) g.Node {
	// One pass, in schema order: richtext becomes a prose section as it always
	// did, and every other readable field becomes a term beside its value.
	var prose []g.Node
	type read struct {
		field entity.Field
		item  components.DetailItem
	}
	var reads []read
	// instantsByLabel pairs a term with the instant behind it, so the value door below can hand
	// that one item a <time>. It is keyed by the term because DetailItem is a label and a string:
	// the term a person reads is the only identity an item has, and it is the same words the <dt>
	// carries. Two fields labelled alike would be two <dt>s nobody could tell apart anyway.
	instantsByLabel := map[string]components.TimeText{}
	for _, f := range readable(r.Schema.Fields) {
		if f.Widget == "richtext" {
			if html, ok := rendered[f.Name]; ok {
				prose = append(prose, h.Section(h.H2(g.Text(words.FieldLabel(f))), components.Prose(components.ProseProps{HTML: html})))
			} else {
				prose = append(prose, h.Section(h.H2(g.Text(words.FieldLabel(f))), h.P(g.Text(display.Text(row[f.Name])))))
			}
			continue
		}
		label := words.FieldLabel(f)
		value := words.Value(f, row[f.Name])
		if f.Type == entity.TypeTime {
			if when, ok := display.Instant(row[f.Name]); ok {
				instantsByLabel[label] = components.TimeText{AtUTC: when, Text: value}
			}
		}
		reads = append(reads, read{field: f,
			item: components.DetailItem{Label: label, Value: value}})
	}
	// Every block of the record opens its terms through the one list, and that list keeps the
	// same value door a list cell uses, so the record and the list state an instant identically
	// whichever section the field landed in. A time whose value is no instant never reaches the
	// map above: it keeps its text.
	list := func(items []components.DetailItem) g.Node {
		return components.DetailListWithSlots(components.DetailListProps{Items: items}, components.DetailListSlots{
			Value: func(item components.DetailItem) g.Node {
				when, ok := instantsByLabel[item.Label]
				if !ok {
					return nil
				}
				return timeElement(when)
			},
		})
	}
	if len(r.Present.Sections) == 0 {
		all := make([]components.DetailItem, 0, len(reads))
		for _, r := range reads {
			all = append(all, r.item)
		}
		return g.Group{list(all), g.Group(prose)}
	}
	// Declared sections are the blocks of the record screen, in the order the
	// author named them. A field naming none is drawn after every one of them, in
	// schema order: an undeclared field that drifted between two blocks somebody
	// placed would be the section list losing an argument it never entered.
	out := make([]g.Node, 0, len(r.Present.Sections)+1)
	taken := map[string]bool{}
	for _, sec := range r.Present.Sections {
		items := make([]components.DetailItem, 0, len(reads))
		for _, r := range reads {
			if r.field.Presentation.Section == sec.Key {
				items = append(items, r.item)
				taken[r.field.Name] = true
			}
		}
		out = append(out, h.Section(h.H2(g.Text(words.Section(sec).Label)), list(items)))
	}
	rest := make([]components.DetailItem, 0, len(reads))
	for _, r := range reads {
		if !taken[r.field.Name] {
			rest = append(rest, r.item)
		}
	}
	if len(rest) > 0 {
		out = append(out, list(rest))
	}
	return g.Group{g.Group(out), g.Group(prose)}
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

// timeElement is one instant as a screen states it: the element every generated
// screen uses, with the instant a machine can read and the exact moment — in the
// zone the server actually wrote it in — a person can. The words inside the
// element are UTC wall time until the reader's own browser says them again in the
// reader's own zone (see ui/assets/js/components.js); the title is what is true
// before that happens, and it stays true afterwards because datetime never moves.
//
// stamp is an instant spelled out with the zone it is stated in. A wall time with
// no zone on it is a guess the reader has to make for themselves.
func timeElement(when components.TimeText) g.Node {
	return components.Time(components.TimeProps{Instant: when, Title: stamp(when.AtUTC)})
}

// stamp is the exact moment with its zone designator: what a person hovering the
// cell is told before the browser has said the same moment in their own zone. It is
// the ISO-8601 instant — `Z`, not the word `UTC` — because a `title` is one of the
// attributes ui/legible reads as what a person reads, and a spelled zone is a word
// no catalogue owns; the same moment in the shape `datetime` carries is data in
// every language.
func stamp(at time.Time) string { return at.UTC().Format(time.RFC3339) }

func pages(total int64) int { return int((total + PerPage - 1) / PerPage) }

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

// next is the sort a header click asks for: the same field the other way round,
// or a new field ascending.
func next(sort, field string) string {
	if sort == field {
		return "-" + field
	}
	return field
}
