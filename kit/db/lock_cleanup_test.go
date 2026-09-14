package db_test

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// Faults live in one test schema. The real driver and pool must retire a session
// that might retain an advisory lock, even when PostgreSQL returned an ordinary
// SQL error rather than a broken-connection error.
func TestUnconfirmedAdvisoryLocksDiscardTheirPhysicalConnection(t *testing.T) {
	for _, fault := range []struct {
		name, function, body string
	}{
		{"unlock error", "pg_advisory_unlock", "RAISE EXCEPTION 'unlock failed';"},
		{"unlock false", "pg_advisory_unlock", "RETURN false;"},
		{"unlock timeout", "pg_advisory_unlock", "PERFORM pg_sleep(15); RETURN pg_catalog.pg_advisory_unlock($1);"},
		{"acquisition error", "pg_try_advisory_lock", "PERFORM pg_catalog.pg_try_advisory_lock($1); RAISE EXCEPTION 'failed after acquisition';"},
	} {
		t.Run(fault.name, func(t *testing.T) {
			adminURL, appURL := dbtest.URLs(t)
			admin := dbtest.Open(t, adminURL)
			// Putting pg_catalog explicitly last selects the schema-local fault
			// for this connection only, without replacing any server function.
			parsed, err := url.Parse(appURL)
			if err != nil {
				t.Fatal(err)
			}
			query := parsed.Query()
			query.Set("options", query.Get("options")+",pg_catalog")
			parsed.RawQuery = query.Encode()
			statement := "CREATE FUNCTION " + fault.function + "(bigint) RETURNS boolean LANGUAGE plpgsql AS $$ BEGIN " + fault.body + " END $$"
			if _, err := admin.ExecContext(t.Context(), statement); err != nil {
				t.Fatal(err)
			}
			conn, err := db.OpenWithPool(t.Context(), parsed.String(), db.Pool{MaxOpenConns: 1, MaxIdleConns: 1})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = conn.Close() })
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			started := time.Now()
			unlock, taken, err := db.TryLock(ctx, conn, t.Name())
			if fault.function == "pg_try_advisory_lock" {
				if err == nil || !strings.Contains(err.Error(), "failed after acquisition") || taken || unlock != nil {
					t.Fatalf("failed acquisition = %v, %v, %v", unlock != nil, taken, err)
				}
			} else {
				if err != nil || !taken {
					t.Fatalf("acquisition = %v, %v", taken, err)
				}
				t.Cleanup(unlock)
				cancel()
				unlock()
			}
			if elapsed := time.Since(started); elapsed > 12*time.Second {
				t.Fatalf("lock cleanup took %s; the server fault waits 15s", elapsed)
			}
			if stats := conn.Stats(); stats.OpenConnections != 0 {
				t.Fatalf("unconfirmed lock session returned to the pool: %+v", stats)
			}
			// Connection closure can reach PostgreSQL just after the driver
			// returns. Observe the fixture's own advisory locks until it does.
			deadline := time.Now().Add(2 * time.Second)
			for {
				var held int
				const query = `SELECT count(*) FROM pg_locks l JOIN pg_stat_activity a ON a.pid = l.pid
					WHERE l.locktype = 'advisory' AND a.application_name = current_schema()`
				if err := admin.QueryRowContext(t.Context(), query).Scan(&held); err != nil {
					t.Fatal(err)
				}
				if held == 0 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("discarded session retains %d advisory locks", held)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
