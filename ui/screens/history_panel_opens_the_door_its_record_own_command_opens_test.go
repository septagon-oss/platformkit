package screens

// The panel's three addresses are the adapter's composition, not the renderer's:
// ui/screens reads the mounted verb, the record's address and the row's write count,
// and ui/resource only draws what arrives. A case that hands the renderer an Action,
// a Carries and a Condition of its own therefore says nothing about whether the
// adapter composes them the way the renderer's rules need. This is that seam, read
// where the two layers meet: the panel must post to the address this record's own
// command form would post to — Command.Action is that rule — must carry the verb at
// the end of that address, which is what lets the page offer the command once, and
// must quote the row's write count, because a restore quoting none is refused before
// it starts.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/ui/resource"
)

func TestThePanelTheAdapterComposesOpensTheDoorItsRecordOwnCommandOpens(t *testing.T) {
	const at = "/app/note/notes"
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	versioned := httpx.Resource{
		Module: "note", Entity: "note", Path: "/notes", Screen: at,
		Schema: entity.Schema{Module: "note", Entity: "note", Path: "/notes",
			Fields: []entity.Field{{Name: "title", Type: entity.TypeString}}},
		History: &httpx.History{RestoreVerb: "restore", Asks: "This puts that version back.",
			Versions: func(context.Context, uuid.UUID) ([]httpx.Version, error) {
				return []httpx.Version{
					{Version: 9, At: time.Unix(1700000000, 0).UTC(),
						Fields: json.RawMessage(`{"title":"Later"}`), Changed: []string{"title"}},
					{Version: 3, At: time.Unix(1600000000, 0).UTC(),
						Fields: json.RawMessage(`{"title":"Earlier"}`), Changed: []string{"title"}},
				}, nil
			}},
	}
	row := map[string]any{"id": id.String(), entity.RevisionField: float64(14)}

	panel := historyPanel(versioned, context.Background(),
		resource.Options{Workspace: "/app", Home: "Workspace"}, at, row)
	if panel == nil {
		t.Fatal("a resource whose History answers with a series composed no panel beside the record")
	}
	if len(panel.Rows) != 2 {
		t.Fatalf("the panel holds %d versions, want the two this record's series answered", len(panel.Rows))
	}
	own := resource.Command{Verb: versioned.History.RestoreVerb, Base: at}.Action(at + "/" + id.String())
	if panel.Action != own {
		t.Errorf("the panel posts to %q, want the address this record's own restore command posts to, %q",
			panel.Action, own)
	}
	if verb := panel.Action[strings.LastIndex(panel.Action, "/")+1:]; verb != panel.Carries {
		t.Errorf("the panel carries the verb %q but posts to a door named %q: the page offers its command twice, because no subtraction matches the two",
			panel.Carries, verb)
	}
	if panel.Condition != "14" {
		t.Errorf("the panel quotes the write count %q, want %q, the count this row carries: a restore naming none is refused before it starts",
			panel.Condition, "14")
	}
}
