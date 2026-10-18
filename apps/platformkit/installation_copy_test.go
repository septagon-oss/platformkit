package main

// Every case that takes a copy of the installation this package's TestMain prepared asserts about an
// installed composition without installing one. These two cases are what makes that safe rather than
// optimistic: the first says a copy answers what a real installation answers, the second says a
// case's own writes stay in the database that case was given.

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestACopyOfTheInstalledCompositionAnswersWhatAFreshInstallAnswers puts a fresh install and a copy
// of the prepared installation side by side and asks them the same questions: which files the ledger
// holds with which checksums, how many namespaces the installation is laid out in, which tables there
// are and how many rows each of them carries. Every difference names the thing and both answers.
//
// This is the claim every case that takes a copy rests on: a copy holding one row more or less than
// an installation would let two hundred cases pass while asserting something no installation does.
func TestACopyOfTheInstalledCompositionAnswersWhatAFreshInstallAnswers(t *testing.T) {
	freshPath, fresh := configureFresh(t)
	install(t, freshPath)
	copyPath, copy := configure(t)
	install(t, copyPath)

	freshAdmin := dbtest.Open(t, fresh.Database.MigrateURL)
	copiedAdmin := dbtest.Open(t, copy.Database.MigrateURL)

	for _, question := range []struct {
		what string
		ask  func(*testing.T, *sql.DB) []string
	}{
		{"the ledger", installationLedger},
		{"the tables and their rows", installationTables},
		{"how the installation is laid out", installationNamespaces},
	} {
		if diff := compareAnswers(question.ask(t, freshAdmin), question.ask(t, copiedAdmin)); diff != "" {
			t.Errorf("an installed composition and a copy of it differ in %s:\n%s", question.what, diff)
		}
	}
}

// TestWhatOneCaseWritesIsNowhereInTheNextCasesCopy is the isolation a copy is supposed to add: what
// one case writes lands in a database no other case is ever given. The first subtest writes a marker
// table and a row into its own namespace; the second takes a copy afterwards and finds its own
// composition and no marker.
func TestWhatOneCaseWritesIsNowhereInTheNextCasesCopy(t *testing.T) {
	t.Run("one case writes", func(t *testing.T) {
		path, cfg := configure(t)
		install(t, path)
		owner := dbtest.Open(t, cfg.Database.MigrateURL)
		if _, err := owner.ExecContext(t.Context(), "CREATE TABLE "+marker+" (said text not null)"); err != nil {
			t.Fatalf("write into this case's own database: %v", err)
		}
		if _, err := owner.ExecContext(t.Context(), "INSERT INTO "+marker+" VALUES ('the first case')"); err != nil {
			t.Fatalf("write a row into this case's own database: %v", err)
		}
		if rows := rowsIn(t, owner, marker); rows != 1 {
			t.Errorf("this case's own database holds %d marker rows, want 1", rows)
		}
	})

	t.Run("the next case takes a copy", func(t *testing.T) {
		path, cfg := configure(t)
		install(t, path)
		owner := dbtest.Open(t, cfg.Database.MigrateURL)
		if _, err := owner.ExecContext(t.Context(), "SELECT 1 FROM "+marker); err == nil {
			t.Errorf("a copy taken after another case wrote holds that case's %q table; a case's writes belong to the database it was given", marker)
		}
		if rows := rowsIn(t, owner, "tasks"); rows < 0 {
			t.Errorf("this case's own copy answers for the tasks table: %d rows", rows)
		}
	})
}

// TestACaseThatAsksTwiceIsGivenTwoInstallationsThatNeverMet is the pin beside that. A case may ask
// for an installation more than once — a helper that hands it one per resource it is reading does
// exactly that, five times in one case — and each ask must be an installation of its own: writing in
// the first must not be visible in the second, and the first must still hold what it was written when
// the second is already there. An earlier version of the copy answered the second ask by asking the
// server for the same database twice, which the server refused.
func TestACaseThatAsksTwiceIsGivenTwoInstallationsThatNeverMet(t *testing.T) {
	firstPath, first := configure(t)
	install(t, firstPath)
	firstOwner := dbtest.Open(t, first.Database.MigrateURL)
	if _, err := firstOwner.ExecContext(t.Context(), "CREATE TABLE "+marker+" (said text not null)"); err != nil {
		t.Fatalf("write in this case's first installation: %v", err)
	}

	secondPath, second := configure(t)
	install(t, secondPath)
	secondOwner := dbtest.Open(t, second.Database.MigrateURL)
	if _, err := secondOwner.ExecContext(t.Context(), "SELECT 1 FROM "+marker); err == nil {
		t.Errorf("this case's second installation holds what it wrote in its first; the two asks are one database")
	}
	if rows := rowsIn(t, firstOwner, marker); rows != 0 {
		t.Errorf("this case's first installation lost what it wrote when the case asked again: %d rows, want 0", rows)
	}
	if first.Database.MigrateURL == second.Database.MigrateURL {
		t.Errorf("the two asks were handed the same database: %s", first.Database.MigrateURL)
	}
}

