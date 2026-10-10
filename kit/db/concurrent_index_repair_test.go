package db_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestConcurrentIndexRepairRebuildsAfterACancelledBuild(t *testing.T) {
	for _, tc := range []struct {
		name     string
		failures int
		code     string
		attempts int
		applied  int
	}{
		{"two cancelled builds", 2, "40P01", 3, 1},
		{"retry budget exhausted", 5, "40P01", 5, 0},
		{"other errors are not retried", 2, "23514", 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			adminURL, _ := dbtest.URLs(t)
			admin := dbtest.Open(t, adminURL)
			ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
			defer cancel()
			exec := func(statement string) {
				t.Helper()
				if _, err := admin.ExecContext(ctx, statement); err != nil {
					t.Fatal(err)
				}
			}
			exec(`CREATE TABLE repair_items (id bigint NOT NULL)`)
			exec(`INSERT INTO repair_items VALUES (1)`)
			exec(`CREATE SEQUENCE repair_attempts`)
			// Like the composition-lock fixture, a sequence preserves the count when
			// an injected SQLSTATE aborts the statement. The expression is deliberately
			// marked immutable only to inject a failure inside PostgreSQL's real build.
			function := func(failures int, code string) string {
				return fmt.Sprintf(`CREATE OR REPLACE FUNCTION repair_key(value bigint)
RETURNS bigint LANGUAGE plpgsql IMMUTABLE AS $$
BEGIN
  IF EXISTS (SELECT 1 FROM pg_locks WHERE pid = pg_backend_pid()
             AND locktype = 'advisory' AND classid = 0 AND objid = %d AND granted) THEN
    RAISE EXCEPTION 'index repair holds an advisory lock' USING ERRCODE = 'P0001';
  END IF;
  IF nextval('repair_attempts') <= %d THEN
    RAISE EXCEPTION 'interrupted index build' USING ERRCODE = '%s';
  END IF;
  RETURN value;
END $$`, compositionLockKey, failures, code)
			}
			exec(function(1, "40P01"))
			const build = `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS repair_unique
ON repair_items (repair_key(id));`
			_, seedErr := admin.ExecContext(ctx, build)
			if pgErr, ok := errors.AsType[*pgconn.PgError](seedErr); !ok || pgErr.Code != "40P01" {
				t.Fatalf("seed interrupted build: %v", seedErr)
			}
			var valid bool
			if err := admin.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index
WHERE indexrelid = 'repair_unique'::regclass`).Scan(&valid); err != nil || valid {
				t.Fatalf("seed must leave an invalid index: valid=%v, error=%v", valid, err)
			}
			exec(`ALTER SEQUENCE repair_attempts RESTART WITH 1`)
			exec(function(tc.failures, tc.code))
			err := db.MigrateWith(ctx, adminURL,
				db.MigrationBudget{LockTimeout: new(90 * time.Second)},
				mapSource("repair", "-- pkit: autocommit=true\n"+build))
			if tc.applied == 1 {
				if err != nil {
					t.Errorf("repair must drop and rebuild after each cancelled build: %v", err)
				}
			} else if pgErr, ok := errors.AsType[*pgconn.PgError](err); !ok || pgErr.Code != tc.code {
				t.Errorf("refusal = %v, want SQLSTATE %s", err, tc.code)
			}
			var attempts int
			if err := admin.QueryRowContext(ctx, `SELECT last_value FROM repair_attempts`).Scan(&attempts); err != nil {
				t.Fatal(err)
			}
			if attempts != tc.attempts {
				t.Errorf("build attempts = %d, want %d", attempts, tc.attempts)
			}
			if rows := recordedFiles(t, admin, "repair"); rows != tc.applied {
				t.Errorf("history rows = %d, want %d", rows, tc.applied)
			}
			if err := admin.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index
WHERE indexrelid = 'repair_unique'::regclass`).Scan(&valid); err != nil {
				t.Fatal(err)
			}
			if valid != (tc.applied == 1) {
				t.Errorf("index valid = %v, applied = %d", valid, tc.applied)
			}
			if tc.applied == 1 {
				exec(`INSERT INTO repair_items VALUES (1) ON CONFLICT DO NOTHING`)
				var rows int
				if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM repair_items`).Scan(&rows); err != nil {
					t.Fatal(err)
				}
				if rows != 1 {
					t.Errorf("certified unique index admitted a duplicate: %d rows", rows)
				}
			}
		})
	}
}
