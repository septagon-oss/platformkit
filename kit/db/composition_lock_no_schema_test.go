// composition_lock_no_schema_test.go pins the other edge of the composition lock's scope:
// a session whose search_path resolves to no namespace has no ledger to write and no
// namespace to name in the lock's second half. `pg_advisory_lock(k, NULL)` answers NULL
// and takes nothing, so a run that went on from there would believe it was alone when it
// was not. kit/db/migrate.go (compositionSchema) refuses that session before any file.
package db_test

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestAMigrationWhosePathNamesNoSchemaIsRefusedAndWritesNothing points a run at a
// search_path whose only schema does not exist and asks three things: the run answers
// with an error rather than success, it answers promptly rather than waiting on a lock,
// and the one table its file would create exists nowhere in the database afterwards.
func TestAMigrationWhosePathNamesNoSchemaIsRefusedAndWritesNothing(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	own := schemaOf(t, migrateURL)
	absent := own + "_absent"
	if len(absent) > 63 {
		absent = absent[len(absent)-63:]
	}

	u, err := url.Parse(migrateURL)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("options", strings.Replace(q.Get("options"), "-csearch_path="+own, "-csearch_path="+absent, 1))
	u.RawQuery = q.Encode()
	if got := schemaOf(t, u.String()); got != absent {
		t.Fatalf("the rewritten URL carries search_path %q, want %q", got, absent)
	}

	const table = "composition_without_namespace_head"
	source := db.MigrationSource{Owner: "nonamespace", Files: fstest.MapFS{
		"000001_head.up.sql": {Data: []byte("CREATE TABLE " + table + " (id bigint PRIMARY KEY)")},
	}}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(t.Context()), 60*time.Second)
	defer cancel()
	err = db.MigrateWith(ctx, u.String(), db.MigrationBudget{}, source)
	if err == nil {
		t.Fatal("a run whose search_path resolves to no schema succeeded; it has no namespace to lock and no ledger to write")
	}
	t.Logf("refused: %v", err)
	if errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a run whose search_path resolves to no schema waited out its context (%v); it must be refused, not queued", err)
	}
	// The refusal must be kit/db's, not the database's. With compositionSchema's
	// `schema == 0` branch taken out, the run goes on to pg_advisory_lock(7240101, 0)
	// — which takes nothing — and is stopped only when the ledger's CREATE TABLE
	// arrives: `db: migrate: create history: ERROR: no schema has been selected to
	// create in (SQLSTATE 3F000)`. That answers the three assertions above just as
	// well, so this one asks for the words only this check says.
	if !strings.Contains(err.Error(), "resolves to no schema") {
		t.Fatalf("the run was refused, but not by the check that names its namespace: %v", err)
	}

	admin := dbtest.Open(t, migrateURL)
	var found int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*) FROM pg_tables WHERE tablename = $1", table).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found != 0 {
		t.Errorf("the refused run left %d table(s) named %s behind; a refused run writes nothing", found, table)
	}
}
