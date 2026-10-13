package app

import (
	"context"
	"testing"
	"time"

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

func TestARefusedLedgerMoveCannotReopenAlreadyHandledWork(t *testing.T) {
	cfg, opts := compose(t)
	opts.Role, opts.App = Worker, "collect"
	if err := db.Migrate(t.Context(), cfg.Database.MigrateURL, migrations.Source); err != nil {
		t.Fatal(err)
	}
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	owner := dbtest.Open(t, cfg.Database.MigrateURL)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "shop"}
	const name = "ledger.invoice_issued"
	durable := appname.Durable("", "ledger", name)
	id := uuid.New()
	if _, err := owner.ExecContext(t.Context(),
		`INSERT INTO tenants (id, slug, name, app) VALUES ($1, 'shop', 'Shop', 'collect')`, tenant.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.ExecContext(t.Context(),
		`INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES ($1, $2, $3)`, id, durable, tenant.ID); err != nil {
		t.Fatal(err)
	}
	// A different delivery under the same old durable keeps the rename refused.
	// The replayed event itself was committed before this transaction began.
	held, release, claimed := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		claimed <- db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			if err := tx.DB().Exec(`INSERT INTO platformkit_handled (event_id, durable, tenant_id) VALUES (?, ?, ?)`,
				uuid.New(), durable, tenant.ID).Error; err != nil {
				return err
			}
			close(held)
			<-release
			return nil
		})
	}()
	defer func() { close(release); <-claimed }()
	select {
	case <-held:
	case err := <-claimed:
		claimed <- err
		t.Fatalf("hold the old durable: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("the claim did not reach its transaction")
	}
	ran := make(chan uuid.UUID, 8)
	tr := memory.New()
	opts.Transport = tr
	mod := module.Module{Name: "ledger", Events: []string{name}, Subscriptions: []events.Subscription{{
		Module: "ledger", Name: name,
		Handler: func(_ context.Context, _ db.Tx[db.Tenant], ev events.Event) error {
			ran <- ev.ID
			return nil
		},
	}}}
	a, err := New(t.Context(), cfg, []module.Module{mod}, opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	finished := make(chan struct{})
	go func() { defer close(finished); _ = a.work(ctx, conn, tr, nil) }()
	defer func() { cancel(); <-finished }()
	// A safe worker may stop or delay startup until the old ledger can move;
	// neither branch is required to print the current implementation's refusal.
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	ev := events.Event{ID: id, Name: name, TenantID: tenant.ID, App: "collect", Payload: []byte(`{}`)}
waiting:
	for {
		select {
		case <-finished:
			break waiting
		case <-deadline.C:
			break waiting
		case <-ticker.C:
			if err := tr.Publish(ctx, ev); err != nil && ctx.Err() == nil {
				t.Fatal(err)
			}
		case got := <-ran:
			t.Errorf("event %s ran again while its committed claim remains under %s and the ledger move is refused", got, durable)
			break waiting
		}
	}
	// Cancel and join before the read: no in-flight callback may escape the
	// assertion by committing after it. The old mark must remain the only mark.
	cancel()
	<-finished
	var copies int
	if err := owner.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_handled WHERE event_id = $1`, id).Scan(&copies); err != nil {
		t.Fatal(err)
	}
	if copies != 1 {
		t.Errorf("already handled event now has %d durable claims, want its one committed claim", copies)
	}
}
