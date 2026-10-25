// Package screens generates the screens of any resource kit/rest registered,
// for any shell that mounts them: the list, the detail, the two forms, the two
// writes and the delete. Nothing here names a task or a user, which is the
// claim behind docs/adr/0007: adding an entity adds seven screens and no code.
//
// The renderers are ui/resource's, pure functions of a schema and what a
// handler read; this package is the adapter that takes httpx.Resource — the
// same schema beside its guarded closures — and Mount is the fold that puts the
// renderers behind page.Serve. Describe is the same knowledge as a document,
// for a shell that is not a browser.
//
// The claim "adding an entity adds screens and no code" is true of a collection,
// which gets seven. A resource that is not a collection gets the screens its routes
// answer and no more, because a door behind which no route answers is not a screen,
// it is a refusal with a button on it.
package screens

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	g "maragu.dev/gomponents"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/entity/display"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/richtext"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/page"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Options is what a shell decides about its generated screens. See
// resource.Options; Mount sets Locale per request from page.Request.
type Options = resource.Options

// described is the half of a registered resource the renderers read: its schema,
// the fields a command owns, and whether the tenant has one of these or a shelf of
// them. The guarded closures stay with this adapter — that is the boundary docs/
// adr/0007 draws, and ui/resource's package doc holds it.
//
// The last field is the reason this is a function and not a struct literal at each
// call site. Singleton was carried to the native shell (catalog.go) and never to the
// web ones, which is how a tenant's one row of settings came to be rendered as a
// shelf of one, with a New button that answered 405 and a Delete that answered 409.
// A field the renderers read has to be delivered here, and
// TestTheAdapterCarriesEveryFieldTheRenderersRead fails when one is not.
func described(r httpx.Resource) resource.Resource {
	return resource.Resource{Schema: r.Schema, Name: r.Name, Immutable: r.Immutable, Singleton: r.Singleton,
		Screen: r.Screen, Operations: screenOperations(r), Present: r.Present, API: r.Path}
}

// writeAvailable is structural: request authority is checked by the mounted guard.
func writeAvailable(r httpx.Resource, verb httpx.CRUD) bool {
	if !r.WriteAuth().Declared() || !r.Offers(verb) {
		return false
	}
	switch verb {
	case httpx.CRUDCreate:
		return !r.Singleton && r.Create != nil
	case httpx.CRUDUpdate:
		return r.Update != nil
	case httpx.CRUDDelete:
		return !r.Singleton && r.Delete != nil
	}
	return false
}

// screenOperations keeps an explicit empty set distinct from the all-verbs default.
func screenOperations(r httpx.Resource) []string {
	out := make([]string, 0, 5)
	for _, verb := range httpx.CRUDValues() {
		if verb == httpx.CRUDList || verb == httpx.CRUDRead {
			if r.Offers(verb) {
				out = append(out, string(verb))
			}
		} else if writeAvailable(r, verb) {
			out = append(out, string(verb))
		}
	}
	return out
}

// view is the whole of what a mounted screen knows: the resource's own shape, and
// the lifecycle routes *this caller* may use. It is the adapter's only place where an
// authorized httpx.Resource becomes something a renderer draws, which is also why a
// command appears on one person's record page and not another's.
//
// at is the list path, which is where a collection command's route is mounted and
// what the renderer needs to build an action without knowing how paths are derived.
func view(r httpx.Resource, ctx context.Context, at string) resource.Resource {
	v := described(r)
	v.Commands = commands(r, ctx, at)
	return v
}

// commands is the projection from a recorded httpx.Command to the plain value a
// screen draws, filtered by what the caller may do. Two things are dropped here and
// both matter: the guard, because CommandsFor has already applied it per caller, and
// the closure, because ui/resource must not be able to write — see docs/adr/0007 and
// httpx.Command.Run.
//
// A command with no Run is left out entirely. It is a description of a route nobody
// wired, and a form that posts to a route that performs nothing is a lie with a
// button on it.
func commands(r httpx.Resource, ctx context.Context, at string) []resource.Command {
	available := r.CommandsFor(ctx)
	out := make([]resource.Command, 0, len(available))
	for _, c := range available {
		if c.Run == nil {
			continue
		}
		// A `system` command is one no person is offered, so no browser route is
		// mounted for it. The JSON route keeps its guard unchanged — this narrows
		// availability, never authority — and a page that offered it would be
		// offering an action its author said belongs to an operator.
		if c.Present.System {
			continue
		}
		label := c.Present.Label
		if label == "" {
			// The Summary is the label because nothing else was ever offered:
			// the button word and the API document's own sentence have always
			// been one string here, and an author who wants them to differ now
			// has somewhere to say so.
			label = c.Summary
		}
		out = append(out, resource.Command{
			Bindings: c.Bindings,
			Verb:     c.Verb, Label: label, Description: c.Description,
			Fields: c.Fields, Collection: c.Collection, Base: at, Present: c.Present,
		})
	}
	return out
}

