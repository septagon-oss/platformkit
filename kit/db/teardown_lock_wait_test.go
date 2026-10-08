package db_test

import (
	"database/sql"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestTheRemovalSessionCarriesItsLockWait reads back, from the server, the lock
// wait the fixture's removal sets through the startup `options`. A malformed
// option either refuses the connection or is silently not applied, and in the
// second case a lost lock race queues behind a DROP nobody waits for any more —
// the hang the wait exists to end.
func TestTheRemovalSessionCarriesItsLockWait(t *testing.T) {
	adminURL := os.Getenv("PLATFORMKIT_TEST_ADMIN_URL")
	if adminURL == "" {
		t.Fatalf("PLATFORMKIT_TEST_ADMIN_URL is unset; start the stack with `make up` and export the test URLs")
	}
	dsn, err := withLockWait(adminURL)
	if err != nil {
		t.Fatalf("withLockWait: %v", err)
	}
	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pool.Close() }()
	var lockWait string
	if err := pool.QueryRowContext(t.Context(), "SHOW lock_timeout").Scan(&lockWait); err != nil {
		t.Fatalf("a session opened with the removal's options could not be asked: %v", err)
	}
	// Go's Duration string against the server's: both read "3s" for whole
	// seconds, and differ off them ("2.5s" against "2500ms").
	if lockWait != dropLockWait.String() {
		t.Errorf("lock_timeout = %q on the removal's session, want %s (dropLockWait)", lockWait, dropLockWait)
	}
}
