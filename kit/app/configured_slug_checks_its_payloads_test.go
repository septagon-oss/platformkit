package app

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
)

type slugOrderPlaced struct {
	Total int64 `json:"total"`
}

// A deployment names its app with nats.app, the slug's one configuration key:
// the migration places its tenants under that slug and the transport addresses
// its events with it. The reference application composes with exactly that
// configuration and no Options.App. Such a composition either refuses to boot
// with two answers to "which app am I", or it checks its own tenants' payloads
// against its own declarations; it never commits a malformed declared event.
func TestAConfiguredSlugChecksItsOwnTenantsPayloads(t *testing.T) {
	cfg, opts := compose(t)
	if err := db.Migrate(t.Context(), cfg.Database.MigrateURL, migrations.Source); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	t.Cleanup(func() { events.DeclareAll(nil) })

	tenantID := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		`INSERT INTO tenants (id, slug, name, app) VALUES ($1, 'slug-customer', 'Slug customer', 'collect')`, tenantID); err != nil {
		t.Fatalf("place the tenant under the configured slug: %v", err)
	}
	orders := module.Module{
		Name:     "orders",
		Declared: []events.Declared{events.Declare[slugOrderPlaced]("orders.order_placed")},
	}
	cfg.NATS.App = "collect"
	if _, err := New(t.Context(), cfg, []module.Module{orders, brand("slugcheck", "")}, opts); err != nil {
		if !strings.Contains(err.Error(), "collect") {
			t.Fatalf("compose app collect: %v", err)
		}
		t.Logf("the composition refused two answers to its own app: %v", err)
		return
	}

	ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: tenantID, Slug: "slug-customer"})
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, "orders.order_placed", map[string]any{"total": "not minor units"})
	})
	if err == nil {
		t.Error("app collect's declared payload did not refuse a string total for its own tenant")
	}
	var rows int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM platformkit_outbox WHERE tenant_id = $1 AND name = 'orders.order_placed'`, tenantID).Scan(&rows); err != nil {
		t.Fatalf("count outbox rows: %v", err)
	}
	if rows != 0 {
		t.Errorf("app collect has %d outbox rows after a refused write, want none", rows)
	}
}
