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

	// The owner URL as the caller gave it, so the teardown below removes its
	// database from outside it: no session drops the database it stands in, and
	// the two URLs after this are rewritten to name the new one.
	adminURL := os.Getenv(names[0])
	admin, err := sql.Open("pgx", adminURL)
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
		if err := removeTestDatabase(adminURL, database); err != nil {
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
	// The teardown's own budget: dropTries removals of dropWindow each, two seconds
	// apart, so no teardown costs a person more than about two and a half minutes.
	// The CI Postgres serves four other packages' migrations while this package
	// finishes, and a removal that gives up on one lost lock race reddens a run in
	// which every test passed — that was 2026-10-01.
	//
	// The window is sized by a second measurement, from the run CI refused on
	// 2026-10-06: `context deadline exceeded after 6 removals; sessions still
	// connected: none` — six timeouts on nobody. Removing a database is not only a
	// lock race: DROP DATABASE asks the cluster for a forced immediate checkpoint
	// and waits for it, which the server logs as `checkpoint starting: immediate
	// force wait`. Measured against this repository's own stack, one empty database
	// came away in 0.46 s, 2.4 s and 5.4 s with four packages' tests running beside
	// it, and the checkpoint the server logged for one of them was `total=2.341 s`
	// of that 2.4 s. On a runner whose Postgres serves four packages through the
	// race detector that wait exceeds fifteen seconds, so a window that small is
	// refused by the removal's own cost — and each abandoned attempt asks for a
	// checkpoint of its own, so the sixth is slower than the first. Three removals
	// with room to finish beat six that cannot finish once.
	dropTries  = 3
	dropWindow = 45 * time.Second

	// dropClearWait is how long one removal waits for the sessions it ended to be
	// gone; dropLockWait, set on the session that asks (see withLockWait), is how
	// long it then waits for a lock it lost. Both are the server's to answer, which
	// is what makes an attempt that loses the race give the lock back instead of
	// holding its place in the queue: a DROP DATABASE the client abandons keeps
	// waiting anyway, and every later attempt queues behind a statement nobody is
	// waiting for any more.
	dropClearWait = 5 * time.Second
	dropLockWait  = 3 * time.Second
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
// Two things stop it, and the removal answers both. A session standing in the
// database holds it: `DROP DATABASE … WITH (FORCE)` terminates those sessions on
// its own, but once, and without waiting for them to notice, so a pool a test
// walked away from redials and the removal loses the lock race — what CI refused
// on 2026-10-01 with every test in the package passed. And the removal itself is
// slow, because DROP DATABASE waits for a forced checkpoint — what CI refused on
// 2026-10-06 with six timeouts and nobody connected, sized by the comment on
// dropWindow above. So each removal terminates the sessions, waits for the server
// to report none left, asks for the database with a lock wait of its own, and on
// failure says which of those refused and who was seen inside. A database that is
// already gone counts as removed: the fixture's job is to leave nothing behind,
// not to have been the one that removed it.
func removeTestDatabase(adminURL, database string) error {
	var (
		held = "nobody was read"
		err  error
	)
	for attempt := range dropTries {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
		}
		if held, err = removeOnce(adminURL, database); err == nil {
			return nil
		}
	}
	return fmt.Errorf("%v after %d removals; sessions still connected: %s", err, dropTries, held)
}

