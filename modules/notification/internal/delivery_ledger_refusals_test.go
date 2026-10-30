package internal

// The sent-once index and read grants are database behavior that a fake would only
// copy, so these cases ask PostgreSQL to decide both.

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/notification/contracts"
)

// ledgerSchema is the module's own SQL read from disk, because a white-box file
// cannot import the package that embeds it.
func ledgerSchema(t *testing.T) (*sql.DB, *db.Conn) {
	t.Helper()
	return dbtest.Schema(t, db.MigrationSource{Owner: "notification", Files: os.DirFS("../migrations")})
}

// ledgerTenant is a tenant of these cases' own: the ledger's rows are keyed by it
// and nothing in the module reads its slug.
func ledgerTenant() tenancy.Tenant {
	return tenancy.Tenant{ID: uuid.New(), Slug: "ledgeracme", Name: "Ledger Acme"}
}

// appRole is the role the application connects as, asked of the connection itself:
// dbtest builds a schema per call, so the name is not something a case may assume.
func appRole(t *testing.T, conn *db.Conn) string {
	t.Helper()
	var role string
	err := db.Run(tenancy.WithTenant(t.Context(), ledgerTenant()), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT current_user").Scan(&role).Error
	})
	if err != nil || role == "" {
		t.Fatalf("who the application connects as = %q: %v", role, err)
	}
	return role
}

