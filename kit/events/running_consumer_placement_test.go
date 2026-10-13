package events_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/migrations"
)

func TestRunningScopedConsumerDoesNotRepeatWorkAfterTenantPlacement(t *testing.T) {
	for _, placementFirst := range []bool{true, false} {
		name := "placement_before_consumer_boot"
		if !placementFirst {
			name = "placement_while_another_replica_consumes"
		}
		t.Run(name, func(t *testing.T) {
			adminURL, appURL := dbtest.URLs(t)
			if err := db.Migrate(t.Context(), adminURL, migrations.Source); err != nil {
				t.Fatal(err)
			}
			owner := dbtest.Open(t, adminURL)
			conn, err := db.Open(t.Context(), appURL)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			tenant := tenancy.Tenant{ID: uuid.New(), Slug: "late"}
			placeTenant(t, conn, tenant.ID, tenant.Slug, "")
			name := "ledger_" + tenant.ID.String()[:8] + ".invoice_issued"
			effect := "ledger_" + tenant.ID.String()[:8] + ".effect_committed"
			publish(t, conn, tenant, name, nil)
			id := eventID(t, conn, tenant.ID, name)
			var calls atomic.Int64
			handle := func(ctx context.Context, tx db.Tx[db.Tenant], _ events.Event) error {
				calls.Add(1)
				return events.Publish(ctx, tx, effect, nil)
			}
			old := memory.New()
			if err := events.Consume(ctx, conn, old, []events.Subscription{{Module: ledgerModule, Name: name, Handler: handle}}); err != nil {
				t.Fatal(err)
			}
			// Deliver the committed outbox event before its relay stamps publication.
			// A relay crash at this point leaves the claim and effect committed, and
			// the outbox row pending for the next relay. No replay clears the claim.
			if err := old.Publish(ctx, events.Event{ID: id, TenantID: tenant.ID, Name: name}); err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 1 {
				t.Fatal("initial delivery did not commit its handler")
			}
			place := func() {
				t.Helper()
				cfg := config.Config{
					Database: config.Database{URL: appURL, MigrateURL: adminURL},
					NATS:     config.NATS{App: "collect"},
					App:      config.App{TenantApps: map[string]string{"late": "collect"}},
				}
				if err := app.Migrate(ctx, cfg, nil); err != nil {
					t.Fatal(err)
				}
			}
			if placementFirst {
				place()
			}
			// Replica A completes its required move and opens scoped consumers.
			if _, err := events.MoveLedger(ctx, conn, "collect", "boot"); err != nil {
				t.Fatal(err)
			}
			scoped := memory.New()
			if err := events.Consume(ctx, conn, scoped, []events.Subscription{{App: "collect", Module: ledgerModule, Name: name, Handler: handle}}); err != nil {
				t.Fatal(err)
			}
			if !placementFirst {
				// Replica B's declaring boot places this tenant. Before B can move
				// its ledger, A's already-running relay can read the newly owned row.
				place()
			}
			if err := events.RelayApp(ctx, conn, scoped, "collect"); err != nil {
				t.Fatal(err)
			}
			if got := calls.Load(); got != 1 {
				t.Errorf("event handled %d times after tenant placement; want once", got)
			}
			var effects int
			if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_outbox WHERE tenant_id=$1 AND name=$2`, tenant.ID, effect).Scan(&effects); err != nil {
				t.Fatal(err)
			}
			if effects != 1 {
				t.Errorf("event committed %d effects after tenant placement; want one", effects)
			}
		})
	}
}
