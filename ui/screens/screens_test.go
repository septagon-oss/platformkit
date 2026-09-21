package screens_test

import (
	"strings"
	"testing"

	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/httpx"
	core "github.com/septagon-oss/platformkit/ui/resource"
	"github.com/septagon-oss/platformkit/ui/screens"
)

// Note is the entity every test here renders. It is not a module's: what is
// under test is the adapter over ui/resource, so the struct carries every
// widget the generator can make.
type Note struct {
	crud.Base
	Title  string   `json:"title" validate:"required" doc:"What this note is about"`
	Body   string   `json:"body,omitempty" gorm:"type:text" ui:"widget:textarea;hide:list"`
	Status string   `json:"status" enum:"open,done" default:"open"`
	Rank   int      `json:"rank,omitempty"`
	Pinned bool     `json:"pinned"`
	Tags   []string `json:"tags,omitempty" gorm:"-"`
}

func (Note) TableName() string { return "notes" }

// resource is the registered entity as httpx records it: the schema the
// renderers read beside the permissions and closures they never see.
func resource() httpx.Resource {
	return httpx.Resource{
		Module: "note", Entity: "note", Path: "/notes", Screen: "/app/note/notes",
		Read: "note:read", Write: "note:write", Immutable: []string{"status"},
		Schema: crud.Schema{Module: "note", Entity: "note", Path: "/api/v1/note/notes", Fields: crud.Fields[*Note]()},
	}
}

var opts = screens.Options{Workspace: "/app", Home: "Dashboard"}

func render(t *testing.T, nodes []g.Node) string {
	t.Helper()
	var b strings.Builder
	if err := g.Group(nodes).Render(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// The renderer package is imported as core here because the tests already
// name the registered entity resource().

// TestAScreenfulIsTheAPIsDefaultPage: the renderers link page two by their own
// number and Mount asks the API for a page by the storage adapter's default; a
// list would otherwise show a page of one size and link a page of another.
func TestAScreenfulIsTheAPIsDefaultPage(t *testing.T) {
	t.Parallel()
	if core.PerPage != crud.DefaultLimit {
		t.Fatalf("core.PerPage = %d, crud.DefaultLimit = %d", core.PerPage, crud.DefaultLimit)
	}
}

// TestTheAdapterRendersWhatTheRegisteredResourceDescribes: the same rows
// through httpx.Resource and through the schema it carries are one document.
func TestTheAdapterRendersWhatTheRegisteredResourceDescribes(t *testing.T) {
	t.Parallel()
	r := resource()
	rows := []map[string]any{{"id": "1", "title": "Buy milk", "status": "open", "rank": 2.0, "pinned": true}}
	adapted := screens.List(r, opts, rows, 1, 1, "", true)
	pure := core.List(core.Resource{Schema: r.Schema, Immutable: r.Immutable, Screen: r.Screen}, opts, rows, 1, 1, "", true)
	if adapted.Title != pure.Title || render(t, adapted.Body) != render(t, pure.Body) {
		t.Fatal("the adapter and the renderer disagree about the list")
	}
	// The path is no longer the renderer's to work out: the kernel composed it
	// when it registered the resource (httpx.Resource.Screen), and both the
	// screen and the link are built from that one answer.
}
