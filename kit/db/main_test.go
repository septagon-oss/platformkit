package db_test

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// Migration locks belong to a database, not a schema. Give this package its
// own database so its deliberate lock holds and acquisition deadlines measure
// its own sessions, independently of other packages migrating at the same time.
// Individual tests still use dbtest's schemas, grants and application role.
func TestMain(m *testing.M) {
	os.Exit(runInTestDatabase(m))
}

func runInTestDatabase(m *testing.M) (code int) {
	names := []string{"PLATFORMKIT_TEST_ADMIN_URL", "PLATFORMKIT_TEST_DATABASE_URL"}
	for _, name := range names {
		if os.Getenv(name) == "" {
			// Pure tests can run without services. Database tests still fail at
			// dbtest's existing required-environment check; none are skipped.
			return m.Run()
		}
	}

	admin, err := sql.Open("pgx", os.Getenv(names[0]))
	if err != nil {
		fmt.Fprintln(os.Stderr, "kit/db fixture: could not open the owner connection")
		return 1
	}
	defer admin.Close()

	// The name is generated here, never supplied by a caller. Like the browser
	// fixture, this requires the development owner to have CREATE DATABASE.
	database := "platformkit_dbtest_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	setup, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if _, err := admin.ExecContext(setup, "CREATE DATABASE "+database); err != nil {
		fmt.Fprintf(os.Stderr, "kit/db fixture: create database: %v\n", err)
		return 1
	}
	// Registered before any later return, so a run that refuses the URLs below
	// still hands back the database it made.
	defer func() {
		if err := removeTestDatabase(admin, database); err != nil {
			fmt.Fprintf(os.Stderr, "kit/db fixture: drop database: %v\n", err)
			code = 1
		}
	}()

	for _, name := range names {
		target, err := intoDatabase(os.Getenv(name), database)
		if err != nil {
			fmt.Fprintf(os.Stderr, "kit/db fixture: %s must be a PostgreSQL URL: %v\n", name, err)
			return 1
		}
		if err := os.Setenv(name, target); err != nil {
			fmt.Fprintf(os.Stderr, "kit/db fixture: set %s: %v\n", name, err)
			return 1
		}
	}
	return m.Run()
}

// intoDatabase is raw pointed at database — in the path and in the query
// parameter that beats it in pgx, so both spellings of a connection URL name the
// one database this process created and intends to remove.
func intoDatabase(raw, database string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return "", fmt.Errorf("%q is not a PostgreSQL URL", raw)
	}
	parsed.Path = "/" + database
	parsed.RawPath = ""
	query := parsed.Query()
	query.Del("dbname")
	query.Del("database")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

const (
	// The teardown's own budget. Six removals with a two-second look between them
	// is a hundred seconds at the most; the CI Postgres serves four other packages'
	// migrations while this package finishes, and a removal that gives up on one
	// lost lock race reddens a run in which every test passed.
	dropTries  = 6
	dropWindow = 15 * time.Second
)

// terminateSessions ends every session connected to the database it is handed.
// pg_terminate_backend answers true when it ends one and false when it was
// already gone, and it leaves the session running it alone — which is the one the
// DROP below has to stay alive on. Neither answer is read: the question that
// matters is whether the database can be removed after this, which the caller
// asks the server directly.
const terminateSessions = `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()`

