package app

// A case that fills the counter table through the limiter has to ask the table what
// landed, because the answer that records nothing carries no error to notice: a busy
// counter refuses with the whole window and no error, so that a caller which fails
// open on an error cannot let a queued counter admit the traffic the counter exists
// to refuse. This asks the store which of its two answers left a row.
//
// It refuses the counter on purpose with a lock this case holds itself, at the
// weakest mode that stands in the way of the counter's write: the refusal then
// arrives at the queue budget the counter writes into its own transaction — the
// server ending the wait, not a sleep somebody hoped was long enough — and the table
// stays readable throughout, which a stronger lock would take away.

import (
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/limit"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// TestARefusedCounterAttemptRecordsNoRowThePurgeWouldCount checks that the counter's
// two answers differ where a case can see it: the attempt a busy counter withheld
// leaves no row, and the attempt it admits leaves the one row the purge is then
// judged on. An attempt that recorded nothing yet wrote a row would let a case seed
// windows nobody counted, and an admission that wrote none would make every
// assertion downstream a statement about a table nobody filled.
func TestARefusedCounterAttemptRecordsNoRowThePurgeWouldCount(t *testing.T) {
	admin, conn := dbtest.Schema(t)

	// The counters are system-only rows, so the owner reads them as the purge does.
	rows := func() int {
		t.Helper()
		var n int
		if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM platformkit_limits`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if got := rows(); got != 0 {
		t.Fatalf("a fresh namespace holds %d counter rows, want none standing in for the ones below", got)
	}

	release := holdCounterTable(t, admin)
	admitted, _, _, err := countOneEvent(t, conn, "withheld")
	if err != nil {
		t.Fatalf("counting against a counter whose table is held: %v", err)
	}
	if admitted {
		t.Error("the counter admitted an event while another session held its table; the first event of a quota of " +
			"three is owed the row it wrote, not an answer that outran a lock standing in its way")
	}
	if got := rows(); got != 0 {
		t.Errorf("the counter holds %d rows after answering that it recorded nothing; an attempt that reached no "+
			"row leaves nothing behind, or a case that reads only the error seeds a window nobody counted", got)
	}

	release(t)
	// The patience the purge case owes a busy counter, asked of a table this case just
	// unlocked: an admission is owed here, and one ask the pool could not serve is the
	// counter being busy rather than the case being wrong.
	var admittedNow bool
	for ask := 1; ; ask++ {
		var err error
		admittedNow, _, _, err = countOneEvent(t, conn, "admitted")
		if err != nil {
			t.Fatalf("counting once the counter's table is free: %v", err)
		}
		if admittedNow {
			break
		}
		if ask >= seedAsks {
			t.Fatalf("the counter refused the first event of a quota of three %d times with its table free", ask)
		}
		time.Sleep(seedPause)
	}
	if got := rows(); got != 1 {
		t.Errorf("an admitted event put %d rows on the counter table, want the one window it answered with", got)
	}
}

// countOneEvent reserves the way a request does — the connection the middleware
// opened, under a tenant of its own, through the limiter httpx is handed — and hands
// back the four answers as they came. A quota of three makes the first event owed an
// admission, so the verdict distinguishes the two answers by itself.
func countOneEvent(t *testing.T, conn *db.Conn, name string) (bool, int, time.Duration, error) {
	t.Helper()
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: uuid.New(), Slug: name}), conn)
	return limit.Postgres(httpx.ConnFrom).Reserve(ctx, name, 3, time.Minute)
}

// holdCounterTable locks the counter's table from a session of this test's own and
// answers with the way to unlock it. SHARE is the mode that stands in the way of the
// counter's own row lock while leaving the owner's read of the table free, so a case
// can ask what the attempt did while the attempt is still being answered; the lock is
// one transaction's, in this namespace's schema, and dies with that transaction.
func holdCounterTable(t *testing.T, admin *sql.DB) func(*testing.T) {
	t.Helper()
	c, err := admin.Conn(t.Context())
	if err != nil {
		t.Fatalf("a connection to hold the counter's table: %v", err)
	}
	tx, err := c.BeginTx(t.Context(), nil)
	if err != nil {
		_ = c.Close()
		t.Fatalf("a transaction to hold the counter's table: %v", err)
	}
	if _, err := tx.ExecContext(t.Context(), "LOCK platformkit_limits IN SHARE MODE"); err != nil {
		_ = tx.Rollback()
		_ = c.Close()
		t.Fatalf("holding the counter's table: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(); _ = c.Close() })
	return func(t *testing.T) {
		t.Helper()
		if err := tx.Rollback(); err != nil {
			t.Fatalf("releasing the counter's table: %v", err)
		}
		if err := c.Close(); err != nil {
			t.Fatalf("closing the session that held the counter's table: %v", err)
		}
	}
}
