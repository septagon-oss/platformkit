package db_test

// MigrateWithOptions and BackfillWithOptions are the six older doors reduced to one
// spelling each; what a delegation can break is what these cases pin — the sentence of
// a refused run, the defaults of one naming nothing, its declaration, its ledger.

import (
	"database/sql"
	"errors"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// aWaitInThePast is a duration no run may be given, refused before either connects.
func aWaitInThePast() *time.Duration {
	bad := -time.Second
	return &bad
}

// TestMigrateOptionsRefusesABudgetInThePastTheWayMigrateWithDoes: the refusal is
// byte-identical on both doors, and neither reached a database to answer it. A
// delegate that filled a default in, or wrapped the sentence again, is a new text.
func TestMigrateOptionsRefusesABudgetInThePastTheWayMigrateWithDoes(t *testing.T) {
	lock, statement := aWaitInThePast(), aWaitInThePast()
	const unreachable = "postgres://platformkit_owner:hunter2@127.0.0.1:1/platformkit?sslmode=disable&connect_timeout=1"
	source := db.MigrationSource{Owner: "unreachable", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY)")},
	}}
	for _, budget := range []db.MigrationBudget{{LockTimeout: lock}, {StatementTimeout: statement}} {
		legacy, errLegacy := migrateOne(func() error {
			return db.MigrateWith(t.Context(), unreachable, budget, source)
		})
		options, errOptions := migrateOne(func() error {
			return db.MigrateWithOptions(t.Context(), unreachable, db.MigrateOptions{Budget: budget}, source)
		})
		if errLegacy == nil || errOptions == nil {
			t.Fatal("a budget in the past was accepted")
		}
		if legacy != options {
			t.Errorf("MigrateWith says %q and MigrateWithOptions says %q", legacy, options)
		}
		if !strings.HasPrefix(legacy, "db: migrate: ") || !strings.Contains(legacy, "is a wait in the past") {
			t.Errorf("the refusal is not the sentence the runner writes: %q", legacy)
		}
		if errors.Is(errLegacy, db.ErrContended) || errors.Is(errOptions, db.ErrContended) {
			t.Errorf("a run refused for its budget reported contention instead: %v", errLegacy)
		}

		_, errBackfill := migrateOne(func() error {
			return db.BackfillWith(t.Context(), unreachable, budget, source)
		})
		_, errBackfillOptions := migrateOne(func() error {
			return db.BackfillWithOptions(t.Context(), unreachable, db.MigrateOptions{Budget: budget}, source)
		})
		if errBackfill == nil || errBackfillOptions == nil {
			t.Fatal("the drain accepted a budget in the past")
		}
		if errBackfill.Error() != errBackfillOptions.Error() {
			t.Errorf("BackfillWith says %q and BackfillWithOptions says %q", errBackfill, errBackfillOptions)
		}
	}
}

// migrateOne runs one door and returns its error's text.
func migrateOne(run func() error) (string, error) {
	err := run()
	if err == nil {
		return "", nil
	}
	return err.Error(), err
}

// TestConnectionFailureAtTheOptionsDoorCarriesNoCredential: the boot and the worker now
// stand at these doors, so the host stays in the message and the password out of it.
func TestConnectionFailureAtTheOptionsDoorCarriesNoCredential(t *testing.T) {
	const secret = "hunter2-not-a-real-password"
	url := "postgres://platformkit_owner:" + secret + "@127.0.0.1:1/platformkit?sslmode=disable&connect_timeout=2"
	source := db.MigrationSource{Owner: "unreachable", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY)")},
	}}
	for _, pair := range []struct {
		name        string
		legacy      func() error
		withOptions func() error
	}{
		{"migrate",
			func() error { return db.MigrateWith(t.Context(), url, db.MigrationBudget{}, source) },
			func() error { return db.MigrateWithOptions(t.Context(), url, db.MigrateOptions{}, source) }},
		{"backfill",
			func() error { return db.BackfillWith(t.Context(), url, db.MigrationBudget{}, source) },
			func() error { return db.BackfillWithOptions(t.Context(), url, db.MigrateOptions{}, source) }},
	} {
		legacy, options := pair.legacy(), pair.withOptions()
		if legacy == nil || options == nil {
			t.Fatalf("%s: a port nothing listens on answered success", pair.name)
		}
		if legacy.Error() != options.Error() {
			t.Errorf("%s: the older door says %q and the options door %q", pair.name, legacy, options)
		}
		if !strings.Contains(options.Error(), "127.0.0.1") {
			t.Errorf("%s: the failure names no host: %v", pair.name, options)
		}
		if strings.Contains(options.Error(), secret) {
			t.Errorf("%s: the failure carries the password out of the connection string: %v", pair.name, options)
		}
	}
}

