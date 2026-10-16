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

func TestNamingAppsAfterAnUnnamedUpgradeStillPlacesTheirTenants(t *testing.T) {
	ctx := t.Context()
	adminURL, appURL := dbtest.URLs(t)
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
		t.Fatal(err)
	}
	owner := dbtest.Open(t, adminURL)
	apps := map[string]appname.Name{"shop": "collect", "school": "academy"}
	for slug := range apps {
		id := uuid.New()
		if _, err := owner.ExecContext(ctx,
			`INSERT INTO tenants (id, slug, name, app) VALUES ($1, $2, $2, '')`, id, slug); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.ExecContext(ctx,
			`INSERT INTO tenant_hosts (host, tenant_id, is_primary) VALUES ($1, $2, true)`, slug+".example.com", id); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.ExecContext(ctx,
			`INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES ($1, $2, $3)`,
			uuid.New(), appname.Durable("", "ledger", "ledger.invoice_issued"), id); err != nil {
			t.Fatal(err)
		}
	}
	// Installing the release and choosing an app name are separate deployment
	// steps. The unnamed boot must remain supported, without using up the later
	// placement that the same operator's explicit proof asks for.
	cfg := config.Config{Database: config.Database{URL: appURL, MigrateURL: adminURL}}
	if err := app.Migrate(ctx, cfg, nil); err != nil {
		t.Fatalf("upgrade while the deployment still names no app: %v", err)
	}
	if err := app.Drain(ctx, cfg, nil); err != nil {
		t.Fatal(err)
	}
	cfg.NATS.App = "collect"
	cfg.App = config.App{Hosts: []string{"shop.example.com"}, TenantApps: map[string]string{"school": "academy"}}
	if err := app.Migrate(ctx, cfg, nil); err != nil {
		t.Fatalf("name the apps after the unnamed upgrade: %v", err)
	}
	if err := app.Drain(ctx, cfg, nil); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(ctx, appURL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for slug, want := range apps {
		var got string
		if err := owner.QueryRowContext(ctx, `SELECT app FROM tenants WHERE slug = $1`, slug).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want.String() {
			t.Errorf("tenant %s belongs to app %q after the later named boot, want %q", slug, got, want)
		}
		report, err := events.MoveLedger(ctx, conn, want, "boot")
		if err != nil {
			t.Fatal(err)
		}
		if report.Claims != 1 || report.Tenants != 1 {
			t.Errorf("app %s moved %+v after the later placement, want its tenant's existing claim", want, report)
		}
	}
}
