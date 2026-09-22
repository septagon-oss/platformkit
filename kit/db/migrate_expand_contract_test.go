package db_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// These tests specify T-0018: the runner's lock and statement budgets, the
// -- pkit: header grammar, the static rule table, the contract half's wait for
// its expand partner, and the batched data migration that runs outside one
// transaction and resumes where it stopped.
//
// They are written against the door that exists today — db.Migrate,
// db.MigrationSource, a fstest.MapFS and a real database — so that every one of
// them is a red test rather than a description of one. A case that needs a
// symbol the kernel does not have yet is listed in the task's SPECIFY.md as
// unwritable now, with the reason; none of them is silently dropped.

// probeTable is the ordinary first file: a table with the single-column primary
// key a batched backfill keys itself by, and rows put there by the migration
// itself, so the run under test needs no second door.
const probeTable = `CREATE TABLE probe (
	id bigint PRIMARY KEY,
	done boolean NOT NULL DEFAULT false,
	resumed boolean NOT NULL DEFAULT false,
	passes integer NOT NULL DEFAULT 0
);
INSERT INTO probe (id) SELECT g FROM generate_series(1, 25) g`

// TestMigrationsRunInsideTheStatementBudgets: every file runs with a
// lock_timeout budget, and with no statement_timeout unless one is configured.
// The budgets are the runner's, not the operator's patience: a migration that
// queues behind a running application must stop and say so within seconds, and
// an index build is the reason a *statement* budget stays off by default (a
// long build is legitimate, an unbounded lock wait is not).
//
// The file reads the two settings back into a table, which is what an
// application can observe without a second door: the value that was in force
// when its own statements ran.
func TestMigrationsRunInsideTheStatementBudgets(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	source := db.MigrationSource{Owner: "budgets", Files: fstest.MapFS{
		"000001_record.up.sql": {Data: []byte(`CREATE TABLE budgets (setting text, value text);
			INSERT INTO budgets VALUES ('lock_timeout', current_setting('lock_timeout'));
			INSERT INTO budgets VALUES ('statement_timeout', current_setting('statement_timeout'))`)},
	}}
	if err := db.Migrate(t.Context(), migrateURL, source); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// The documented defaults: a five second wait for a lock, and no bound on a
	// statement. A deployment that wants a statement budget sets
	// database.statement_timeout; the runner's own default must not be the
	// operator's patience and must not be a number that kills a real index build.
	for setting, want := range map[string]string{"lock_timeout": "5s", "statement_timeout": "0"} {
		var got string
		scan(t, dbtest.Open(t, migrateURL),
			"SELECT value FROM budgets WHERE setting = "+quoteLiteral(setting), &got)
		if got != want {
			t.Errorf("%s during a migration = %q, want the documented default %q", setting, got, want)
		}
	}
}

