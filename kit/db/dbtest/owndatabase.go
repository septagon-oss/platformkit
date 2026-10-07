package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// RunInOwnDatabase gives one package its own PostgreSQL database for the length of its run, and
// is a package's TestMain: `func TestMain(m *testing.M) { os.Exit(dbtest.RunInOwnDatabase(m)) }`.
//
// The reason is the queue, not the data. A migration run holds one advisory lock for its whole
// composition (kit/db's compositionLockKey), an advisory lock belongs to a database rather than a
// schema, and it cannot be narrowed to one namespace: a module's schema is created as
// `CREATE SCHEMA IF NOT EXISTS <owner>`, named after the module and so shared by every
// installation in that database whatever search_path each arrived under. Every package that
// migrates therefore queues at the same door. `go test ./...` runs one process per package and all
// of them against the database the test URLs name, so a package whose every case installs an
// application — apps/platformkit, one migration per case — spends its wall clock behind the other
// packages' migrations rather than doing its own, and on a loaded machine that is where its stated
// bound catches it. One database per package divides that queue by the packages that ask, and costs
// a package nothing it was not already paying: each of its cases was migrating into its own schema
// all along, and a schema inside a private database is the same work with nothing to wait for.
//
// The URLs after this name the new database in the path, which is where pgx reads it; a query that
// names a database of its own is refused rather than guessed at, because two spellings of one
// name are a question this fixture cannot answer for its caller. The roles the tests connect as are
// cluster-wide, and dbtest's URLs grant what a test's schema needs inside it, so a fresh database
// carries nothing the run needs and inherits nothing it does not ask for.
//
// A caller with no test URLs at all is left alone: `m.Run()` on the environment it was given, which
// is what a checkout with no stack runs, and whose database cases then fail at URLs rather than
// skipping.
func RunInOwnDatabase(m *testing.M) (code int) {
	names := []string{"PLATFORMKIT_TEST_ADMIN_URL", "PLATFORMKIT_TEST_DATABASE_URL"}
	if os.Getenv(names[0]) == "" || os.Getenv(names[1]) == "" {
		return m.Run()
	}
	adminURL := os.Getenv(names[0])
	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dbtest: own database: the owner URL cannot be opened")
		return 1
	}
	defer admin.Close()

	// The name is generated here and carries this process's own run id, so two
	// packages started at once cannot name the same database, and a person reading
	// psql can see which one is whose.
	database := "platformkit_test_" + run
	setup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(setup, "CREATE DATABASE "+quote(database)); err != nil {
		fmt.Fprintln(os.Stderr, "dbtest: own database: the owner cannot create one; `make up` grants it CREATE DATABASE")
		return 1
	}
	// Registered before anything that can fail below, so a run that refuses the URLs still hands
	// back the database it made.
	defer func() {
		if err := removeOwnDatabase(adminURL, database); err != nil {
			fmt.Fprintln(os.Stderr, "dbtest: own database: the database is still there:", err)
			code = 1
		}
	}()

	for _, name := range names {
		target, err := intoDatabase(os.Getenv(name), database)
		if err != nil {
			fmt.Fprintln(os.Stderr, "dbtest: own database:", err)
			return 1
		}
		if err := os.Setenv(name, target); err != nil {
			fmt.Fprintln(os.Stderr, "dbtest: own database: set", name+":", err)
			return 1
		}
	}
	return m.Run()
}

// intoDatabase is raw pointed at database — in the path, which is where pgx reads a connection
// URL's database from. Nothing else about the URL is rewritten: query pairs are left exactly as
// their author wrote them, because re-encoding one would change what the server is told (a `+`
// that pgx reads as a plus sign becomes a space under url.Values.Encode) and this fixture has no
// business saying anything about a GUC it did not set.
func intoDatabase(raw, database string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("%s is not a URL: %w", raw, err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", fmt.Errorf("%q is not a PostgreSQL URL", raw)
	}
	if _, found := queryValue(parsed.RawQuery, "dbname"); found {
		return "", fmt.Errorf("%s names its database in the query as well as the path; remove one", raw)
	}
	parsed.Path = "/" + database
	parsed.RawPath = ""
	return parsed.String(), nil
}

// queryValue is the value a raw query carries for key, without decoding anything: this is asked to
// notice a pair, not to read it.
func queryValue(rawQuery, key string) (string, bool) {
	for _, pair := range strings.Split(rawQuery, "&") {
		if name, value, found := strings.Cut(pair, "="); found && name == key {
			return value, true
		}
	}
	return "", false
}

// The removal's own patience, and it is small on purpose: only this package ever connected to the
// database it made, so what can hold it is a pool this package's own tests walked away from.
// pg_terminate_backend ends those; the FORCE below ends any that arrive between the two statements.
// One attempt gets removeTry, and removeTries of them are made, because DROP DATABASE asks the
// cluster for a forced immediate checkpoint and waits for it, which is server work rather than a
// lock race and can take seconds when the server is busy.
const (
	removeTries = 3
	removeTry   = 30 * time.Second
)

func removeOwnDatabase(adminURL, database string) error {
	admin, err := sql.Open("pgx", adminURL)
	if err != nil {
		return err
	}
	defer admin.Close()
	var last error
	for range removeTries {
		ctx, cancel := context.WithTimeout(context.Background(), removeTry)
		_, termErr := admin.ExecContext(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", database)
		_, last = admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quote(database)+" WITH (FORCE)")
		cancel()
		if last == nil {
			return termErr
		}
	}
	return last
}
