package db_test

import (
	"database/sql"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
)

func TestMigrationInspectionReportsPendingWithoutChangingSchema(t *testing.T) {
	pending := db.MigrationSource{Owner: "inspectcase", Files: fstest.MapFS{
		"000001_create.up.sql": {Data: []byte("CREATE TABLE inspection_probe (id integer)")},
	}}
	adminURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, migrations.Source); err != nil {
		t.Fatal(err)
	}
	admin := dbtest.Open(t, adminURL)
	var before int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM schema_migrations`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	got, err := db.InspectMigrations(t.Context(), adminURL, migrations.Source, pending)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Pending) != 1 || got.Pending[0] != "inspectcase/000001_create.up.sql" {
		t.Fatalf("inspection=%+v, want one pending migration", got)
	}
	var after int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM schema_migrations`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("inspection changed migration history from %d rows to %d", before, after)
	}
	var table sql.NullString
	if err := admin.QueryRowContext(t.Context(), `SELECT to_regclass('inspection_probe')::text`).Scan(&table); err != nil {
		t.Fatal(err)
	}
	if table.Valid {
		t.Fatalf("inspection created migration table %q", table.String)
	}
}