// removeTestDatabase drops the database the fixture created.
//
// `DROP DATABASE … WITH (FORCE)` terminates the sessions standing in the database
// on its own, but once, and without waiting for them to notice: the test binary is
// still alive here, so a pool that a test walked away from redials and the removal
// loses the lock race. That is what CI refused on 2026-10-01 — every test in this
// package had passed, and the run was red on the teardown alone. So each removal
// terminates the sessions, asks for the database, and on failure asks who is still
// in there, up to dropTries times. A database that is already gone counts as
// removed: the fixture's job is to leave nothing behind, not to have been the one
// that removed it.
func removeTestDatabase(admin *sql.DB, database string) error {
	var (
		held []string
		err  error
	)
	for attempt := range dropTries {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
		}
		ctx, cancel := context.WithTimeout(context.Background(), dropWindow)
		if _, err = admin.ExecContext(ctx, terminateSessions, database); err != nil {
			held = sessionsIn(admin, database)
			cancel()
			return fmt.Errorf("terminate sessions: %v (%s)", err, strings.Join(held, " | "))
		}
		_, err = admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+database+" WITH (FORCE)")
		cancel()
		if err == nil {
			return nil
		}
		held = sessionsIn(admin, database)
	}
	return fmt.Errorf("%v after %d removals; sessions still connected: %s", err, dropTries, strings.Join(held, " | "))
}

// sessionsIn names up to eight backends still connected to database, so a
// teardown that cannot finish says who is holding it instead of only that it
// stopped. It asks with a context of its own because the removal's deadline is
// usually the thing that just passed.
func sessionsIn(admin *sql.DB, database string) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	rows, err := admin.QueryContext(ctx, `SELECT pid || ' ' || COALESCE(state, 'unknown') || ': ' || left(query, 120)
		FROM pg_stat_activity WHERE datname = $1 LIMIT 8`, database)
	if err != nil {
		return []string{"the session list could not be read: " + err.Error()}
	}
	defer rows.Close()
	var held []string
	for rows.Next() {
		var backend string
		if err := rows.Scan(&backend); err != nil {
			held = append(held, "the session list could not be read: "+err.Error())
			break
		}
		held = append(held, backend)
	}
	if len(held) == 0 {
		return []string{"none"}
	}
	return held
}

// TestTheFixtureRemovesADatabaseASessionIsStandingIn is the CI refusal of
// 2026-10-01 pinned: the package's tests all passed and the run was still red,
// because the teardown asked for the database once while a session still open
// held it, and gave up. A session that is not terminated keeps its database from
// being dropped at all, so a `DROP DATABASE` with no termination in front of it
// fails this case, and so does a teardown that terminates once and never looks
// again.
func TestTheFixtureRemovesADatabaseASessionIsStandingIn(t *testing.T) {
	adminURL := os.Getenv("PLATFORMKIT_TEST_ADMIN_URL")
	if adminURL == "" {
		t.Fatalf("PLATFORMKIT_TEST_ADMIN_URL is unset; start the stack with `make up` and export the test URLs")
	}
	admin := dbtest.Open(t, adminURL)

	scratch := "platformkit_dbtest_pin_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+scratch); err != nil {
		t.Fatalf("create the scratch database: %v", err)
	}
	// Registered after dbtest.Open's, so it runs first: the removal happens while
	// the owner pool is still standing, the way the fixture's own does.
	t.Cleanup(func() {
		if err := removeTestDatabase(admin, scratch); err != nil {
			t.Errorf("the scratch database was left behind: %v", err)
		}
	})

	sessionURL, err := intoDatabase(adminURL, scratch)
	if err != nil {
		t.Fatalf("point a session at the scratch database: %v", err)
	}
	// Never closed, on purpose: this is the session the removal has to end, and a
	// case that closed it first would be dropping an empty database.
	session, err := sql.Open("pgx", sessionURL)
	if err != nil {
		t.Fatalf("open a session into the scratch database: %v", err)
	}
	if err := session.PingContext(t.Context()); err != nil {
		t.Fatalf("the session did not land in the scratch database: %v", err)
	}

	if err := removeTestDatabase(admin, scratch); err != nil {
		t.Fatalf("remove a database a session is standing in: %v", err)
	}
	var left int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM pg_database WHERE datname = $1", scratch).Scan(&left); err != nil {
		t.Fatalf("read the scratch database back: %v", err)
	}
	if left != 0 {
		t.Errorf("the scratch database is still in the cluster")
	}
}