// marker is the table the two isolation cases write, and it is named once so the case that looks for
// it afterwards looks for the same thing it was written as.
const marker = "t0578_marker"

// installationLedger is the migration ledger, one line per applied file, in the order the runner
// reads it.
func installationLedger(t *testing.T, admin *sql.DB) []string {
	t.Helper()
	return listed(t, admin, `SELECT owner || ' ' || version || ' ' || name || ' ' || checksum FROM schema_migrations ORDER BY owner, version`)
}

// installationTables is every base table the installation holds, with how many rows the preparation
// left in it. The namespace is left off the name: each installation lives in a namespace named after
// the case that owns it, so the names of two installations differ whatever their contents.
func installationTables(t *testing.T, admin *sql.DB) []string {
	t.Helper()
	own := current(t, admin, "SELECT current_schema()")
	rows, err := admin.QueryContext(t.Context(), `SELECT table_schema, table_name FROM information_schema.tables
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema') AND table_type = 'BASE TABLE'
		ORDER BY table_schema, table_name`)
	if err != nil {
		t.Fatalf("ask this installation which tables it holds: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var schema, table string
		if err := rows.Scan(&schema, &table); err != nil {
			t.Fatalf("read this installation's tables: %v", err)
		}
		// The namespace an installation was created in is named after the case that owns it, so it
		// is left off: the question is what the installation holds, not who was told to hold it.
		name := identify(table)
		if schema != own {
			name = identify(schema) + "." + name
		}
		var count int64
		if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM "+name).Scan(&count); err != nil {
			t.Fatalf("count the rows of %s: %v", name, err)
		}
		out = append(out, fmt.Sprintf("%s = %d", name, count))
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read this installation's tables: %v", err)
	}
	return out
}

// installationNamespaces counts the namespaces an installation is laid out in. The names themselves
// compare equal only by accident — each one names its own case — so this answers the shape.
func installationNamespaces(t *testing.T, admin *sql.DB) []string {
	t.Helper()
	return listed(t, admin, `SELECT 'namespaces = ' || count(*)::text FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'public')`)
}

// listed is the answer to a query that returns one column of text.
func listed(t *testing.T, admin *sql.DB, query string) []string {
	t.Helper()
	rows, err := admin.QueryContext(t.Context(), query)
	if err != nil {
		t.Fatalf("ask this installation %q: %v", query, err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("read this installation's answer: %v", err)
		}
		out = append(out, line)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read this installation's answer: %v", err)
	}
	return out
}

// rowsIn is how many rows a table holds, read in its own namespace.
func rowsIn(t *testing.T, admin *sql.DB, table string) int64 {
	t.Helper()
	var rows int64
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&rows); err != nil {
		t.Fatalf("count the rows of %s: %v", table, err)
	}
	return rows
}

// identify is an identifier as the server takes it.
func identify(id string) string { return `"` + strings.ReplaceAll(id, `"`, `""`) + `"` }

// current is one answer to one question.
func current(t *testing.T, admin *sql.DB, query string) string {
	t.Helper()
	var answer string
	if err := admin.QueryRowContext(t.Context(), query).Scan(&answer); err != nil {
		t.Fatalf("ask this installation %q: %v", query, err)
	}
	return answer
}

// compareAnswers answers nothing when the two installations agree, and every line one of them holds
// that the other does not when they do not.
func compareAnswers(installed, copied []string) string {
	seen := make(map[string]int, len(copied))
	for _, line := range copied {
		seen[line]++
	}
	var missing []string
	for _, line := range installed {
		if seen[line] == 0 {
			missing = append(missing, "	only the installation holds: "+line)
			continue
		}
		seen[line]--
	}
	for line, extra := range seen {
		for range extra {
			missing = append(missing, "	only the copy holds: "+line)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprint(len(missing), " differences:\n") + strings.Join(missing, "\n")
}
