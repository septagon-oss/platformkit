package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

func TestConcurrentReplicasRepairTheSameInvalidIndex(t *testing.T) {
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
	exec(`CREATE TABLE replica_items (id bigint NOT NULL)`)
	exec(`INSERT INTO replica_items VALUES (1)`)
	exec(`CREATE FUNCTION replica_key(value bigint) RETURNS bigint LANGUAGE plpgsql IMMUTABLE AS $$
BEGIN RAISE EXCEPTION 'interrupted build' USING ERRCODE = '40P01'; END $$`)
	const build = `CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS replica_unique ON replica_items (replica_key(id));`
	if _, err := admin.ExecContext(ctx, build); err == nil {
		t.Fatal("seed build must be interrupted")
	}
	var valid bool
	if err := admin.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index
WHERE indexrelid = 'replica_unique'::regclass`).Scan(&valid); err != nil || valid {
		t.Fatalf("seed index valid=%v, error=%v", valid, err)
	}
	exec(`CREATE OR REPLACE FUNCTION replica_key(value bigint) RETURNS bigint LANGUAGE plpgsql IMMUTABLE AS $$
BEGIN PERFORM pg_sleep(0.1); RETURN value; END $$`)
	source := mapSource("replica", "-- pkit: autocommit=true\n"+build)
	start, done := make(chan struct{}), make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			done <- db.MigrateWith(ctx, adminURL, db.MigrationBudget{LockTimeout: new(90 * time.Second)}, source)
		}()
	}
	close(start)
	for range 2 {
		if err := <-done; err != nil {
			t.Errorf("replica failed repairing a shared pending index: %v", err)
			if pgErr, ok := errors.AsType[*pgconn.PgError](err); ok {
				t.Logf("server detail: %s", pgErr.Detail)
			}
		}
	}
	if rows := recordedFiles(t, admin, "replica"); rows != 1 {
		t.Errorf("history rows = %d, want 1", rows)
	}
	if err := admin.QueryRowContext(ctx, `SELECT indisvalid FROM pg_index
WHERE indexrelid = 'replica_unique'::regclass`).Scan(&valid); err != nil || !valid {
		t.Errorf("final index valid=%v, error=%v", valid, err)
	}
	exec(`INSERT INTO replica_items VALUES (1) ON CONFLICT DO NOTHING`)
	var rows int
	if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM replica_items`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("certified index admitted %d duplicate rows", rows)
	}
}
