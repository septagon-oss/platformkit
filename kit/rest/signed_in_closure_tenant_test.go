package rest_test

import (
	"context"
	"log/slog"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// A signed-in resource's in-process read uses the same tenant transaction as its route.
func TestASignedInResourceClosureCannotReadAnotherTenantsRow(t *testing.T) {
	const acmeHost, globexHost = "acme.test", "globex.test"
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	admin, app := dbtest.Schema(t)
	if _, err := admin.ExecContext(t.Context(), ddl); err != nil {
		t.Fatal(err)
	}
	acmeRow, globexRow := uuid.New(), uuid.New()
	for _, row := range []struct {
		id, tenant uuid.UUID
		title      string
	}{
		{acmeRow, acme.ID, "acme row"},
		{globexRow, globex.ID, "globex row"},
	} {
		if _, err := admin.ExecContext(t.Context(),
			"INSERT INTO rest_tasks (id, tenant_id, title) VALUES ($1, $2, $3)",
			row.id, row.tenant, row.title); err != nil {
			t.Fatal(err)
		}
	}

	s := spec
	s.Operations = []httpx.CRUD{rest.List, rest.Read}
	s.Read = ""
	s.ReadAuth = httpx.SignedIn()
	api, router := httpx.New(httpx.Options{
		PublicHost: acmeHost, Installation: acmeHost,
		Tenants: hosts{byHost: map[string]tenancy.Tenant{acmeHost: acme, globexHost: globex}},
		Conn:    app, Authorize: member{}, Entitle: everyPlan{},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: principal}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	s.Mount(api.Surfaces(s.Module))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatal(err)
	}
	res := api.Resources()[0]
	var ownTitle string
	var foreignErr error
	var listTotal int64
	httpx.Register(api.Surfaces(s.Module).App, probeOperation("signed-in-closure-tenant", "/signed-in-closure-tenant"),
		httpx.SignedIn(), func(ctx context.Context, _ *struct{}) (*struct{}, error) {
			own, err := res.Get(ctx, acmeRow)
			if err != nil {
				t.Errorf("own row: %v", err)
			} else {
				ownTitle, _ = own["title"].(string)
			}
			_, foreignErr = res.Get(ctx, globexRow)
			_, listTotal, err = res.List(ctx, crud.Query{Limit: 10})
			if err != nil {
				t.Errorf("own list: %v", err)
			}
			return nil, nil
		})
	if status, body := callHost(t, router, http.MethodPost, acmeHost,
		api.Surfaces(s.Module).App.Path("/signed-in-closure-tenant"), ""); status != http.StatusNoContent {
		t.Fatalf("closure request = %d %s, want %d", status, body, http.StatusNoContent)
	}
	if ownTitle != "acme row" || listTotal != 1 {
		t.Errorf("own row = %q, list total = %d; want acme's one row", ownTitle, listTotal)
	}
	if !refused(foreignErr, http.StatusNotFound) {
		t.Errorf("foreign row = %v, want 404 from tenant RLS", foreignErr)
	}
}
