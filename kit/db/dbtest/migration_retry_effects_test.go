package dbtest

import (
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
)

func TestARetriedMigrationAppliesItsRowsAndHistoryOnce(t *testing.T) {
	adminURL, _ := URLs(t)
	admin := Open(t, adminURL)
	// Sequence increments survive rollback, so the first attempt can refuse
	// inside PostgreSQL without depending on a timer or another session.
	if _, err := admin.ExecContext(t.Context(), `CREATE SEQUENCE attempts;
		CREATE TABLE effects (value integer)`); err != nil {
		t.Fatal(err)
	}
	source := db.MigrationSource{Owner: "retry", Files: fstest.MapFS{
		"1_effect.up.sql": {Data: []byte(`
INSERT INTO effects VALUES (1);
DO $$ BEGIN
    IF nextval('attempts') = 1 THEN
        RAISE EXCEPTION 'lock unavailable' USING ERRCODE = '55P03';
    END IF;
END $$;
`)},
	}}
	if err := migrate(t, t.Context(), adminURL, source); err != nil {
		t.Fatalf("retryable migration did not complete: %v", err)
	}
	// Reopening the same history must not execute the already applied file.
	if err := migrate(t, t.Context(), adminURL, source); err != nil {
		t.Fatalf("reopening applied history: %v", err)
	}
	var attempts, rows, history int
	if err := admin.QueryRowContext(t.Context(), `SELECT
		(SELECT last_value FROM attempts),
		(SELECT count(*) FROM effects),
		(SELECT count(*) FROM schema_migrations WHERE owner = 'retry')`).
		Scan(&attempts, &rows, &history); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || rows != 1 || history != 1 {
		t.Fatalf("attempts=%d rows=%d history=%d; want 2 attempts and one committed effect and history row",
			attempts, rows, history)
	}
}