// TestAContendedMigrationIsReportedAsContended: the lock timeout is not a
// failure, it is a refusal to wait. Nothing is applied, nothing is recorded, the
// files already applied stay applied, and the message says the operator may run
// it again — which is the difference between a stopped installation and a
// stopped installation that nobody understands.
func TestAContendedMigrationIsReportedAsContended(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	first := &fstest.MapFile{Data: []byte("CREATE TABLE probe (id bigint PRIMARY KEY)")}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "probe", Files: fstest.MapFS{"000001_probe.up.sql": first}}); err != nil {
		t.Fatal(err)
	}

	// Somebody else holds the lock this file asks for. An advisory lock is the
	// only wait a test can start on a shared database without stopping a table
	// every parallel test is using, and the timeout does not know the
	// difference: SQLSTATE 55P03 either way.
	//
	// The file sets its own one millisecond budget as its first statement, so the
	// case is deterministic and takes no time. The runner re-asserts its own
	// budgets before every file, which is what stops a file from leaking one to
	// the next (and a rolled back SET LOCAL never leaves the transaction at all).
	locker := dbtest.Open(t, migrateURL)
	conn, err := locker.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), "SELECT pg_advisory_lock($1)", 7240102); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = conn.ExecContext(t.Context(), "SELECT pg_advisory_unlock($1)", 7240102)
	}()

	files := fstest.MapFS{
		"000001_probe.up.sql": first,
		"000002_index.up.sql": {Data: []byte("-- pkit: phase=expand\n" +
			"SET lock_timeout = '1ms';\nSELECT pg_advisory_lock(7240102);\nCREATE TABLE later (x text)")},
	}
	err = db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "probe", Files: files})
	if err == nil {
		t.Fatal("a migration whose lock wait exceeded the budget returned no error")
	}
	if !strings.Contains(err.Error(), "contended") {
		t.Errorf("contended migration reported %q; the message must say the wait exceeded a budget and that the operator may run it again", err)
	}
	if !strings.Contains(err.Error(), "probe/000002_index.up.sql") {
		t.Errorf("contended migration did not name the file: %q", err)
	}
	admin := dbtest.Open(t, migrateURL)
	var applied int
	scan(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'probe' AND version = 2", &applied)
	if applied != 0 {
		t.Errorf("a contended migration recorded %d history rows; a refusal writes nothing", applied)
	}
	var kept int
	scan(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'probe' AND version = 1", &kept)
	if kept != 1 {
		t.Errorf("the file applied before the contention is gone: %d rows, want 1", kept)
	}
	if tables := countRows(t, admin, "SELECT count(*) FROM pg_class WHERE relname = 'later' AND relnamespace = current_schema()::regnamespace"); tables != 0 {
		t.Errorf("the contended file created %d tables", tables)
	}
}

// TestMigrationHeaderGrammar: the header is a grammar, so every mistake in it
// is refused before the runner connects. A marker the runner would not read —
// one placed after the SQL, one carrying a key it does not have, one repeated
// with two values — is worse than no marker, because it says "this file was
// reviewed" about a file that was not.
func TestMigrationHeaderGrammar(t *testing.T) {
	body := "CREATE TABLE grammar (x integer)"
	for _, tc := range []struct {
		name, header, want string
	}{
		{"unknown key", "-- pkit: phase=expand\n-- pkit: draught=5", "draught"},
		{"repeated key", "-- pkit: phase=expand\n-- pkit: phase=contract", "phase"},
		{"unknown phase", "-- pkit: phase=migrate", "expand"},
		{"batch without data phase", "-- pkit: phase=expand\n-- pkit: batch=500", "phase=data"},
		{"batch of zero", "-- pkit: phase=data\n-- pkit: batch=0\n-- pkit: table=probe", "batch"},
		{"batch above the bound", "-- pkit: phase=data\n-- pkit: batch=10000000\n-- pkit: table=probe", "batch"},
		{"data without a table", "-- pkit: phase=data\n-- pkit: batch=500", "table"},
		{"data with a name, not an identifier", "-- pkit: phase=data\n-- pkit: batch=500\n-- pkit: table=probe; DROP TABLE x", "table"},
		{"contract without its expand", "-- pkit: phase=contract", "expand="},
		{"expand partner is not a number", "-- pkit: phase=contract\n-- pkit: expand=one", "expand"},
		{"expand marker without a contract", "-- pkit: expand=3", "contract"},
		{"autocommit on a data file", "-- pkit: phase=data\n-- pkit: batch=500\n-- pkit: table=probe\n-- pkit: autocommit=true", "autocommit"},
		{"autocommit without its value", "-- pkit: phase=expand\n-- pkit: autocommit=maybe", "autocommit"},
		{"marker after the SQL", "CREATE TABLE late_marker (x integer);\n-- pkit: phase=expand", "header"},
		{"allow without a reason", "-- pkit: allow=drop-column", "reason"},
		{"empty reason", "-- pkit: allow=drop-column reason=   ", "reason"},
		{"reason without an allow", "-- pkit: reason=because", "allow"},
		{"allow names something that is not a rule", "-- pkit: allow=phase reason=because", "drop-column"},
		// A `reason=` sentence ends at the next pair the grammar has, so the
		// declaration written after it is read rather than swallowed: this file
		// refuses as a contract half with no expansion named, and never as the
		// expand file an unread `phase` would have made it.
		{"a key after the reason is read", "-- pkit: allow=drop-column reason=the release after phase=contract", "expand="},
		{"two reasons on one line", "-- pkit: allow=drop-column reason=one sentence reason=another", "reason="},
	} {
		t.Run(tc.name, func(t *testing.T) {
			migrateURL, _ := dbtest.URLs(t)
			source := db.MigrationSource{Owner: "grammar", Files: fstest.MapFS{
				"000001_grammar.up.sql": {Data: []byte(tc.header + "\n" + body)},
			}}
			err := db.Migrate(t.Context(), migrateURL, source)
			if err == nil {
				t.Fatalf("migrate accepted %q and reported nothing", tc.header)
			}
			if !strings.Contains(err.Error(), "grammar/000001_grammar.up.sql") {
				t.Errorf("refusal did not name the file: %q", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refusal %q does not name %q, which is what the operator has to act on", err, tc.want)
			}
		})
	}
}

// TestStaticRulesRefuseAndAllowMarks: the guard table, one case per rule, and
// beside each the exception marker that rule accepts. The pass cases matter as
// much as the refusals: a rule that refuses the ordinary file it was written to
// protect is a rule the next owner will delete.
func TestStaticRulesRefuseAndAllowMarks(t *testing.T) {
	// The first file creates the table and indexes it in the same file, which is
	// the ordinary shape and must stay ordinary. The second file is the one under
	// test, so "the table existed before this file" is a fact of the case rather
	// than an assumption. Every case gets a schema of its own.
	creates := &fstest.MapFile{Data: []byte(`CREATE TABLE probe (id bigint PRIMARY KEY, a text, b text);
		CREATE INDEX probe_a ON probe (a)`)}
	for _, tc := range []struct {
		name  string
		file  string // the second file, as an owner wrote it wrongly
		rule  string // the rule the refusal must name
		fixed string // the same file a reviewer would accept, or empty
	}{
		{
			name:  "a type change rewrites the table",
			file:  "ALTER TABLE probe ALTER COLUMN a TYPE varchar(64)",
			rule:  "alter-column-type",
			fixed: "-- pkit: allow=alter-column-type reason=a is already text and this only narrows the length\nALTER TABLE probe ALTER COLUMN a TYPE varchar(64)",
		},
		{
			name:  "a not-null column with no default rewrites the table",
			file:  "ALTER TABLE probe ADD COLUMN c integer NOT NULL",
			rule:  "add-column-not-null",
			fixed: "-- pkit: allow=add-column-not-null reason=the table is empty in every installation before this release\nALTER TABLE probe ADD COLUMN c integer NOT NULL",
		},
		{
			name: "a default makes the same statement ordinary",
			file: "ALTER TABLE probe ADD COLUMN c integer NOT NULL DEFAULT 0",
		},
		{
			// The file that creates a table may index it: there is nothing
			// reading the table yet, and this is every module's first file.
			name: "an index in the file that created the table",
		},
		{
			name:  "an index on a table that already existed",
			file:  "CREATE INDEX probe_b ON probe (b)",
			rule:  "index-not-concurrent",
			fixed: "-- pkit: allow=index-not-concurrent reason=the table holds a handful of rows in every installation\nCREATE INDEX probe_b ON probe (b)",
		},
		{
			name:  "dropping a column belongs to the contract half",
			file:  "ALTER TABLE probe DROP COLUMN b",
			rule:  "drop-column",
			fixed: "-- pkit: allow=drop-column reason=no installation ever had this column\nALTER TABLE probe DROP COLUMN b",
		},
		{
			name:  "CONCURRENTLY refuses to enter the migration's transaction",
			file:  "CREATE INDEX CONCURRENTLY IF NOT EXISTS probe_b ON probe (b)",
			rule:  "index-concurrent-without-autocommit",
			fixed: "-- pkit: autocommit=true\nCREATE INDEX CONCURRENTLY IF NOT EXISTS probe_b ON probe (b)",
		},
		{
			name:  "an autocommit file with nothing nontransactional in it",
			file:  "-- pkit: autocommit=true\nCREATE TABLE other (x text)",
			rule:  "autocommit-without-concurrently",
			fixed: "-- pkit: autocommit=true\nCREATE INDEX CONCURRENTLY IF NOT EXISTS probe_b ON probe (b)",
		},
		{
			name:  "an autocommit file must survive being run twice",
			file:  "-- pkit: autocommit=true\nCREATE INDEX CONCURRENTLY probe_b ON probe (b)",
			rule:  "autocommit-not-rerunnable",
			fixed: "-- pkit: autocommit=true\nCREATE INDEX CONCURRENTLY IF NOT EXISTS probe_b ON probe (b)",
		},
		{
			name:  "a data body that ignores the batch would rewrite the table",
			file:  "-- pkit: phase=data\n-- pkit: batch=500\n-- pkit: table=probe\nUPDATE probe SET b = 'x'",
			rule:  "data-body-unbounded",
			fixed: "-- pkit: phase=data\n-- pkit: batch=500\n-- pkit: table=probe\n-- pkit: allow=data-body-unbounded reason=every row takes the same value\nUPDATE probe SET b = 'x'",
		},
		{
			// The two directions of one reading, in one file each. A body whose only
			// window reference is a value it is writing does not read the window, so the
			// window would not bound it and the rule is right about it; and an apostrophe
			// in the commentary above the body is commentary, not the start of a literal
			// that hides the statements after it from every reader. The refusal is what the
			// first file earns; the second one drains.
			name:  "a data body that names the window only inside a value it writes",
			file:  "-- pkit: phase=data\n-- pkit: batch=4\n-- pkit: table=probe\nUPDATE probe SET b = 'where id in (select id from batch)'",
			rule:  "data-body-unbounded",
			fixed: "-- pkit: phase=data\n-- pkit: batch=4\n-- pkit: table=probe\nUPDATE probe SET b = 'x' -- it's the same value for every row\n  WHERE id IN (SELECT id FROM batch)",
		},
		{
			// No exception leg: the reviewer's answer is to split the file, and a
			// data file is one statement whatever the marker says.
			name: "a data file may not change the schema",
			file: "-- pkit: phase=data\n-- pkit: batch=500\n-- pkit: table=probe\nALTER TABLE probe ADD COLUMN c text; UPDATE probe SET c = 'x' WHERE id IN (SELECT id FROM batch)",
			rule: "data-with-ddl",
		},
		{
			// Refused before the connection opens, which is why this case needs no
			// outbox table: the rule is about the text, not the schema.
			name: "a backfill that writes the outbox per row takes the relay",
			file: "-- pkit: phase=data\n-- pkit: batch=500\n-- pkit: table=probe\nINSERT INTO platformkit_outbox (id, tenant_id, name, payload) SELECT id, id, 'x', '{}' WHERE id IN (SELECT id FROM batch)",
			rule: "data-writes-outbox",
		},
		{
			name:  "an exception nobody needed is a lie waiting to rot",
			file:  "-- pkit: allow=drop-column reason=a column nothing references\nALTER TABLE probe ADD COLUMN c text",
			rule:  "unused-allow",
			fixed: "ALTER TABLE probe ADD COLUMN c text",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := func(second string) db.MigrationSource {
				files := fstest.MapFS{"000001_probe.up.sql": creates}
				if second != "" {
					files["000002_rule.up.sql"] = &fstest.MapFile{Data: []byte(second)}
				}
				return db.MigrationSource{Owner: "probe", Files: files}
			}
			err := db.Migrate(t.Context(), migrateURL(t), source(tc.file))
			if tc.rule == "" {
				if err != nil {
					t.Fatalf("the ordinary file was refused: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("%s was accepted:\n%s", tc.rule, tc.file)
			}
			if !strings.Contains(err.Error(), "probe/000002_rule.up.sql") {
				t.Errorf("refusal did not name the file: %q", err)
			}
			if !strings.Contains(err.Error(), tc.rule) {
				t.Errorf("refusal %q does not name the rule %q the reviewer has to read", err, tc.rule)
			}
			if tc.fixed == "" {
				return
			}
			// The same file with the marker written, in the owner's own words, is
			// the owner's decision, and it applies.
			if err := db.Migrate(t.Context(), migrateURL(t), source(tc.fixed)); err != nil {
				t.Errorf("allow= did not except the rule:\n%s\n%v", tc.fixed, err)
			}
		})
	}
}

// migrateURL is a schema of the caller's, so one case can refuse and then apply
// in a database nothing else is looking at.
func migrateURL(t *testing.T) string {
	t.Helper()
	url, _ := dbtest.URLs(t)
	return url
}

// TestContractMigrationWaitsForItsExpand is the release rule that makes
// expand/contract more than a habit: the contract half does not run in the
// release that adds the expand, and the runner refuses it while the expand has
// not already been applied on the installation it is looking at.
//
// "Already applied" is the whole of what the ledger can state. How long ago is
// a release calendar, which belongs to the product.
func TestContractMigrationWaitsForItsExpand(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	owner := "orders"
	// An installation one release behind: version 1 is already applied.
	first := &fstest.MapFile{Data: []byte("CREATE TABLE orders (id bigint PRIMARY KEY, total bigint, currency text)")}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: owner, Files: fstest.MapFS{
		"000001_orders.up.sql": first,
	}}); err != nil {
		t.Fatal(err)
	}

	// The release that adds the new column and, in the same owner, the file that
	// drops the old one. This is the mistake the guard exists for: the drop would
	// run against rows the backfill has not visited yet.
	expand := &fstest.MapFile{Data: []byte("-- pkit: phase=expand\nALTER TABLE orders ADD COLUMN amount_minor bigint")}
	contract := &fstest.MapFile{Data: []byte("-- pkit: phase=contract\n-- pkit: expand=2\nALTER TABLE orders DROP COLUMN total")}
	err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: owner, Files: fstest.MapFS{
		"000001_orders.up.sql": first, "000002_amount.up.sql": expand, "000003_drop_total.up.sql": contract,
	}})
	if err == nil {
		t.Fatal("the contract half applied in the same run as its expand")
	}
	if !strings.Contains(err.Error(), "000003_drop_total.up.sql") || !strings.Contains(err.Error(), "000002") {
		t.Errorf("refusal names neither the contract file nor the expand version it waits for: %q", err)
	}
	admin := dbtest.Open(t, migrateURL)
	var applied int
	scan(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'orders'", &applied)
	if applied != 1 {
		t.Errorf("the refused run left %d history rows, want the 1 that was there before", applied)
	}
	if cols := countRows(t, admin, "SELECT count(*) FROM pg_attribute WHERE attrelid = 'orders'::regclass AND attname = 'amount_minor'"); cols != 0 {
		t.Errorf("the refused run applied part of the release: amount_minor exists")
	}

	// The next release: the expand is now in the history, so the contract half is
	// the only thing pending, and the runner applies it. A guard that only ever
	// refuses is not a guard, it is a refusal to ship.
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: owner, Files: fstest.MapFS{
		"000001_orders.up.sql": first, "000002_amount.up.sql": expand,
	}}); err != nil {
		t.Fatalf("the expand half: %v", err)
	}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: owner, Files: fstest.MapFS{
		"000001_orders.up.sql": first, "000002_amount.up.sql": expand, "000003_drop_total.up.sql": contract,
	}}); err != nil {
		t.Fatalf("the contract half after its expand had applied: %v", err)
	}
	if cols := countRows(t, admin, "SELECT count(*) FROM pg_attribute WHERE attrelid = 'orders'::regclass AND attname = 'total'"); cols != 0 {
		t.Errorf("the contract half never ran: the old column is still there")
	}

	// A fresh installation has no reader and no rows, so the pair applies in
	// order and the gate does not fire. Without this the rule would refuse every
	// module's own test, which migrates from nothing.
	freshURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), freshURL, db.MigrationSource{Owner: owner, Files: fstest.MapFS{
		"000001_orders.up.sql": first, "000002_amount.up.sql": expand, "000003_drop_total.up.sql": contract,
	}}); err != nil {
		t.Fatalf("a fresh installation runs the pair in order: %v", err)
	}
	if n := countRows(t, dbtest.Open(t, freshURL), "SELECT count(*) FROM schema_migrations WHERE owner = 'orders'"); n != 3 {
		t.Errorf("fresh installation applied %d files, want 3", n)
	}
}

