package events

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events/internal/delivery"
	provider "github.com/septagon-oss/platformkit/kit/events/providers/nats"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

func TestATerminalRetryAfterTheLedgerMoveCannotReviveTheHandler(t *testing.T) {
	fast(t)
	owner, conn := dbtest.Schema(t)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "shop"}
	if _, err := owner.ExecContext(ctx,
		`INSERT INTO tenants (id, slug, name, app) VALUES ($1, 'shop', 'Shop', '')`, tenant.ID); err != nil {
		t.Fatal(err)
	}
	// The existing terminal-recovery fixture makes persistence fail while the
	// broker keeps its exhausted delivery pending. No failed attempt commits.
	if _, err := owner.ExecContext(ctx, `CREATE SEQUENCE terminal_attempts;
		ALTER TABLE platformkit_dead_letters ADD CONSTRAINT unavailable
		CHECK (nextval('terminal_attempts') < 0) NOT VALID`); err != nil {
		t.Fatal(err)
	}
	old, js := jetstreamForTest(t)
	name := "ledger_" + tenant.ID.String()[:8] + ".invoice_issued"
	effect := "ledger_" + tenant.ID.String()[:8] + ".action_committed"
	unscoped := appname.Durable("", "ledger", name)
	scoped := appname.Durable("collect", "ledger", name)
	t.Cleanup(func() {
		for _, durable := range []string{unscoped, scoped} {
			_ = js.DeleteConsumer(stream, durable)
		}
		for _, app := range []appname.Name{"", "collect"} {
			_ = js.PurgeStream(stream, &nats.StreamPurgeRequest{Subject: appname.Subject(app, tenant.ID, name)})
		}
	})
	worker, stop := context.WithCancel(ctx)
	defer stop()
	var attempts atomic.Int64
	sub := Subscription{Module: "ledger", Name: name, Handler: func(context.Context, db.Tx[db.Tenant], Event) error {
		attempts.Add(1)
		return errors.New("permanent provider refusal")
	}}
	if err := Consume(worker, conn, old, []Subscription{sub}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return Publish(ctx, tx, name, nil)
	}); err != nil {
		t.Fatal(err)
	}
	// All old-address publication finishes before the deployment names its app.
	if err := Relay(ctx, conn, old); err != nil {
		t.Fatal(err)
	}
	tick := time.Tick(10 * time.Millisecond)
	for {
		info, err := js.ConsumerInfo(stream, unscoped)
		if err != nil {
			t.Fatal(err)
		}
		var tried bool
		if err := owner.QueryRowContext(ctx, `SELECT is_called FROM terminal_attempts`).Scan(&tried); err != nil {
			t.Fatal(err)
		}
		if tried && info.Delivered.Consumer > uint64(delivery.MaxDeliveries) {
			break
		}
		select {
		case <-tick:
		case <-ctx.Done():
			t.Fatal("delivery did not exhaust its handler attempts: ", ctx.Err())
		}
	}
	stop()
	if err := old.(interface{ Close() error }).Close(); err != nil {
		t.Fatal(err)
	}
	if got := attempts.Load(); got != int64(delivery.MaxDeliveries) {
		t.Fatalf("old handler attempts = %d, want %d", got, delivery.MaxDeliveries)
	}
	if err := dbtest.System(ctx, conn, func(_ context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec(`SELECT platformkit_place_tenants('collect', '', 'shop=collect', NULL)`).Error
	}); err != nil {
		t.Fatal(err)
	}
	// An unfinished terminal write must either keep the move refused until it
	// commits, or recover under the new durable. A successful move cannot leave
	// an acknowledged terminal outcome invisible to its scoped subscription.
	report, moveErr := MoveLedger(ctx, conn, "collect", "boot")
	if moveErr != nil && !errors.Is(moveErr, ErrLedgerMoveContended) {
		t.Fatal(moveErr)
	}
	if moveErr != nil {
		var records int
		if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_outbox WHERE name=$1`, EventLedgerMoved).Scan(&records); err != nil {
			t.Fatal(err)
		}
		if report != (MoveReport{}) || records != 0 {
			t.Errorf("refused move returned %+v and emitted %d records, want neither", report, records)
		}
	}
	if _, err := owner.ExecContext(ctx, `ALTER TABLE platformkit_dead_letters DROP CONSTRAINT unavailable`); err != nil {
		t.Fatal(err)
	}
	recovery, _ := jetstreamForTest(t)
	if err := Consume(ctx, conn, recovery, []Subscription{sub}); err != nil {
		t.Fatal(err)
	}
	waitForCompletion := func(durable string) {
		t.Helper()
		for {
			info, err := js.ConsumerInfo(stream, durable)
			if err != nil {
				t.Fatal(err)
			}
			if info.AckFloor.Consumer >= 1 && info.NumPending == 0 && info.NumAckPending == 0 {
				return
			}
			select {
			case <-tick:
			case <-ctx.Done():
				t.Fatalf("consumer %s did not finish its delivery: %+v", durable, info)
			}
		}
	}
	waitForCompletion(unscoped)
	if moveErr != nil {
		if _, err := MoveLedger(ctx, conn, "collect", "boot"); err != nil {
			t.Fatal(err)
		}
	}
	var oldDead, newDead int
	if err := owner.QueryRowContext(ctx,
		`SELECT count(*) FILTER (WHERE durable=$1), count(*) FILTER (WHERE durable=$2)
		FROM platformkit_dead_letters WHERE tenant_id=$3`, unscoped, scoped, tenant.ID).Scan(&oldDead, &newDead); err != nil {
		t.Fatal(err)
	}
	t.Logf("move report=%+v error=%v; acknowledged terminal records: old=%d scoped=%d", report, moveErr, oldDead, newDead)
	if got := attempts.Load(); got != int64(delivery.MaxDeliveries) {
		t.Errorf("terminal recovery reran the old handler: %d attempts", got)
	}
	var id uuid.UUID
	if err := owner.QueryRowContext(ctx, `SELECT id FROM platformkit_outbox WHERE name=$1`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(ctx, `UPDATE platformkit_outbox SET published_at=NULL WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	fresh, err := provider.JetStream("collect", os.Getenv("PLATFORMKIT_TEST_NATS_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fresh.(interface{ Close() error }).Close() })
	if err := RelayApp(ctx, conn, fresh, "collect"); err != nil {
		t.Fatal(err)
	}
	var revived atomic.Int64
	if err := Consume(ctx, conn, fresh, []Subscription{{App: "collect", Module: "ledger", Name: name,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], _ Event) error {
			revived.Add(1)
			return Publish(ctx, tx, effect, nil)
		}}}); err != nil {
		t.Fatal(err)
	}
	waitForCompletion(scoped)
	if got := revived.Load(); got != 0 {
		t.Errorf("scoped replay revived a terminal event's handler %d times after the move; want zero without an operator replay", got)
	}
	var effects int
	if err := owner.QueryRowContext(ctx, `SELECT count(*) FROM platformkit_outbox WHERE name=$1`, effect).Scan(&effects); err != nil {
		t.Fatal(err)
	}
	if effects != 0 {
		t.Errorf("terminal event committed %d new effects after the move; want zero", effects)
	}
}