// pairFiles is one history both doors apply: a table, then a data file that fills it.
func pairFiles() fstest.MapFS {
	return fstest.MapFS{
		"000001_seats.up.sql": {Data: []byte(`CREATE TABLE seats (id bigint PRIMARY KEY, label text NOT NULL DEFAULT '');
INSERT INTO seats (id) SELECT g FROM generate_series(1, 7) g`)},
		"000002_place.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=seats
UPDATE seats SET label = 'seat ' || id WHERE id IN (SELECT id FROM batch)`)},
	}
}

// ledger reads one schema's history as a run of either door leaves it.
func ledger(t *testing.T, admin *sql.DB) string {
	t.Helper()
	rows, err := admin.Query(`SELECT owner, version, name, checksum FROM schema_migrations ORDER BY owner, version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out strings.Builder
	for rows.Next() {
		var owner, name, checksum string
		var version int64
		if err := rows.Scan(&owner, &version, &name, &checksum); err != nil {
			t.Fatal(err)
		}
		out.WriteString(owner + "/" + name + "/" + checksum + "\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

// freshSchema hands back a namespace one door has not touched, named after the test.
func freshSchema(t *testing.T) (adminURL string, admin *sql.DB) {
	t.Helper()
	adminURL, _ = dbtest.URLsFor(t)
	return adminURL, dbtest.Open(t, adminURL)
}

// TestMigrateOptionsWithNoOptionsAppliesWhatMigrateApplies: the zero options are the
// run Migrate performs — file for file, checksum for checksum, rows included.
func TestMigrateOptionsWithNoOptionsAppliesWhatMigrateApplies(t *testing.T) {
	source := db.MigrationSource{Owner: "pair", Files: pairFiles()}
	first, firstAdmin := freshSchema(t)
	if err := db.Migrate(t.Context(), first, source); err != nil {
		t.Fatal(err)
	}
	want, wantWork, wantQueue := ledger(t, firstAdmin), drained(t, firstAdmin), queue(t, firstAdmin)

	second, secondAdmin := freshSchema(t)
	if err := db.MigrateWithOptions(t.Context(), second, db.MigrateOptions{}, source); err != nil {
		t.Fatal(err)
	}
	if got := ledger(t, secondAdmin); got != want {
		t.Errorf("the two doors left different history:\n%s---\n%s", want, got)
	}
	if got := drained(t, secondAdmin); got != wantWork {
		t.Errorf("the two doors left the data file at %d rows against %d", got, wantWork)
	}
	if got := queue(t, secondAdmin); got != wantQueue {
		t.Errorf("the two doors left %d drains pending against %d", got, wantQueue)
	}
}

// queue counts the drains a run left for the worker.
func queue(t *testing.T, admin *sql.DB) int {
	t.Helper()
	return countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill")
}

// TestMigrateOptionsCarriesWhatMigrateDeclaringCarries: the declaration reaches the
// session before the files, on either door, NULL kept for what was not declared.
func TestMigrateOptionsCarriesWhatMigrateDeclaringCarries(t *testing.T) {
	app := "pair-app"
	decl := db.Declaration{App: &app, Hosts: []string{"acme.example", "globex.example"}, Tenants: map[string]string{"acme": "pair-app"}}
	record := `INSERT INTO declared (app, hosts, tenants)
		VALUES (current_setting('platformkit.app', true), current_setting('platformkit.app_hosts', true), current_setting('platformkit.app_tenants', true))`
	first, firstAdmin := freshSchema(t)
	declaredFirst := declaredThrough(t, first, record, func(url string, files fstest.MapFS) error {
		return db.MigrateDeclaring(t.Context(), url, db.MigrationBudget{}, decl, db.MigrationSource{Owner: "pair", Files: files})
	})
	second, secondAdmin := freshSchema(t)
	declaredSecond := declaredThrough(t, second, record, func(url string, files fstest.MapFS) error {
		return db.MigrateWithOptions(t.Context(), url, db.MigrateOptions{Declaration: decl}, db.MigrationSource{Owner: "pair", Files: files})
	})
	for _, schema := range []struct {
		name   string
		admin  *sql.DB
		placed []string
	}{{"MigrateDeclaring", firstAdmin, declaredFirst}, {"MigrateWithOptions", secondAdmin, declaredSecond}} {
		if n := countRows(t, schema.admin, "SELECT count(*) FROM declared"); n != 1 {
			t.Fatalf("%s placed %d declaration rows, want 1", schema.name, n)
		}
	}
	if strings.Join(declaredFirst, "|") != strings.Join(declaredSecond, "|") {
		t.Errorf("the declaration arrived differently: %v against %v", declaredFirst, declaredSecond)
	}
	if declaredFirst[0] != app || declaredFirst[1] != "acme.example,globex.example" || declaredFirst[2] != "acme=pair-app" {
		t.Errorf("neither door declared what the composition named: %v", declaredFirst)
	}
}

// declaredThrough runs one door over a schema whose file records its session settings.
func declaredThrough(t *testing.T, url, record string, door func(url string, files fstest.MapFS) error) []string {
	t.Helper()
	files := fstest.MapFS{
		"000001_declared.up.sql": {Data: []byte("CREATE TABLE declared (app text, hosts text, tenants text)")},
		"000002_record.up.sql":   {Data: []byte(record)},
	}
	if err := door(url, files); err != nil {
		t.Fatal(err)
	}
	var app, hosts, tenants sql.NullString
	if err := dbtest.Open(t, url).QueryRow(`SELECT app, hosts, tenants FROM declared`).Scan(&app, &hosts, &tenants); err != nil {
		t.Fatal(err)
	}
	return []string{nullable(app), nullable(hosts), nullable(tenants)}
}

func nullable(s sql.NullString) string {
	if !s.Valid {
		return "<none>"
	}
	return s.String
}

// TestBackfillWithOptionsDrainsWhatBackfillDeclaringDrains: the drain takes the same
// pair the migration takes, because a placement reads the session its own batch set.
func TestBackfillWithOptionsDrainsWhatBackfillDeclaringDrains(t *testing.T) {
	decl := db.Declaration{Hosts: []string{"acme.example"}}
	first, firstAdmin := freshSchema(t)
	if err := db.Migrate(t.Context(), first, db.MigrationSource{Owner: "pair", Files: pairFiles()}); err != nil {
		t.Fatal(err)
	}
	if err := db.BackfillDeclaring(t.Context(), first, db.MigrationBudget{}, decl, db.MigrationSource{Owner: "pair", Files: pairFiles()}); err != nil {
		t.Fatal(err)
	}
	want, wantLedger := drained(t, firstAdmin), ledger(t, firstAdmin)

	second, secondAdmin := freshSchema(t)
	if err := db.Migrate(t.Context(), second, db.MigrationSource{Owner: "pair", Files: pairFiles()}); err != nil {
		t.Fatal(err)
	}
	if err := db.BackfillWithOptions(t.Context(), second, db.MigrateOptions{Declaration: decl}, db.MigrationSource{Owner: "pair", Files: pairFiles()}); err != nil {
		t.Fatal(err)
	}
	if got := drained(t, secondAdmin); got != want {
		t.Errorf("the two drains left %d rows against %d", got, want)
	}
	if got := ledger(t, secondAdmin); got != wantLedger {
		t.Errorf("the two drains left different history:\n%s---\n%s", wantLedger, got)
	}
	if n := queue(t, secondAdmin); n != 0 {
		t.Errorf("the drain left %d cursor rows behind; a finished file is removed from the queue", n)
	}
}

func drained(t *testing.T, admin *sql.DB) int {
	t.Helper()
	return countRows(t, admin, "SELECT count(*) FROM seats WHERE label <> ''")
}