// ledgerCount reads the ledger as the schema owner, which is how these cases see a
// table the application role can no longer read. An empty outcome counts every row
// of the notice's.
func ledgerCount(t *testing.T, admin *sql.DB, notice uuid.UUID, outcome string) int64 {
	t.Helper()
	var n int64
	err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM notification_deliveries WHERE notification_id = $1 AND ($2 = '' OR outcome = $2)`,
		notice, outcome).Scan(&n)
	if err != nil {
		t.Fatalf("count the ledger as the owner: %v", err)
	}
	return n
}

// TestTwoAttemptsOfOneChannelWriteOneSentRow is 000056's own answer, asked of the
// index. The outbox redelivers a delivery it did not see acknowledged, so two
// workers can carry one channel at once, and each has to read "not sent yet" from a
// row the other wrote and has not committed. Both then write `sent`: the second
// write waits for the first, and when the first commits the index answers it "not
// written" with no error, so both workers acknowledge the event and one row says the
// person was told. A check before the write cannot decide this — it is the check
// that races — and a stand-in would enforce only its copy of the predicate.
func TestTwoAttemptsOfOneChannelWriteOneSentRow(t *testing.T) {
	admin, conn := ledgerSchema(t)
	tenant, notice := ledgerTenant(), uuid.New()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	if err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return record(tx, notice, ChannelInApp, OutcomeRequested, "")
	}); err != nil {
		t.Fatalf("open the channel: %v", err)
	}

	// One attempt reads the channel, writes, and holds its row uncommitted until the
	// other is waiting on it. The handoff is what puts the second attempt in the
	// state a redelivery reaches, which no pause can be trusted to hit.
	answers := make(chan error, 2)
	carries := func(begin <-chan struct{}, wrote chan<- struct{}, hold <-chan struct{}, pids chan<- int) {
		answers <- db.Run(tenancy.WithTenant(ctx, tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			<-begin
			if pids != nil {
				var pid int
				if err := tx.DB().Raw("SELECT pg_backend_pid()").Scan(&pid).Error; err != nil {
					return err
				}
				pids <- pid
			}
			open, err := closed(tx, notice, ChannelInApp)
			if err != nil {
				return err
			}
			if open {
				t.Error("an attempt read the channel closed by a row no transaction has committed")
			}
			if err := record(tx, notice, ChannelInApp, OutcomeSent, ""); err != nil {
				return err
			}
			if wrote != nil {
				close(wrote)
			}
			if hold != nil {
				<-hold
			}
			return nil
		})
	}
	firstBegin, firstWrote, holdFirstOpen := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go carries(firstBegin, firstWrote, holdFirstOpen, nil)
	close(firstBegin)
	waitOrFail(t, ctx, firstWrote, "the first attempt to write its row")

	// The second attempt starts with the first's row written and uncommitted: its
	// read finds the channel open, and its write blocks on a row that may still
	// vanish. That it is waiting is asked of the database, and only then is the
	// first transaction let commit.
	secondPID := make(chan int, 1)
	secondBegin := make(chan struct{})
	go carries(secondBegin, nil, nil, secondPID)
	close(secondBegin)
	var second int
	select {
	case second = <-secondPID:
	case <-ctx.Done():
		t.Fatal("the second attempt never opened its transaction: ", ctx.Err())
	}
	t.Log("the second writer waits on", awaitWaiting(t, ctx, admin, second, answers))
	close(holdFirstOpen)

	for range 2 {
		select {
		case err := <-answers:
			if err != nil {
				t.Errorf("an attempt against the sent-once index: %v", err)
			}
		case <-ctx.Done():
			t.Fatal("an attempt never answered: ", ctx.Err())
		}
	}
	if sent := ledgerCount(t, admin, notice, OutcomeSent); sent != 1 {
		t.Errorf("two attempts of one channel left %d sent rows, want 1", sent)
	}
	if all := ledgerCount(t, admin, notice, ""); all != 2 {
		t.Errorf("two attempts of one channel left %d rows in all, want the one requested row and one sent row", all)
	}
	// The same write is made once more as the worker makes it after a redelivery
	// whose answer was lost: with the first row committed there is nothing to write
	// and still no error, which is what lets the event be acknowledged.
	if err := db.Run(tenancy.WithTenant(ctx, tenant), conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return record(tx, notice, ChannelInApp, OutcomeSent, "")
	}); err != nil {
		t.Errorf("the write of a redelivered send: %v", err)
	}
	if sent := ledgerCount(t, admin, notice, OutcomeSent); sent != 1 {
		t.Errorf("a redelivered send left %d sent rows, want 1", sent)
	}
	if all := ledgerCount(t, admin, notice, ""); all != 2 {
		t.Errorf("a redelivered send left %d rows, want the original requested and sent rows", all)
	}
}

// TestALedgerItCannotReadIsNotAnsweredAsUndelivered is the ledger's half of a rule
// modules/auth refuses for its own guard: a read that failed is not a read that
// answered. The application role cannot SELECT this ledger here, and each of the
// three reads has to say so — `closed` above all, because its bool answers whether a
// person is about to be touched by a second message. The step whose read is refused
// writes nothing: a mail nobody can prove left is neither a mail the ledger may let
// through nor one it may silently owe again.
func TestALedgerItCannotReadIsNotAnsweredAsUndelivered(t *testing.T) {
	admin, conn := ledgerSchema(t)
	tenant, relay := ledgerTenant(), &counted{}
	svc := NewService(everybody{}, WithMailer(relay))
	ctx := tenancy.WithTenant(t.Context(), tenant)
	var notice, recipient uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Notify(ctx, tx, contracts.Notice{
			Recipient: uuid.New(), Title: "a notice with a mail owed", Wants: contracts.WantsEmail,
		})
		if err != nil {
			return err
		}
		notice, recipient = row.ID, row.RecipientID
		return nil
	}); err != nil {
		t.Fatalf("raise the notice: %v", err)
	}

	role := appRole(t, conn)
	if _, err := admin.ExecContext(t.Context(), "REVOKE SELECT ON notification_deliveries FROM "+pq.QuoteIdentifier(role)); err != nil {
		t.Fatalf("take the ledger's read away from %s: %v", role, err)
	}

	reads := []struct {
		what string
		ask  func(context.Context, db.Tx[db.Tenant]) error
	}{
		{"closed", func(_ context.Context, tx db.Tx[db.Tenant]) error {
			done, err := closed(tx, notice, ChannelEmail)
			if err == nil {
				t.Errorf("closed answered %v with no error for a ledger it cannot read", done)
			}
			return err
		}},
		{"Deliveries", func(_ context.Context, tx db.Tx[db.Tenant]) error {
			rows, err := Deliveries(tx, notice)
			if err == nil {
				t.Errorf("Deliveries answered %d rows with no error for a ledger it cannot read", len(rows))
			}
			return err
		}},
		{"Coverage", func(_ context.Context, tx db.Tx[db.Tenant]) error {
			requested, _, err := Coverage(tx)
			if err == nil {
				t.Errorf("Coverage answered %d requested channels with no error for a ledger it cannot read", requested)
			}
			return err
		}},
	}
	for _, read := range reads {
		// One transaction per read: a refused statement aborts its transaction, and
		// a second read refused for that reason alone would say nothing about itself.
		err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return read.ask(ctx, tx) })
		if err == nil {
			continue // the read answered no error, and said so in its own words just above
		}
		if !strings.Contains(err.Error(), "permission denied") {
			t.Errorf("%s failed with %v, want the refusal it cannot recover from", read.what, err)
		}
	}

	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return SendMail(svc, somewhere{}, true).Handler(ctx, tx, mailRequest(notice, recipient, uuid.New()))
	})
	if err == nil {
		t.Fatal("the mail step answered nil although it could not read the ledger it writes its answer from")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("the mail step with no readable ledger: %v", err)
	}
	if owed := intentCount(t, admin); owed != 0 {
		t.Errorf("the unreadable ledger left %d owed mails, want the step refused before it wrote one", owed)
	}
	if n := relay.n.Load(); n != 0 {
		t.Errorf("the unreadable ledger reached the relay %d times, want no socket", n)
	}
}

// intentCount is what the mail worker was left to send, read as the owner so the
// count is the table's and not one tenant's row-level answer about it.
func intentCount(t *testing.T, admin *sql.DB) int64 {
	t.Helper()
	var n int64
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM "+intentTable).Scan(&n); err != nil {
		t.Fatalf("count the owed mail as the owner: %v", err)
	}
	return n
}

// waitOrFail waits for one named step of a concurrent case and refuses rather than
// waiting on a step that never came.
func waitOrFail(t *testing.T, ctx context.Context, step <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-step:
	case <-ctx.Done():
		t.Fatalf("wait for %s: %v", what, ctx.Err())
	}
}

// awaitWaiting reports what one backend is waiting on, and refuses if it stopped
// waiting unreleased: a writer that finished on its own never met the row this case
// is about. The question is asked of the database and its answer polled, because a
// lock is a state and not an event.
func awaitWaiting(t *testing.T, ctx context.Context, admin *sql.DB, pid int, finished <-chan error) string {
	t.Helper()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		var event string
		err := admin.QueryRowContext(ctx,
			`SELECT COALESCE(wait_event_type || '/' || wait_event, '') FROM pg_stat_activity WHERE pid = $1`,
			pid).Scan(&event)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("ask what backend %d is waiting on: %v", pid, err)
		}
		if strings.HasPrefix(event, "Lock/") || strings.HasPrefix(event, "Transaction/") {
			return event
		}
		select {
		case <-finished:
			t.Fatal("the second attempt wrote its row while the first held it uncommitted")
		case <-ctx.Done():
			t.Fatalf("wait for backend %d to wait: %v", pid, ctx.Err())
		case <-ticker.C:
		}
	}
}
