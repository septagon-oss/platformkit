package limit

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestAnAttemptThatEnteredItsTransactionIsNeverReplayed(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	p := postgres{conns: func(context.Context) (*db.Conn, bool) { return conn, true }}
	calls := 0
	err := p.run(t.Context(), func(_ context.Context, tx db.Tx[db.System]) error {
		calls++
		if err := tx.DB().Exec("INSERT INTO platformkit_limits (key, count) VALUES (?, ?)", "transaction-retry", 1).Error; err != nil {
			return err
		}
		return driver.ErrBadConn
	})
	if !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("transaction failure = %v, want the original connection error", err)
	}
	if calls != 1 {
		t.Errorf("transaction callback ran %d times, want one: an entered attempt must never be replayed", calls)
	}
	var count int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_limits WHERE key = $1", "transaction-retry").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("failed attempt committed %d counters, want none", count)
	}
}
