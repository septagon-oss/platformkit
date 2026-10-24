// Package resource renders resolved entity policies and supplied rows as HTML.
// It performs no I/O; ui/screens supplies authorized routes and request values.
// See README.md for composition, fallback and value-reading contracts.
package resource

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/entity/present"
	"github.com/septagon-oss/platformkit/kit/locale"
	"github.com/septagon-oss/platformkit/ui/components"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/forms"
)

// Resource supplies a schema, immutable resolved policy and authorized routes.
// When Resolved is absent or unsupported, the original declarations resolve once.
type Resource struct {
	// Resolved is an immutable policy for this render; unsupported versions resolve the original declarations.
	Resolved *present.Resolved
	// CommandsOnly suppresses entity surfaces when resolving a legacy command projection.
	CommandsOnly bool
	Schema       entity.Schema
	Immutable    []string
	// API is the registered JSON read address, or empty for offline rendering.
	// It scopes rich-text drafts without inferring a route from the screen address.
	API string
	// Present supplies legacy declarations used only when Resolved cannot be read.
	Present entity.EntryHints
	// Screen is the registered collection or singleton page address.
	// Offline callers supply their own address; all navigation starts here.
	Screen string
	// Singleton identifies an id-less record surface with no collection or create/delete doors.
	Singleton bool
	// Operations lists mounted CRUD verbs. Nil means all five; an explicit empty
	// set means none. These route capabilities narrow the resolved presentation.
	Operations []string
	// Commands contains only callable routes authorized for this render.
	// The policy supplies their words and flags; a policy alone grants no route.
	Commands []Command
	// History contains the versions already read under the record's guard.
	// Nil hides the panel; Carries avoids duplicating its restore command.
	History *HistoryPanel
}

// PerPage is a screenful, and the page the list renders and links. kit/crud's
// default limit is the same number, so a page of a list and a page of the
// collection route are the same page; ui/screens, which imports both, checks
// that they agree.
const PerPage = 50