// TestDataMigrationRunsInBatchesAndCommitsEachOne is the claim the batch job
// exists for: not "one transaction that is smaller", but a transaction per
// batch. The evidence is xmin — the id of the transaction that wrote a row
// version. Twenty-five rows with a batch of ten in one transaction carry one
// xmin; in three batches they carry three, and a batch that sees its
// predecessor's counts on the table has read work somebody else committed.
//
// This case takes the fresh-owner path: an owner with no applied history drains
// its own data file during the migration, because there is nothing to wait for.
func TestDataMigrationRunsInBatchesAndCommitsEachOne(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "fill", Files: fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(probeTable)},
		"000002_fill.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET done = true, passes = (SELECT count(*) FROM probe p WHERE p.done) / 10 + 1
	WHERE id IN (SELECT id FROM batch)`)},
	}}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	admin := dbtest.Open(t, migrateURL)
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE done"); n != 25 {
		t.Errorf("%d of 25 rows were filled", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM (SELECT passes FROM probe GROUP BY 1) s"); n != 3 {
		t.Errorf("%d distinct batch numbers, want 3: the backfill ran in %d window(s) of 10", n, n)
	}
	if n := countRows(t, admin, "SELECT count(DISTINCT xmin::text) FROM probe"); n < 2 {
		t.Errorf("%d distinct writing transactions filled the table, want one per batch", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'fill' AND version = 2"); n != 1 {
		t.Errorf("the completed data migration recorded %d history rows", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("%d backfill progress rows survived a completed migration", n)
	}
}

// TestDataMigrationResumesWhereTheLastRunStopped is the whole reason progress is
// recorded: the run that dies in the middle of a ten-million-row table comes
// back at the row it died on, and does not repeat the work it committed.
//
// The failure is not simulated. The body divides by the number of rows it has
// already filled less ten, so it succeeds through the first window and raises on
// the second, on the twenty-fifth row of a table of twenty-five, in the same
// place every time.
func TestDataMigrationResumesWhereTheLastRunStopped(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	files := fstest.MapFS{
		"000001_probe.up.sql": {Data: []byte(probeTable)},
		"000002_fill.up.sql": {Data: []byte(`-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET done = true, passes = passes + 1
	WHERE id IN (SELECT id FROM batch) AND 1.0 / ((SELECT count(*) FROM probe WHERE done) - 10) < 0`)},
	}
	err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "fill", Files: files})
	if err == nil {
		t.Fatal("a backfill whose body raised on the second window reported success")
	}
	admin := dbtest.Open(t, migrateURL)
	// The first window committed; the second rolled back with its own error.
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE done"); n != 10 {
		t.Errorf("%d rows were filled before the failure; want the 10 of the committed first batch", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'fill' AND version = 2"); n != 0 {
		t.Errorf("an unfinished backfill wrote the history row; the version is not applied")
	}
	// Progress says where the next run starts. This table is the contract the
	// worker and the runner share, so the test reads it directly.
	var cursor string
	scan(t, admin, "SELECT cursor FROM schema_migration_backfill WHERE owner = 'fill' AND version = 2", &cursor)
	if cursor != "10" {
		t.Errorf("resumable cursor = %q, want the last key of the committed batch, 10", cursor)
	}

	// The correction, shipped the way a correction is shipped: the same version,
	// which was never applied, with a body that works.
	files["000002_fill.up.sql"] = &fstest.MapFile{Data: []byte(`-- pkit: phase=data
