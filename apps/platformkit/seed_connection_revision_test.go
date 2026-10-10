package main

import (
	"context"
	"net/http"
	"testing"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestTheConnectionRevisionFollowsTheSeededSiteThroughAnUnchangedRun(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)
	start(t, cfg, c.modules, appOptions(cfg, c, app.All))
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	readRevision := func() int64 {
		t.Helper()
		var revision int64
		if err := dbtest.System(t.Context(), conn, func(ctx context.Context, system db.Tx[db.System]) error {
			tenant, err := c.tenants.ByHost(ctx, system, acmeHost)
			if err != nil {
				return err
			}
			return db.InTenant(ctx, system, tenant, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				settings, err := c.sites.Settings(ctx, tx)
				if err != nil {
					return err
				}
				if settings.HomeSlug != "home" {
					t.Errorf("starter site home = %q, want home", settings.HomeSlug)
				}
				revision = settings.Revision
				return nil
			})
		}); err != nil {
			t.Fatal(err)
		}
		code, body := do(t, cfg, nil, http.MethodGet, acmeHost, connectionPath, "")
		if code != http.StatusOK {
			t.Fatalf("connection = %d %s", code, body)
		}
		if got := connectionBody(t, body)["revision"]; got != float64(revision) {
			t.Errorf("connection revision = %v, site owner revision = %d", got, revision)
		}
		return revision
	}
	initial := readRevision()
	if initial != 1 {
		t.Fatalf("starter site revision = %d, want one committed seed write", initial)
	}
	if err := seedCommand([]string{"--config", path, "--tenant", "acme", "--as", adminEmail}); err != nil {
		t.Fatal(err)
	}
	if got := readRevision(); got != initial {
		t.Errorf("unchanged seed moved site revision from %d to %d", initial, got)
	}
	session := signIn(t, cfg, acmeHost, adminEmail, adminPass)
	code, body := do(t, cfg, session, http.MethodPut, acmeHost, sitePath,
		`{"title":"Seeded Workspace","homeSlug":"home","theme":"dark","primaryColor":"#b45309"}`)
	if code != http.StatusOK {
		t.Fatalf("save site = %d %s", code, body)
	}
	if got := readRevision(); got != initial+1 {
		t.Errorf("site revision after manual save = %d, want %d", got, initial+1)
	}
}
