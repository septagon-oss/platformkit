package screens

// The panel's door is not the adapter's composition alone: ui/screens draws it only
// where the record's resolved command policy offers that command to the caller the page
// is drawn for, and httpx installs the answer a declaration asks when it mounts a
// resource. A fixture that names a restore verb and no command therefore answers "no
// door". These four cases ask for the mounted copy of their own fixture, drawn for a
// caller who may restore, so the door is the one the record's own command form opens,
// the verb the page offers once is the one at the end of that address, and the write
// count quoted is the one this row carries — a restore quoting none is refused before it
// starts.

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/ui/resource"
)

// panelTenant, panelCaller and panelDirectory are the one caller these page cases
// draw for: a recognised member of one tenant, allowed whatever it asks, holding no
// credential of its own scope. The restore command is declared httpx.SignedIn(), so
// membership is the whole answer and no grant question reaches the directory.
var (
	panelTenant = tenancy.Tenant{ID: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Slug: "acme", Name: "Acme"}
	panelCaller = tenancy.Principal{UserID: uuid.MustParse("22222222-2222-4222-8222-222222222222")}
)

type panelDirectory struct{}

func (panelDirectory) ByHost(context.Context, db.Tx[db.System], string) (tenancy.Tenant, error) {
	return panelTenant, nil
}

func (panelDirectory) Allowed(context.Context, tenancy.Tenant, tenancy.Grant) (bool, error) {
	return true, nil
}

// historyServedTo mounts res on an API that serves no request and answers only the
// authorization its declarations ask, and returns the mounted resource with this
// caller's context. A hand-built httpx.Resource declares its doors and answers none
// of them, so it draws no door; only the registered copy knows who may knock.
func historyServedTo(t *testing.T, res httpx.Resource) (httpx.Resource, context.Context) {
	t.Helper()
	api, _ := httpx.New(httpx.Options{
		Cache: cache.Memory("pkit"), PublicHost: "demo.localhost", Unwired: true,
		Tenants: panelDirectory{}, Authorize: panelDirectory{},
		Log: slog.New(slog.DiscardHandler),
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{}, false, nil
		},
	})
	for _, command := range res.Commands {
		api.AddCommand(res.Module, res.Entity, command)
	}
	api.RegisterResource(res)
	for _, mounted := range api.Resources() {
		if mounted.Entity == res.Entity {
			ctx := tenancy.WithPrincipal(tenancy.WithTenant(context.Background(), panelTenant), panelCaller)
			return mounted, ctx
		}
	}
	t.Fatal("the API mounted none of the resource these cases registered")
	return httpx.Resource{}, context.Background()
}

// panelRestoreCommand is the door a caller who may restore declares: the verb the
// resource names as its restore verb, callable in process by a signed-in member.
func panelRestoreCommand(verb string) httpx.Command {
	return httpx.Command{Verb: verb, Auth: httpx.SignedIn(),
		Run: func(context.Context, uuid.UUID, map[string]any) error { return nil }}
}

func TestThePanelTheAdapterComposesOpensTheDoorItsRecordOwnCommandOpens(t *testing.T) {
	const at = "/app/note/notes"
	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	versioned := httpx.Resource{
		Module: "note", Entity: "note", Path: "/notes", Screen: at,
		Schema: entity.Schema{Module: "note", Entity: "note", Path: "/notes",
			Fields: []entity.Field{{Name: "title", Type: entity.TypeString}}},
		ReadBy:   httpx.SignedIn(),
		Commands: []httpx.Command{panelRestoreCommand("restore")},
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

	mounted, ctx := historyServedTo(t, versioned)
	o := Options{Workspace: "/app", Home: "Workspace"}
	panel := historyPanel(mounted, view(mounted, ctx, at, o), ctx, o, at, row)
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
