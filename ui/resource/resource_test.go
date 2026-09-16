package resource_test

import (
	"net/http"
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// Note is the entity every test here renders. It is not a module's: what is
// under test is the generator, so the struct carries every widget it can make.
type Note struct {
	entity.Base
	Title  string   `json:"title" validate:"required" doc:"What this note is about"`
	Body   string   `json:"body,omitempty" gorm:"type:text" ui:"widget:textarea;hide:list"`
	Status string   `json:"status" enum:"open,done" default:"open"`
	Rank   int      `json:"rank,omitempty"`
	Pinned bool     `json:"pinned"`
	Tags   []string `json:"tags,omitempty" gorm:"-"`
}

func (Note) TableName() string { return "notes" }

func note() resource.Resource {
	return resource.Resource{
		Schema:    entity.Schema{Module: "note", Entity: "note", Path: "/api/v1/note/notes", Fields: entity.Fields[*Note]()},
		Immutable: []string{"status"},
	}
}

var opts = resource.Options{Root: "/admin", Home: "Dashboard"}

func render(t *testing.T, nodes []g.Node) string {
	t.Helper()
	var b strings.Builder
	if err := g.Group(nodes).Render(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func TestPathMirrorsTheAPIPath(t *testing.T) {
	t.Parallel()
	if got := resource.Path(note(), opts); got != "/admin/note/notes" {
		t.Fatalf("path = %q", got)
	}
	r := note()
	r.Schema.Path = "/notes"
	if got := resource.Path(r, opts); got != "/admin/note/note" {
		t.Fatalf("a path outside /api/v1 falls back to module/entity, got %q", got)
	}
}

func TestListLeadsWithTheKnownFieldAndHidesWhatTheSchemaHides(t *testing.T) {
	t.Parallel()
	rows := []map[string]any{{"id": "1", "title": "Buy milk", "body": "Two litres", "status": "open", "rank": 2.0, "pinned": true, "tags": []any{"a", "b"}}}
	v := resource.List(note(), opts, rows, 1, 1, "", true)
	if v.Title != "Notes" || v.Status != 0 {
		t.Fatalf("view = %+v", v)
	}
	out := render(t, v.Body)
	for _, want := range []string{"1 note", `href="/admin/note/notes/new"`, `href="/admin/note/notes/1"`, "Buy milk", "Open", "Yes", "a, b"} {
		if !strings.Contains(out, want) {
			t.Fatalf("list lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Two litres") {
		t.Fatal("a hide:list field is on the list")
	}
	if strings.Contains(render(t, resource.List(note(), opts, rows, 1, 1, "", false).Body), `href="/admin/note/notes/new"`) {
		t.Fatal("a caller who may not write is offered New")
	}
}

func TestDetailShowsEveryFieldAndTheWriteAffordances(t *testing.T) {
	t.Parallel()
	row := map[string]any{"id": "1", "title": "Buy milk", "body": "Two litres", "status": "done", "pinned": false}
	v := resource.Detail(note(), opts, row, true)
	if v.Title != "Buy milk" {
		t.Fatalf("the detail's title is %q, want the row's name", v.Title)
	}
	out := render(t, v.Body)
	for _, want := range []string{"Two litres", "Done", "No", `href="/admin/note/notes/1/edit"`, "data-confirm", "Delete", `href="/admin"`, "Dashboard"} {
		if !strings.Contains(out, want) {
			t.Fatalf("detail lacks %q:\n%s", want, out)
		}
	}
	reader := render(t, resource.Detail(note(), opts, row, false).Body)
	if strings.Contains(reader, "/edit") || strings.Contains(reader, "data-confirm") {
		t.Fatal("a caller who may not write is offered Edit or Delete")
	}
}

func TestFormDerivesControlsFromTheSchema(t *testing.T) {
	t.Parallel()
	v := resource.Form(note(), opts, "/admin/note/notes", "New note", nil, nil, "", true)
	if v.Status != 0 {
		t.Fatalf("an unrefused form has status %d", v.Status)
	}
	out := render(t, v.Body)
	for _, want := range []string{`name="title"`, "required", `<textarea`, `name="body"`, `type="number"`, `type="checkbox"`, "Comma separated.", "What this note is about"} {
		if !strings.Contains(out, want) {
			t.Fatalf("create form lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, `name="status"`) {
		t.Fatal("an immutable field is on the create form")
	}
	edit := render(t, resource.Form(note(), opts, "/admin/note/notes/1", "Edit note", map[string]any{"status": "done"}, nil, "", false).Body)
	if !strings.Contains(edit, `<select`) || !strings.Contains(edit, "Changed by a command of its own") {
		t.Fatal("an immutable enum is not a disabled select on edit")
	}
	refused := resource.Form(note(), opts, "/admin/note/notes", "New note",
		map[string]any{"title": ""}, map[string]string{"title": "a note needs a title"}, "a note needs a title", true)
	if refused.Status != http.StatusUnprocessableEntity {
		t.Fatalf("a refused form has status %d, want 422", refused.Status)
	}
	if !strings.Contains(render(t, refused.Body), "a note needs a title") {
		t.Fatal("the refusal is not on the control")
	}
}

func TestControlFollowsTheTagBeforeTheType(t *testing.T) {
	t.Parallel()
	one := func(f entity.Field) string { return render(t, []g.Node{resource.Control(f, "", "", false)}) }
	if !strings.Contains(one(entity.Field{Name: "when", Type: entity.TypeTime}), `type="datetime-local"`) {
		t.Fatal("a time is not a datetime control")
	}
	if !strings.Contains(one(entity.Field{Name: "owner", Type: entity.TypeUUID, Widget: "entity-picker"}), "There is no picker for it yet.") {
		t.Fatal("an entity-picker does not say it is an id")
	}
	if !strings.Contains(one(entity.Field{Name: "ratio", Type: entity.TypeFloat}), `step="any"`) {
		t.Fatal("a float does not accept any step")
	}
}
