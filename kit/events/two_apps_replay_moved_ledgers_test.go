package events_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	natsio "github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	provider "github.com/septagon-oss/platformkit/kit/events/providers/nats"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestTwoAppsReplayTheirMovedLedgersWithoutHandlingTwice(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_NATS_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_NATS_URL is unset")
	}
	_, conn := dbtest.Schema(t)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	nc, err := natsio.Connect(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(nc.Close)
	js, err := nc.JetStream()
	if err != nil {
		t.Fatal(err)
	}
	name := "ledger_" + strings.ReplaceAll(uuid.NewString(), "-", "") + ".invoice_issued"
	apps := []appname.Name{"collect", "academy"}
	tenants := []tenancy.Tenant{
		{ID: uuid.New(), Slug: "shop"},
		{ID: uuid.New(), Slug: "school"},
	}
	// Cleanup is restricted to this test's unique event name and consumers.
	t.Cleanup(func() {
		for _, app := range append([]appname.Name{""}, apps...) {
			_ = js.DeleteConsumer("PLATFORMKIT", appname.Durable(app, "ledger", name))
			for _, tenant := range tenants {
				_ = js.PurgeStream("PLATFORMKIT", &natsio.StreamPurgeRequest{Subject: appname.Subject(app, tenant.ID, name)})
			}
		}
	})
	connect := func(app appname.Name) events.Transport {
		t.Helper()
		tr, err := provider.Connect(config.NATS{URL: url, App: app.String()})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = tr.(interface{ Close() error }).Close() })
		return tr
	}
	// Read acknowledgements, not elapsed silence: each replay must reach the
	// real Consume sink and finish before a zero-handler-call assertion means anything.
	waitForAck := func(app appname.Name, deliveries uint64) {
		t.Helper()
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			info, err := js.ConsumerInfo("PLATFORMKIT", appname.Durable(app, "ledger", name))
			if err != nil {
				t.Fatal(err)
			}
			if info.Config.DeliverPolicy != natsio.DeliverAllPolicy {
				t.Fatal("consumer does not replay retained deliveries")
			}
			if info.AckFloor.Consumer >= deliveries && info.NumPending == 0 && info.NumAckPending == 0 {
				return
			}
			select {
			case <-ticker.C:
			case <-ctx.Done():
				t.Fatalf("app %q did not finish replay: %+v", app, info)
			}
		}
	}
	calls := map[uuid.UUID]*counter{}
	for _, tenant := range tenants {
		placeTenant(t, conn, tenant.ID, tenant.Slug, "")
		calls[tenant.ID] = new(counter)
	}
	old := connect("")
	if err := events.Consume(ctx, conn, old, []events.Subscription{{
		Module: "ledger", Name: name,
		Handler: func(_ context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			if calls[ev.TenantID] == nil || db.TenantOf(tx).ID != ev.TenantID {
				return fmt.Errorf("unexpected tenant %s", ev.TenantID)
			}
			return calls[ev.TenantID].run()
		},
	}}); err != nil {
		t.Fatal(err)
	}
	for _, tenant := range tenants {
		publish(t, conn, tenant, name, nil)
	}
	if err := events.Relay(ctx, conn, old); err != nil {
		t.Fatal(err)
	}
	waitForAck("", 2)
	if err := old.(interface{ Close() error }).Close(); err != nil {
		t.Fatal(err)
	}
	for i, app := range apps {
		tenant := tenants[i]
		if got := calls[tenant.ID].count(); got != 1 {
			t.Fatalf("old consumer handled %s %d times, want once", tenant.Slug, got)
		}
		// Placement is an explicit prerequisite here; the migration integration
		// test separately checks that the deployment can actually establish it.
		if err := dbtest.System(ctx, conn, func(_ context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Exec(`UPDATE tenants SET app = ? WHERE id = ?`, app.String(), tenant.ID).Error
		}); err != nil {
			t.Fatal(err)
		}
		if report, err := events.MoveLedger(ctx, conn, app, "boot"); err != nil || report.Claims != 1 {
			t.Fatalf("move %s: %+v, %v", app, report, err)
		}
	}
	brokers := []events.Transport{connect(apps[0]), connect(apps[1])}
	for i, app := range apps {
		tenant, broker := tenants[i], brokers[i]
		id := eventID(t, conn, tenant.ID, name)
		unstamp(t, conn, id)
		if err := events.RelayApp(ctx, conn, broker, app); err != nil {
			t.Fatal(err)
		}
		// The scoped event is already retained when its new durable is made.
		if err := events.Consume(ctx, conn, broker, []events.Subscription{{
			App: app, Module: "ledger", Name: name,
			Handler: func(_ context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
				if ev.App != app || ev.TenantID != tenant.ID || db.TenantOf(tx).ID != tenant.ID {
					t.Errorf("app %s handled another app's delivery: %+v", app, ev)
					return fmt.Errorf("foreign delivery")
				}
				return calls[tenant.ID].run()
			},
		}}); err != nil {
			t.Fatal(err)
		}
	}
	for i, app := range apps {
		// Two old-address messages are terminated; its own scoped replay is
		// acknowledged by Consume without entering the handler a second time.
		waitForAck(app, 3)
		if got := calls[tenants[i].ID].count(); got != 1 {
			t.Errorf("app %s handled its already committed event %d times, want once", app, got)
		}
		publish(t, conn, tenants[i], name, nil)
		if err := events.RelayApp(ctx, conn, brokers[i], app); err != nil {
			t.Fatal(err)
		}
		waitForAck(app, 4)
		if got := calls[tenants[i].ID].count(); got != 2 {
			t.Errorf("app %s handled %d events after fresh work, want its two unique events", app, got)
		}
	}
}
