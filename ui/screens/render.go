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
	"fmt"

	g "maragu.dev/gomponents"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
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
	return resource.Resource{Schema: r.Schema, Immutable: r.Immutable, Singleton: r.Singleton,
		Screen: r.Screen, Operations: r.OperationWords(), Present: r.Present}
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
			Verb: c.Verb, Label: label, Description: c.Description,
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
	return resource.DetailRichText(view(r, ctx, at), o, row, writable, rendered, locales), nil
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
	id, err := uuid.Parse(rest.Text(row["id"]))
	if err != nil {
		return nil, fmt.Errorf("screens: the %s row's id is %q, which is no id: %w", r.Entity, rest.Text(row["id"]), err)
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
