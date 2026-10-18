package db

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
)

// Seed the runner's cached namespace OID to exercise the real lock statements
// across the signed boundary without manufacturing billions of database objects.
func TestCompositionLockAcceptsTheWholeNamespaceOIDRange(t *testing.T) {
	url := os.Getenv("PLATFORMKIT_TEST_ADMIN_URL")
	if url == "" {
		t.Fatal("PLATFORMKIT_TEST_ADMIN_URL is required")
	}
	pool, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, oid := range []int64{2147483647, 2147483648, 3000000000, 4294967295} {
		t.Run(fmt.Sprint(oid), func(t *testing.T) {
			conn, err := pool.Conn(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			defer conn.ExecContext(t.Context(), "SELECT pg_advisory_unlock_all()")
			r := runner{conn: conn, schema: oid}
			if err := r.holdCompositionLock(t.Context()); err != nil {
				t.Fatalf("hold namespace %d: %v", oid, err)
			}
			assertHeld := func(want bool) {
				t.Helper()
				var held bool
				err := conn.QueryRowContext(t.Context(), `SELECT EXISTS (
					SELECT 1 FROM pg_locks WHERE pid = pg_backend_pid()
					AND locktype = 'advisory' AND classid = $1::oid
					AND objid = $2::oid AND objsubid = 2 AND granted
				)`, compositionLockKey, oid).Scan(&held)
				if err != nil {
					t.Fatal(err)
				}
				if held != want {
					t.Fatalf("namespace %d held = %t, want %t", oid, held, want)
				}
			}
			assertHeld(true)
			if err := r.releaseCompositionLock(t.Context()); err != nil {
				t.Fatalf("release namespace %d: %v", oid, err)
			}
			assertHeld(false)
		})
	}
}