// Options is what a shell decides about its generated screens.
type Options struct {
	// Clock is sampled once per render. Nil keeps relative values absolute offline.
	Clock func() time.Time
	// Location is the viewer zone; nil renders UTC before browser enhancement.
	Location *time.Location
	// Filters holds the validated equality selections used by navigation links.
	Filters map[string]string
	now     *time.Time
	// Workspace is the address the breadcrumb's first entry links to: the
	// workspace's own root. It is not a prefix the renderer composes paths
	// from — a screen is mounted at an address the kernel derived, and the
	// renderer is handed it. There used to be a Root here, and every module's
	// nav entry derived its link from it, which is how a shell's location
	// became nine modules' business.
	Workspace string
	// Home is the breadcrumb's first entry's label, linking to Workspace.
	Home string
	// PictureSource substitutes {id} in an authorized picture delivery URL.
	// Empty means this installation offers no stored-picture delivery door.
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

// List renders the resolved collection layout, choices, count and pagination.
// Rows and total must describe the same scoped read; writable permits only
// mounted write operations. Invalid supported policy returns a render error.
func List(r Resource, o Options, rows []map[string]any, total int64, pageNo int, sort string, writable bool) document.View {
	r, o = r.policy(o), o.snapshot()
	if err := r.validatePolicy(); err != nil {
		return document.View{Body: []g.Node{invalid(err)}}
	}
	at := r.Screen
	reading := r.Resolved.Identity
	title := reading.Plural
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
		actions = []g.Node{components.Button(components.ButtonProps{Label: o.Text("screens.new", "New %s", reading.Singular), Href: at + "/new", Variant: r.primaryAction(true)})}
	}
	pager := components.PaginationProps{
		HTMXProps:   components.HTMXProps{Target: "body", Swap: "outerHTML", PushURL: "true"},
		CurrentPage: pageNo, TotalPages: pages(total), BaseURL: listURL(at, sort, o.Filters),
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
	return document.View{Title: title, Language: r.Resolved.Language, Body: body}
}

// Detail renders one row's resolved title, blocks and authorized commands.
// Prepared rich text and locale standing are optional through DetailRichText.
func Detail(r Resource, o Options, row map[string]any, writable bool) document.View {
	return DetailRichText(r, o, row, writable, nil, nil)
}

// DetailRichText renders prose prepared by the request adapter in its tenant
// transaction, beside the record's standing in each language the tenant speaks
// besides its own.
func DetailRichText(r Resource, o Options, row map[string]any, writable bool, rendered map[string]string,
	locales []entity.LocaleState) document.View {
	r, o = r.policy(o), o.snapshot()
	if err := r.validatePolicy(); err != nil {
		return document.View{Body: []g.Node{invalid(err)}}
	}
	at := r.Screen
	// A singleton is reached at its own path, which is the only place its API is
	// reached too: an id in this path would be an id nobody issued, and the row
	// map does not carry one.
	item := at
	if !r.Singleton {
		item = at + "/" + display.Text(row["id"])
	}
	named := present.Title(r.Resolved.Titles.RecordCandidates, row, display.Text(row["id"]), r.Resolved.Titles.EmptyTitle)
	var actions []g.Node
	if writable {
		if r.offers("update") {
			actions = []g.Node{components.Button(components.ButtonProps{Label: o.Text("screens.edit", "Edit"), Href: item + "/edit", Variant: r.primaryAction(false)})}
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
		breadcrumb(o, r.Resolved.Identity.Plural, at, named),
		components.Toolbar(components.ToolbarProps{Title: named}, actions...),
		localeStrip(locales),
		details(r, o, row, rendered),
	}
	// Below the record rather than above it: what a person comes to read is the
	// row, and a command is what they may do to it once they have read it.
	body = append(body, commandFormsIn(o, o.Words(r.Schema), r.offeredCommands(), item, false, locales, row)...)
	body = append(body, historyDisclosure(o, r.History))
	return document.View{Title: named, Language: r.Resolved.Language, Body: body}
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

// Form renders the resolved create or update layout with submitted values.
// Nonempty detail marks a refused write and returns status 422; errs names
// individual controls. Invalid supported policy returns an HTML render error.
func Form(r Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) document.View {
	r, o = r.policy(o), o.snapshot()
	if err := r.validatePolicy(); err != nil {
		return document.View{Body: []g.Node{invalid(err)}}
	}
	at := r.Screen
	status := 0
	if detail != "" {
		status = statusUnprocessableEntity
	}
	return document.View{Title: title, Language: r.Resolved.Language, Status: status, Modules: richtextModules(r), Body: []g.Node{
		breadcrumb(o, r.Resolved.Identity.Plural, at, title),
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
	for _, f := range r.Resolved.Fields {
		if f.Control == "richtext" && (f.Editable.Create || f.Editable.Update) {
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

// richtextEditor scopes an existing record draft without enabling autosave.
// submitted preserves unsaved text after a refused write; create has no draft key.
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

// FormExample captures the same form body as Form for offline design export.
// id names the example; action scopes control identities across instances and
// refusal swaps. Inputs are synthetic values, not persistence operations.
func FormExample(id string, r Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) examples.Example {
	r, o = r.policy(o), o.snapshot()
	if err := r.validatePolicy(); err != nil {
		return examples.Example{Node: invalid(err)}
	}
	fields := make([]forms.Field, 0, len(r.Resolved.Fields)+1)
	immutable := []string{}
	names := r.Resolved.Layout.Update
	if create {
		names = r.Resolved.Layout.Create
	}
	for _, name := range names {
		field, _ := fieldFor(r.Schema.Fields, name)
		f := resolvedFormField(field, r.field(name))
		if f.Definition.Widget == "richtext" {
			f.Editor = richtextEditor(r, row, create, name, detail != "")
		}
		fields = append(fields, f)
	}
	if !create {
		for _, field := range r.Resolved.Fields {
			f, _ := fieldFor(r.Schema.Fields, field.Name)
			if field.OnRecord && !field.Editable.Update && !entity.ServerOwned(f) && !slices.Contains(names, field.Name) {
				immutable = append(immutable, field.Name)
				fields = append(fields, resolvedFormField(f, field))
			}
		}
	}
	values := make(map[string]string, len(row))
	for name, value := range row {
		values[name] = scalarText(value)
	}
	// An edit carries the observed revision, including after a refused write.
	// A create has no observed row and quotes nothing.
	if condition, quoted := revisionCondition(r, row, create); quoted {
		fields = append(fields, forms.Field{Definition: entity.Field{
			Name: entity.RevisionCondition, Type: entity.TypeString, Widget: "hidden",
			Doc: "The revision this form was drawn from. The write is refused if the row has moved.",
		}})
		values[entity.RevisionCondition] = condition
	}
	return forms.MustExample(id, forms.Model{Fields: fields, Values: values, Errors: errs,
		Immutable: immutable, Detail: detail, Create: create}, forms.Options{
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
		return scalarText(count), true
	}
	if carried, has := row[entity.RevisionCondition]; has {
		return display.Text(carried), true
	}
	return "", false
}

// Control adapts an entity field to the portable form control, preserving the
// generated screens' existing labels, choices and name-derived DOM identity.
func Control(f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return forms.Control(forms.ControlProps{Field: formField(Words{}, f), Value: value, Error: fieldErr, Immutable: immutable})
}

// formField resolves a standalone field for compatibility controls and commands.
func formField(words Words, f entity.Field) forms.Field {
	return resolvedFormField(f, words.field(f))
}

// localeStrip names each supplied translation's completion and freshness.
// Untouched languages remain visible; an empty set draws no strip.
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

func pages(total int64) int { return int((total + PerPage - 1) / PerPage) }

// offers reports whether this resource mounted the route for a verb. An empty
// Operations means all five; the same question, stated once on httpx.Resource,
// decides the route table, and this is its answer at the door.
func (r Resource) offers(verb string) bool {
	if r.Operations == nil {
		return true
	}
	for _, o := range r.Operations {
		if o == verb {
			return true
		}
	}
	return false
}
