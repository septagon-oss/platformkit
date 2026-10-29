package events

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestAnOwedDeliveryRemainsReplayableAfterRetryExhaustion(t *testing.T) {
	fast(t)
	admin, conn := dbtest.Schema(t)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "owed"}
	name := "ledger.invoice_issued"
	durable := appname.Durable("", "ledger", name)
	transport := memory.New()
	var effects int
	if err := Consume(ctx, conn, transport, []Subscription{{
		Module: "ledger", Name: name,
		Handler: func(ctx context.Context, _ db.Tx[db.Tenant], _ Event) error {
			return Defer(ctx, func(context.Context) error { effects++; return nil })
		},
	}}); err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	if err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := Publish(ctx, tx, name, map[string]string{"invoice": "pending"}); err != nil {
			return err
		}
		if err := tx.DB().Raw("SELECT id FROM platformkit_outbox WHERE name = ?", name).Row().Scan(&id); err != nil {
			return err
		}
		// This is the durable state left when a worker dies after commit and
		// before the deferred closure can run. No credential is persisted.
		return tx.DB().Exec(`INSERT INTO platformkit_handled (event_id, durable, tenant_id, effects_owed)
		 VALUES (?, ?, ?, 1)`, id, durable, tenant.ID).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := Relay(ctx, conn, transport); err != nil {
		t.Fatal(err)
	}
	if effects != 0 {
		t.Fatalf("the exhausted delivery ran %d effects before operator replay", effects)
	}
	var dead int
	if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM platformkit_dead_letters WHERE event_id = $1", id).Scan(&dead); err != nil {
		t.Fatal(err)
	}
	if dead != 1 {
		t.Errorf("retry exhaustion recorded %d dead letters for an owed effect, want 1", dead)
	}

	// Unreviewed terminal work must retain the payload beyond the history window.
	if _, err := admin.ExecContext(ctx, "UPDATE platformkit_outbox SET published_at = now() - interval '8 days' WHERE id = $1", id); err != nil {
		t.Fatal(err)
	}
	if err := Purge(ctx, conn); err != nil {
		t.Fatal(err)
	}
	if _, err := Replay(tenancy.WithActor(ctx, uuid.New()), conn, id, durable, "retry the interrupted effect"); err != nil {
		t.Fatalf("the owed delivery is no longer replayable after history retention: %v", err)
	}
	if err := Relay(ctx, conn, transport); err != nil {
		t.Fatal(err)
	}
	if effects != 1 {
		t.Errorf("replay completed %d effects, want 1", effects)
	}
}