// listView, detailView and formView are the same renderers reached with a caller in
// hand. Mount uses them; the exported trio below is what a caller without a request
// renders — a design export, a golden file, a test — and so carries no commands,
// because "may this person resolve the task" has no answer where there is no person.
func listView(r httpx.Resource, ctx context.Context, o Options, at string, rows []map[string]any, total int64, pageNo int, sort string, writable bool) page.View {
	return resource.List(view(r, ctx, at), o, rows, total, pageNo, sort, writable)
}

func detailView(r httpx.Resource, ctx context.Context, o Options, at string, row map[string]any, writable bool) (page.View, error) {
	rendered := map[string]string{}
	for _, field := range r.Schema.Fields {
		if field.Widget != "richtext" {
			continue
		}
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return page.View{}, richtext.ErrMissing
		}
		source, _ := row[field.Name].(string)
		doc, err := richtext.Parse(source)
		if err != nil {
			return page.View{}, err
		}
		html, err := richtext.Render(ctx, tx, doc, r.RichTextFiles, richtext.Workspace)
		if err != nil {
			return page.View{}, err
		}
		rendered[field.Name] = html
	}
	locales, err := localeStates(ctx, r, row)
	if err != nil {
		return page.View{}, err
	}
	v := view(r, ctx, at)
	v.History = historyPanel(r, ctx, o, at, row)
	return resource.DetailRichText(v, o, row, writable, rendered, locales), nil
}

// localeStates is the one language question the record screen asks, asked of the
// resource that mounted it. A resource whose entity translates nothing answers nil
// and its screen says nothing about languages; a resource that does answers in the
// request's own transaction, which is why this is the adapter's work and the
// renderer never sees a closure.
//
// The id is the row's own rather than the path's, because a singleton's screen is
// reached at a path with no id in it and its row has one all the same.
func localeStates(ctx context.Context, r httpx.Resource, row map[string]any) ([]entity.LocaleState, error) {
	if r.Locales == nil {
		return nil, nil
	}
	id, err := uuid.Parse(display.Text(row["id"]))
	if err != nil {
		return nil, fmt.Errorf("screens: the %s row's id is %q, which is no id: %w", r.Entity, display.Text(row["id"]), err)
	}
	return r.Locales(ctx, id)
}

// List is the list screen of a registered resource. See resource.List.
func List(r httpx.Resource, o Options, rows []map[string]any, total int64, pageNo int, sort string, writable bool) page.View {
	return resource.List(described(r), o, rows, total, pageNo, sort, writable)
}

// Detail is one row of a registered resource. See resource.Detail.
func Detail(r httpx.Resource, o Options, row map[string]any, writable bool) page.View {
	// No caller and no request, so no language question gets asked: a design
	// export and a golden file render the record, not its translation standing.
	return resource.DetailRichText(described(r), o, row, writable, nil, nil)
}

// Form is the create and edit screen of a registered resource. See resource.Form.
func Form(r httpx.Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) page.View {
	return resource.Form(described(r), o, action, title, row, errs, detail, create)
}

// FormExample captures the form body Form serves, without its page chrome, for
// design export. See resource.FormExample.
func FormExample(id string, r httpx.Resource, o Options, action, title string, row map[string]any, errs map[string]string, detail string, create bool) examples.Example {
	return resource.FormExample(id, described(r), o, action, title, row, errs, detail, create)
}

// Control adapts an entity field to the portable form control. See resource.Control.
func Control(f entity.Field, value, fieldErr string, immutable bool) g.Node {
	return resource.Control(f, value, fieldErr, immutable)
}

