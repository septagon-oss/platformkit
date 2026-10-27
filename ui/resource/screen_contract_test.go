package resource_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/screen"
	"github.com/septagon-oss/platformkit/ui/document"
	"github.com/septagon-oss/platformkit/ui/resource"
	"golang.org/x/net/html"
	g "maragu.dev/gomponents"
)

func screenResource() resource.Resource {
	return resource.Resource{ScreenOwner: "note", Screen: "/app/note/notes", Immutable: []string{"status"},
		Schema: entity.Schema{Module: "note", Entity: "note", Fields: []entity.Field{
			{Name: "id", Type: entity.TypeString, ReadOnly: true},
			{Name: "title", Type: entity.TypeString, Required: true},
			{Name: "status", Type: entity.TypeString},
			{Name: "transport", Type: entity.TypeString, Widget: "hidden"},
		}},
	}
}

func anchorNames(t *testing.T, view document.View) []string {
	t.Helper()
	var out strings.Builder
	if err := g.Group(view.Body).Render(&out); err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(strings.NewReader(out.String()))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	var visit func(*html.Node)
	visit = func(node *html.Node) {
		for _, attr := range node.Attr {
			if attr.Key == "data-screen-anchor" {
				names = append(names, attr.Val)
			}
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(doc)
	slices.Sort(names)
	return names
}

func TestRenderedViewsCarryExactlyTheirUniqueControls(t *testing.T) {
	r := screenResource()
	row := map[string]any{"id": "record", "title": "Title", "status": "open"}
	o := resource.Options{}
	cases := []struct {
		kind  string
		view  document.View
		names []string
	}{
		{"list", resource.List(r, o, []map[string]any{row, {"id": "second", "title": "Other", "status": "open"}}, 2, 1, "", true), []string{"action.create"}},
		{"detail", resource.Detail(r, o, row, true), []string{"action.delete", "action.edit"}},
		{"create", resource.Form(r, o, "/new", "Create", nil, nil, "", true), []string{"action.cancel", "action.submit", "field.title"}},
		{"edit", resource.Form(r, o, "/edit", "Edit", row, map[string]string{"title": "Required"}, "Refused", false), []string{"action.cancel", "action.submit", "field.status", "field.title"}},
	}
	for _, tc := range cases {
		t.Run(tc.kind, func(t *testing.T) {
			want := make([]string, len(tc.names))
			for i, name := range tc.names {
				want[i] = "app/note/resource/note/" + tc.kind + "#" + name
			}
			if got := anchorNames(t, tc.view); !slices.Equal(got, want) {
				t.Fatalf("anchors = %v, want %v", got, want)
			}
		})
	}
	for _, view := range []document.View{resource.List(r, o, nil, 0, 1, "", false), resource.Detail(r, o, row, false)} {
		if got := anchorNames(t, view); len(got) != 0 {
			t.Fatalf("read-only anchors = %v", got)
		}
	}
}

func TestPublishedControlsFollowMountedOperationsAndImmutableFields(t *testing.T) {
	r := screenResource()
	views := resource.ScreenViews(r)
	if len(views) != 4 {
		t.Fatalf("views = %v", views)
	}
	var edit screen.View
	for _, view := range views {
		if view.Ref().Kind() == "edit" {
			edit = view
		}
	}
	status, err := edit.Anchor("app/note/resource/note/edit#field.status")
	if err != nil || status.Supports(screen.Committed) || status.Binding().Target != "resource/note/note/field/status" {
		t.Fatalf("immutable status = %v, %v", status, err)
	}
	r.Schema.Fields = slices.DeleteFunc(r.Schema.Fields, func(f entity.Field) bool { return f.Name == "title" })
	for _, view := range resource.ScreenViews(r) {
		if _, err := view.Anchor(screen.AnchorRef(string(view.Ref()) + "#field.title")); err == nil {
			t.Fatal("removed field retained its anchor")
		}
	}
	r.Operations = []string{"list", "read"}
	for _, view := range resource.ScreenViews(r) {
		if (view.Ref().Kind() != "list" && view.Ref().Kind() != "detail") || len(view.Anchors()) != 0 {
			t.Fatalf("read-only view = %v", view)
		}
	}
	r.Singleton, r.Operations = true, []string{"read", "update"}
	views = resource.ScreenViews(r)
	if len(views) != 2 || views[0].Ref().Kind() != "detail" || views[1].Ref().Kind() != "edit" {
		t.Fatalf("singleton = %v", views)
	}
}

func TestCommandsScopeFieldsAndRefuseRepeatedTargets(t *testing.T) {
	r := screenResource()
	r.Commands = []resource.Command{
		{ScreenOwner: "note", Entity: "note", Verb: "accept", Fields: []entity.Field{{Name: "reason", Type: entity.TypeString}}},
		{ScreenOwner: "note", Entity: "note", Verb: "decline", Fields: []entity.Field{{Name: "reason", Type: entity.TypeString}}},
		{ScreenOwner: "note", Entity: "note", Verb: "restore", Repeated: true},
	}
	for _, view := range resource.ScreenViews(r) {
		if view.Ref().Kind() != "command" {
			continue
		}
		if strings.HasSuffix(string(view.Ref()), "/restore") {
			if view.Cardinality() != screen.Many || len(view.Anchors()) != 0 {
				t.Fatalf("repeated targets = %v", view)
			}
			continue
		}
		anchor, err := view.Anchor(screen.AnchorRef(string(view.Ref()) + "#field.reason"))
		if err != nil || !anchor.Supports(screen.Committed) {
			t.Fatalf("command field = %v, %v", anchor, err)
		}
		verb := string(view.Ref())[strings.LastIndex(string(view.Ref()), "/")+1:]
		if anchor.Binding().Target != "screen-note-note-"+verb {
			t.Fatalf("command target = %s", anchor.Binding().Target)
		}
	}
}
