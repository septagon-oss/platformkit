package app

import (
	"context"
	"encoding/json"
	"runtime"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
)

type appInvoiceIssued struct {
	Total int64 `json:"total"`
}

// Both compositions remain live in one process. A second app's manifest must
// not remove the first app's payload contract from the outbox's write door.
func TestTwoAppCompositionsKeepTheirEventPayloadContracts(t *testing.T) {
	cfg, opts := compose(t)
	if err := db.Migrate(t.Context(), cfg.Database.MigrateURL, migrations.Source); err != nil {
		t.Fatalf("migrate the shared database: %v", err)
	}
	admin := dbtest.Open(t, cfg.Database.MigrateURL)
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open the shared database: %v", err)
	}
	defer conn.Close()
	t.Cleanup(func() { events.DeclareAll(nil) })

	tenantID := uuid.New()
	if _, err := admin.ExecContext(t.Context(),
		`INSERT INTO tenants (id, slug, name, app) VALUES ($1, 'invoice-customer', 'Invoice customer', 'acme')`, tenantID); err != nil {
		t.Fatalf("place the first app's tenant: %v", err)
	}
	billing := module.Module{
		Name: "billing",
		Declared: []events.Declared{
			events.Declare[appInvoiceIssued]("billing.invoice_issued"),
		},
	}
	cfg.NATS.App = "acme"
	opts.App = appname.MustParse("acme")
	a, err := New(t.Context(), cfg, []module.Module{billing, brand("cataloguea", "")}, opts)
	if err != nil {
		t.Fatalf("compose app acme: %v", err)
	}

	write := func(payload any) error {
		t.Helper()
		ctx := tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: tenantID, Slug: "invoice-customer"})
		return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return events.Publish(ctx, tx, "billing.invoice_issued", payload)
		})
	}
	invalid := map[string]any{"total": "not minor units"}
	if err := write(invalid); err == nil {
		t.Fatal("app acme's declared payload did not refuse a string total before another app was composed")
	}
	countRows := func() int {
		t.Helper()
		var rows int
		if err := admin.QueryRowContext(t.Context(),
			`SELECT count(*) FROM platformkit_outbox WHERE tenant_id = $1 AND name = 'billing.invoice_issued'`, tenantID).Scan(&rows); err != nil {
			t.Fatalf("count app acme's outbox rows: %v", err)
		}
		return rows
	}
	if rows := countRows(); rows != 0 {
		t.Fatalf("app acme's initial refusal left %d outbox rows, want none", rows)
	}

	cfg.NATS.App = "academy"
	opts.App = appname.MustParse("academy")
	b, err := New(t.Context(), cfg, []module.Module{brand("catalogueb", "")}, opts)
	if err != nil {
		t.Fatalf("compose app academy over the same database: %v", err)
	}
	if err := write(appInvoiceIssued{Total: 42}); err != nil {
		t.Fatalf("app acme's valid payload stopped publishing after academy was composed: %v", err)
	}
	if rows := countRows(); rows != 1 {
		t.Fatalf("app acme's valid write left %d outbox rows, want one", rows)
	}
	if err := write(invalid); err == nil {
		t.Error("app academy's composition removed app acme's payload check: malformed billing.invoice_issued was committed")
	}
	if rows := countRows(); rows != 1 {
		t.Errorf("app acme has %d outbox rows after one valid and two refused writes, want only the valid row", rows)
	}
	published := make(chan events.Event, 4)
	transport := memory.New()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	if err := transport.Subscribe(ctx, "catalogue-observer", "billing.invoice_issued", events.Sink{
		Handle: func(_ context.Context, ev events.Event) error {
			published <- ev
			return nil
		},
	}); err != nil {
		t.Fatalf("observe app acme's publications: %v", err)
	}
	if err := events.RelayApp(t.Context(), conn, transport, appname.MustParse("acme")); err != nil {
		t.Fatalf("relay app acme's outbox: %v", err)
	}
	if got := len(published); got != 1 {
		t.Errorf("app acme's relay emitted %d events, want only its one valid event", got)
	}
	for len(published) > 0 {
		ev := <-published
		var body appInvoiceIssued
		if err := json.Unmarshal(ev.Payload, &body); err != nil || body.Total != 42 {
			t.Errorf("app acme's relay emitted payload %s from event %s, want the one valid payload", ev.Payload, ev.ID)
		}
	}
	runtime.KeepAlive(a)
	runtime.KeepAlive(b)
}
