package admin_test

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	g "maragu.dev/gomponents"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/ui/page"
)

// TestAModulesOwnWorkspacePageStandsInsteadOfTheGeneratedRegister is the collision the pets client
// could not boot through (T-0126): a module that writes its own working page for a resource, at the
// address the kernel composes for that resource's screens, and the shell composed after it
// generating a register at the same addresses. The surface gate refuses two routes at one method and
// path, so the application did not start. The module's page is what its people are sent to; the
// shell now leaves that resource's screens to it, and every other resource keeps its register.
func TestAModulesOwnWorkspacePageStandsInsteadOfTheGeneratedRegister(t *testing.T) {
	adminDB, app := dbtest.Schema(t)
	if _, err := adminDB.ExecContext(t.Context(), ddl+plansDDL); err != nil {
		t.Fatalf("create the tables: %v", err)
	}
	api, router := httpx.New(httpx.Options{
		Cache:      cache.Memory("pkit"),
		PublicHost: host, Tenants: caller{}, Conn: app, Authorize: caller{},
		Installation: operatorHost,
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New(), Roles: []string{"admin"}}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	notes := module.Module{
		Name:        "note",
		Permissions: []module.Permission{{Key: "note:read", Label: "read notes"}, {Key: "note:write", Label: "write notes"}},
		Declared:    spec.Declared(),
		Routes: func(s httpx.Surfaces) {
			spec.Mount(s)
			// The module's own desk for one note, at the address the generated register would take.
			page.Serve(s.App, page.Shell{Frame: func(_ context.Context, _ page.Request, body []g.Node) g.Node { return g.Group(body) }}, page.Route{ID: "note-desk", Method: http.MethodGet, Path: "/notes/{id}", Summary: "The note desk"},
				httpx.Permission("note:read"), func(context.Context, page.Request, *struct {
					ID string `path:"id"`
				}) (page.View, error) {
					return page.View{Title: "The note desk"}, nil
				})
		},
	}
	catalogue := module.Module{
		Name:        "plan",
		Permissions: []module.Permission{{Key: "plan:read", Label: "read plans"}, {Key: "plan:write", Operator: true, Label: "write plans"}},
		Declared:    plans.Declared(),
		Routes:      func(s httpx.Surfaces) { plans.Mount(s) },
	}
	shell := admin.New(admin.Deps{Modules: []module.Module{notes, catalogue}, Authorize: caller{}, SignIn: "/api/v1/auth/login"})
	var declared []tenancy.Grant
	for _, m := range []module.Module{notes, catalogue, shell} {
		for _, p := range m.Permissions {
			declared = append(declared, tenancy.Grant{Permission: p.Key, Operator: p.Operator})
		}
	}
	api.Declare(declared)
	for _, m := range []module.Module{notes, catalogue, shell} {
		m.Routes(api.Surfaces(m.Name))
	}
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the application does not start: %v", err)
	}

	generated := map[string]bool{}
	for _, op := range api.Recorded() {
		if op.Method == http.MethodGet && strings.HasPrefix(op.OperationID, "screen-") {
			generated[op.Path] = true
		}
	}
	for path := range generated {
		if strings.HasPrefix(path, "/app/note/notes") {
			t.Errorf("the shell generated %s for a resource whose module serves its own workspace pages", path)
		}
	}
	if !generated["/app/plan/plans"] {
		t.Errorf("a resource with no pages of its own lost its generated register: %v", generated)
	}
	if code, _, _ := call(t, router, http.MethodGet, "/app/note/notes/"+uuid.NewString(), ""); code != http.StatusOK {
		t.Errorf("the module's own desk answered %d, want 200", code)
	}
}