-- pkit: batch=10
-- pkit: table=probe
UPDATE probe SET done = true, resumed = true WHERE id IN (SELECT id FROM batch)`)}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "fill", Files: files}); err != nil {
		t.Fatalf("the resumed run: %v", err)
	}
	// Rows 1 to 10 keep the work of the first run and gain none of the second;
	// rows 11 to 25 are the resume. A run that restarted the scan would mark all
	// twenty-five.
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE resumed"); n != 15 {
		t.Errorf("%d rows were touched by the resumed run, want the 15 after the cursor", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE done AND NOT resumed"); n != 10 {
		t.Errorf("the resumed run repeated work the first one had committed")
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE NOT done"); n != 0 {
		t.Errorf("%d rows are still unfilled", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'fill' AND version = 2"); n != 1 {
		t.Errorf("the finished backfill wrote %d history rows", n)
	}
	if n := countRows(t, admin, "SELECT count(*) FROM schema_migration_backfill"); n != 0 {
		t.Errorf("the progress row outlived the migration it belonged to")
	}
}

// TestAFileAfterAnUnfinishedBackfillWaitsForTheBackfill is the ordering rule
// that keeps a release from running ahead of a drain: the contract half, or any
// later file, does not apply while an earlier data migration of the same owner
// is still unfinished. The run stops there, applies nothing past it, and says
// what it is waiting for; the worker drains it and the next migration continues.
//
// The fresh-owner case is the opposite leg and is covered by the two tests
// around it: with no history there is nothing to wait for, and the files apply
// in order.
func TestAFileAfterAnUnfinishedBackfillWaitsForTheBackfill(t *testing.T) {
	migrateURL, _ := dbtest.URLs(t)
	first := &fstest.MapFile{Data: []byte(probeTable)}
	if err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "drain", Files: fstest.MapFS{
		"000001_probe.up.sql": first,
	}}); err != nil {
		t.Fatal(err)
	}
	err := db.Migrate(t.Context(), migrateURL, db.MigrationSource{Owner: "drain", Files: merged(fstest.MapFS{"000001_probe.up.sql": first},
		"000002_fill.up.sql", "-- pkit: phase=data\n-- pkit: batch=10\n-- pkit: table=probe\n"+
			"UPDATE probe SET done = true WHERE id IN (SELECT id FROM batch)",
		"000003_note.up.sql", "ALTER TABLE probe ADD COLUMN note text",
	)})
	if err != nil {
		t.Fatalf("a pending backfill is not a failure of the migration: %v", err)
	}
	admin := dbtest.Open(t, migrateURL)
	var applied int
	scan(t, admin, "SELECT count(*) FROM schema_migrations WHERE owner = 'drain' AND version >= 2", &applied)
	if applied != 0 {
		t.Errorf("%d file(s) after the pending backfill were applied; the drain has not run", applied)
	}
	if cols := countRows(t, admin, "SELECT count(*) FROM pg_attribute WHERE attrelid = 'probe'::regclass AND attname = 'note'"); cols != 0 {
		t.Errorf("the file after the pending backfill changed the table")
	}
	if n := countRows(t, admin, "SELECT count(*) FROM probe WHERE done"); n != 0 {
		t.Errorf("the migration ran the backfill itself on an installation with history; the worker owns that")
	}
}

// merged is a source's files with the named additions, so one case can show the
// runner the same owner with one more release of files.
func merged(files fstest.MapFS, nameSQL ...string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, file := range files {
		out[name] = file
	}
	for i := 0; i+1 < len(nameSQL); i += 2 {
		out[nameSQL[i]] = &fstest.MapFile{Data: []byte(nameSQL[i+1])}
	}
	return out
}

// countRows reads one scalar out of the owner connection.
func countRows(t *testing.T, admin sqlDB, query string) int {
	t.Helper()
	var n int
	scan(t, admin, query, &n)
	return n
}

func quoteLiteral(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