// removeOnce is one removal, on a session of its own which it closes. The session
// is its own for three reasons: the lock wait belongs to the DROP below and to
// nobody who would reuse a pooled connection, the pg_backend_pid() that
// terminateSessions leaves alone is the one asking, and closing the pool takes the
// socket with it, which is the only thing that ends a statement whose deadline has
// passed rather than leaving it in the queue. Each statement gets the window to
// itself, so a session list that is slow to read cannot spend the deadline the
// removal needs.
func removeOnce(adminURL, database string) (held string, err error) {
	dsn, err := withLockWait(adminURL)
	if err != nil {
		return "the owner session could not be opened: " + err.Error(), err
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		return "the owner session could not be opened: " + err.Error(), err
	}
	// One session: the wait set in the URL, the termination and the removal all
	// happen on it, and a second one would be a second session the termination has
	// to leave alone without being the one that asks.
	admin.SetMaxOpenConns(1)
	defer func() { _ = admin.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), dropWindow)
	defer cancel()

	if _, err := admin.ExecContext(ctx, terminateSessions, database); err != nil {
		return heldNow(admin, database), fmt.Errorf("end its sessions: %v", err)
	}
	left, seen, clearErr := waitClear(ctx, admin, database)

	_, err = admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+database+" WITH (FORCE)")
	if err == nil {
		return "", nil
	}
	if held = heldNow(admin, database); clearErr != nil {
		return held, fmt.Errorf("drop it: %v, and before that: %v", err, clearErr)
	}
	if left > 0 {
		return held, fmt.Errorf("drop it: %v, with %d session(s) still in it (%s)", err, left, strings.Join(seen, " | "))
	}
	return held, fmt.Errorf("drop it: %v", err)
}

// withLockWait is rawURL carrying the removal's lock wait for every connection it
// opens. pgx forwards `options` to the startup packet — the same door dbtest sends
// a search_path through — so a session the pool dials again after a lost
// connection waits no longer than the one that opened it. A SET would bind the
// session that ran it and nothing dialled later.
func withLockWait(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("options", strings.TrimSpace(q.Get("options")+fmt.Sprintf(" -clock_timeout=%d", dropLockWait.Milliseconds())))
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// waitClear asks, until dropClearWait, until the server reports no session
// standing in database. Ending a session is a request, not a fact: the backend has
// to notice, and until it does it holds the lock the removal is about to ask for.
func waitClear(ctx context.Context, admin *sql.DB, database string) (int, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, dropClearWait)
	defer cancel()
	for {
		left, seen, err := standingIn(ctx, admin, database)
		if err != nil || left == 0 {
			return left, seen, err
		}
		select {
		case <-ctx.Done():
			return left, seen, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

// standingIn counts the sessions connected to database and names up to eight of
// them, from one read: the count is the window's total and the names its first
// eight rows, so a removal that gives up reports who it saw as well as how many.
func standingIn(ctx context.Context, admin *sql.DB, database string) (int, []string, error) {
	rows, err := admin.QueryContext(ctx, `SELECT count(*) OVER (), pid || ' ' || COALESCE(state, 'unknown') || ': ' || left(query, 120)
		FROM pg_stat_activity WHERE datname = $1 LIMIT 8`, database)
	if err != nil {
		return -1, nil, err
	}
	defer rows.Close()
	var (
		left int
		seen []string
	)
	for rows.Next() {
		var backend string
		if err := rows.Scan(&left, &backend); err != nil {
			return left, seen, err
		}
		seen = append(seen, backend)
	}
	return left, seen, rows.Err()
}

// heldNow names the backends still connected to database for an error message,
// with a deadline of its own: the removal's is usually the thing that just passed,
// and the question is still worth asking after it.
func heldNow(admin *sql.DB, database string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	left, seen, err := standingIn(ctx, admin, database)
	switch {
	case err != nil:
		return "the session list could not be read: " + err.Error()
	case left <= 0:
		return "none"
	case left > len(seen):
		return strings.Join(append(seen, fmt.Sprintf("(%d more)", left-len(seen))), " | ")
	default:
		return strings.Join(seen, " | ")
	}
}

// TestTheFixtureRemovesADatabaseASessionIsStandingIn is the CI refusal of
// 2026-10-01 pinned: the package's tests all passed and the run was still red,
// because the teardown asked for the database once while a session still open
// held it, and gave up. A session that is not terminated keeps its database from
// being dropped at all, so a `DROP DATABASE` with no termination in front of it
// fails this case, and so does a teardown that terminates once and never looks
// again.
//
// It is also the case the 2026-10-06 refusal came out of: the run was red on this
// teardown again, six times over, with no session connected at all — see
// dropWindow for what the removal actually spends its time on. The assertions here
// are the ones that refused then; what changed is that the removal has room to
// finish, and says which part of itself could not.
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
		if err := removeTestDatabase(adminURL, scratch); err != nil {
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

	if err := removeTestDatabase(adminURL, scratch); err != nil {
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
