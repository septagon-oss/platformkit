package screens_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/page"
	core "github.com/septagon-oss/platformkit/ui/resource"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// TestTheAdapterCarriesEveryFieldTheRenderersRead is the gate that would have
// caught the bug this branch fixes rather than the bug itself.
//
// ui/resource is pure by design (docs/adr/0007): it reads plain values and never
// touches a request, so `described` is the only door through which a registered
// resource reaches a screen. `httpx.Resource.Singleton` was delivered to the native
// shell in catalog.go from the day it existed and never to the web screens, and
// nothing failed — not the compiler, because the literal simply omitted the field,
// not a test, because the screens rendered *something* plausible. The settings page
// that listed one row, offered New against a route that answers 405 and Delete
// against one that answers 409 is what that silence looks like.
//
// So: every exported field of the value the renderers read is enumerated here and
// must change what the adapter renders. A field added to core.Resource without a
// case in shapesFor fails, and a field the adapter stops delivering fails, and
// neither depends on someone remembering to check.
func TestTheAdapterCarriesEveryFieldTheRenderersRead(t *testing.T) {
	t.Parallel()
	for _, f := range reflect.VisibleFields(reflect.TypeOf(core.Resource{})) {
		if !f.IsExported() {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			a, b, render, ok := shapesFor(f.Name)
			if !ok {
				t.Fatalf("no case for field %q. Either the renderers do not read it, or %s", f.Name,
					"the adapter may never deliver it: add the field's two shapes to shapesFor and the screen that would notice")
			}
			if render(a) == render(b) {
				t.Errorf("field %q changed nothing on any screen, so the renderers never learn it: the adapter is not carrying it", f.Name)
			}
		})
	}
}

// shapesFor is one field in both its shapes, beside the screen that has to notice
// the difference. Each render goes through screens, never core, so what is under
// test is the delivery and not the renderer's own branch.
func shapesFor(name string) (a, b httpx.Resource, render func(httpx.Resource) string, ok bool) {
	base := func() httpx.Resource {
		return httpx.Resource{
			Module: "note", Entity: "note", Path: "/api/v1/note/notes",
			Read: "note:read", Write: "note:write",
			Schema: entity.Schema{Module: "note", Entity: "note", Path: "/api/v1/note/notes", Fields: noteFields()},
		}
	}
	row := map[string]any{"id": "11111111-1111-1111-1111-111111111111", "title": "Buy milk", "status": "open"}
	rows := []map[string]any{row}
	switch name {
	case "Schema":
		a, b := base(), base()
		b.Schema.Fields = noteFields(entity.Field{Name: "urgent", Type: entity.TypeBool})
		return a, b, func(r httpx.Resource) string {
			return renderView(screens.List(r, opts, rows, 1, 1, "", false))
		}, true
	case "Immutable":
		a, b := base(), base()
		b.Immutable = []string{"status"}
		return a, b, func(r httpx.Resource) string {
			return renderView(screens.Form(r, opts, "/admin/note/notes", "Edit", row, nil, "", false))
		}, true
	case "Singleton":
		a, b := base(), base()
		b.Singleton = true
		return a, b, func(r httpx.Resource) string {
			return renderView(screens.Detail(r, opts, row, true))
		}, true
	}
	return httpx.Resource{}, httpx.Resource{}, nil, false
}

// noteFields is the schema the cases share. It is built rather than derived from a
// struct so a change in an unrelated test entity cannot quietly add or remove a
// column here and make a comparison pass for the wrong reason.
func noteFields(extra ...entity.Field) []entity.Field {
	fields := []entity.Field{
		{Name: "id", Type: entity.TypeString, ReadOnly: true},
		{Name: "title", Type: entity.TypeString},
		{Name: "status", Type: entity.TypeString},
	}
	return append(fields, extra...)
}

// The comparison is of rendered markup, because a difference in a struct nobody
// renders is not something a person can see.
func renderView(v page.View) string {
	var b strings.Builder
	for _, n := range v.Body {
		if err := n.Render(&b); err != nil {
			return "render error"
		}
	}
	return v.Title + b.String()
}
