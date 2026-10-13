package app_test

import (
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/migrations"
)

func TestNamingAppsPlacesExistingTenantsBeforeMovingTheirLedgers(t *testing.T) {
	ctx := t.Context()
	adminURL, appURL := dbtest.URLs(t)
	// Freeze the actual pre-drain history, including the already applied app
	// column. A forward placement must work without rewriting those SQL bytes.
	prior := migrations.Source
	files := fstest.MapFS{}
	entries, err := fs.ReadDir(prior.Files, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		version, _, found := strings.Cut(entry.Name(), "_")
		if entry.IsDir() || !found || version > "000043" {
			continue
		}
		body, err := fs.ReadFile(prior.Files, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = &fstest.MapFile{Data: body, Mode: 0444}
	}
	prior.Files = files
	if err := db.Migrate(ctx, adminURL, prior); err != nil {
		t.Fatalf("create the app-less installation: %v", err)
	}
	owner := dbtest.Open(t, adminURL)
	apps := map[string]appname.Name{"shop": "collect", "school": "academy"}
	for tenant := range apps {
		id := uuid.New()
		if _, err := owner.ExecContext(ctx,
			`INSERT INTO tenants (id, slug, name, app) VALUES ($1, $2, $2, '')`, id, tenant); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.ExecContext(ctx,
			`INSERT INTO tenant_hosts (host, tenant_id, is_primary) VALUES ($1, $2, true)`, tenant+".example.com", id); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.ExecContext(ctx,
			`INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES ($1, $2, $3)`,
			uuid.New(), appname.Durable("", "ledger", "ledger.invoice_issued"), id); err != nil {
			t.Fatal(err)
		}
	}
	if err := owner.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		Database: config.Database{URL: appURL, MigrateURL: adminURL},
		NATS:     config.NATS{App: "collect"},
		App:      config.App{Hosts: []string{"shop.example.com"}, TenantApps: map[string]string{"school": "academy"}},
	}
	if err := app.Migrate(ctx, cfg, nil); err != nil {
		t.Fatalf("migrate with proof for every existing tenant: %v", err)
	}
	if err := app.Drain(ctx, cfg, nil); err != nil {
		t.Fatalf("finish the tenant placement: %v", err)
	}
	owner = dbtest.Open(t, adminURL)
	conn, err := db.Open(ctx, appURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for tenant, want := range apps {
		var got string
		if err := owner.QueryRowContext(ctx, `SELECT app FROM tenants WHERE slug = $1`, tenant).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want.String() {
			t.Errorf("tenant %s still belongs to app %q after migration and drain, want %q", tenant, got, want)
		}
		report, err := events.MoveLedger(ctx, conn, want, "boot")
		if err != nil {
			t.Fatal(err)
		}
		if report.Claims != 1 || report.Tenants != 1 {
			t.Errorf("app %s moved %+v after placement, want its tenant's one existing claim", want, report)
		}
	}
}
