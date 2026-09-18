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

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
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
	return resource.Resource{Schema: r.Schema, Immutable: r.Immutable, Singleton: r.Singleton}
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
		out = append(out, resource.Command{
			Verb: c.Verb, Label: c.Summary, Description: c.Description,
			Fields: c.Fields, Collection: c.Collection, Base: at,
		})
	}
	return out
}

// Path is where a resource's list screen lives: /api/v1/task/tasks is served at
// Root/task/tasks, which is what every module's nav entry already says.
func Path(r httpx.Resource, o Options) string { return resource.Path(described(r), o) }

// listView, detailView and formView are the same renderers reached with a caller in
// hand. Mount uses them; the exported trio below is what a caller without a request
// renders — a design export, a golden file, a test — and so carries no commands,
// because "may this person resolve the task" has no answer where there is no person.
func listView(r httpx.Resource, ctx context.Context, o Options, at string, rows []map[string]any, total int64, pageNo int, sort string, writable bool) page.View {
	return resource.List(view(r, ctx, at), o, rows, total, pageNo, sort, writable)
}

func detailView(r httpx.Resource, ctx context.Context, o Options, at string, row map[string]any, writable bool) page.View {
	return resource.Detail(view(r, ctx, at), o, row, writable)
}

// List is the list screen of a registered resource. See resource.List.
func List(r httpx.Resource, o Options, rows []map[string]any, total int64, pageNo int, sort string, writable bool) page.View {
	return resource.List(described(r), o, rows, total, pageNo, sort, writable)
}

// Detail is one row of a registered resource. See resource.Detail.
func Detail(r httpx.Resource, o Options, row map[string]any, writable bool) page.View {
	return resource.Detail(described(r), o, row, writable)
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