// historyPanel is the record's own past, read for the caller standing in front of
// this page and drawn beside the row it describes. A resource whose Spec keeps no
// versions answers nil, which is the whole of what the renderer needs to know. A
// series that could not be read leaves the record on its own rather than failing
// the page over it: the row is what the person came for, and the panel is the part
// they can ask for again.
//
// The row's own write count goes with the panel as the condition every restore
// below quotes, because the page that read the row is the page that can say so.
func historyPanel(r httpx.Resource, ctx context.Context, o Options, at string, row map[string]any) *resource.HistoryPanel {
	if r.History == nil || r.History.Versions == nil {
		return nil
	}
	id, err := uuid.Parse(strings.TrimSpace(rest.Text(row["id"])))
	if err != nil {
		return nil
	}
	stored, err := r.History.Versions(ctx, id)
	if err != nil {
		return nil
	}
	out := &resource.HistoryPanel{
		Action:  at + "/" + id.String() + "/" + r.History.RestoreVerb,
		Carries: r.History.RestoreVerb,
		Title:   o.Text("screens.history", "History"),
		Restore: o.Text("screens.history.restore", "Restore this version"),
		// The consequence comes from the command, not from this page: the panel asks in
		// the words whoever mounted the resource wrote, and asks nothing when they
		// declared none.
		Asking: r.History.Asks,
	}
	if revision, numbered := row[entity.RevisionField]; numbered {
		out.Condition = display.Text(revision)
	}
	// What each version held, indexed by its number, because the comparison one
	// version offers is against the one before it and the series arrives newest
	// first. A number missing from the index is a version retention released, or the
	// beginning of the series: both answer "nothing to compare against".
	held := make(map[int64]map[string]any, len(stored))
	for _, v := range stored {
		document := map[string]any{}
		decoder := json.NewDecoder(strings.NewReader(string(v.Fields)))
		decoder.UseNumber()
		_ = decoder.Decode(&document)
		held[v.Version] = document
	}
	for _, v := range stored {
		item := resource.VersionRow{Number: v.Version, When: v.At.UTC().Format("2 Jan 2006, 15:04 UTC")}
		if from, restored := rest.RestoredVersion(v.Reason); restored {
			item.RestoredFrom = from
		}
		for _, name := range v.Changed {
			item.Changes = append(item.Changes, resource.VersionChange{
				Label: rest.Humanize(name),
				Edits: valueEdits(held[v.Version-1], held[v.Version], name),
			})
		}
		out.Rows = append(out.Rows, item)
	}
	return out
}

// valueEdits is one field of one version as the comparison it is: the words the
// version before held, marked against the words this one left. Where no earlier
// version stands on this page there is nothing to mark, and the value is shown as one
// unchanged stretch, cut to the panel's line.
func valueEdits(before, after map[string]any, name string) []resource.VersionEdit {
	if before == nil {
		return []resource.VersionEdit{{Text: excerpt(after[name])}}
	}
	return wordEdits(rest.Text(before[name]), rest.Text(after[name]))
}

// excerpt is a stored value as one line of the panel. A version of a body is
// paragraphs long and the panel is a list of versions, so a line is cut; the whole
// document stays behind the versions route, which answers it in full.
func excerpt(value any) string {
	text := rest.Text(value)
	runes := []rune(text)
	const cut = 160
	if len(runes) <= cut {
		return text
	}
	return string(runes[:cut]) + "…"
}

// entryName shares the catalogue's translated noun with generated form headings.
func entryName(r httpx.Resource, o Options) string {
	name := o.Words(r.Schema).Entry(r.Present).Singular
	if name == "" {
		return r.Entity
	}
	return name
}

// subject is what one generated page is about: the entry it lists or shows, in the two
// names its own catalogue gives it, and the collection address a refusal links back to.
// The names are read the way every other name on the screen is read — from the schema's
// hints, through the request's language — so a refusal speaks a word where the page
// speaks a word, and a slug stays in the API.
func subject(r httpx.Resource, o Options, at string, record bool) page.Subject {
	hints := o.Words(r.Schema).Entry(r.Present)
	one, many := hints.Singular, hints.Plural
	if one == "" || many == "" {
		// The entry wrote no reading words, so both names come from the name the
		// declaration gives the entity, read through that name's own catalogue key at
		// each count — the same seam the page's heading and its count read — so a
		// language chooses its plural form here rather than having a letter appended.
		declared := described(r).Name
		if one == "" {
			one = strings.ToLower(o.Noun(r.Schema.Module, r.Schema.Entity, declared, 1))
		}
		if many == "" {
			many = strings.ToLower(o.Noun(r.Schema.Module, r.Schema.Entity, declared, 2))
		}
	}
	return page.Subject{Singular: one, Plural: many, List: at, Record: record}
}
