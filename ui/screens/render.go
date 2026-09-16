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
package screens

import (
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

// described is the half of a registered resource the renderers read: its
// schema and the fields a command owns. The closures stay with the caller.
func described(r httpx.Resource) resource.Resource {
	return resource.Resource{Schema: r.Schema, Immutable: r.Immutable}
}

// Path is where a resource's list screen lives: /api/v1/task/tasks is served at
// Root/task/tasks, which is what every module's nav entry already says.
func Path(r httpx.Resource, o Options) string { return resource.Path(described(r), o) }

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
