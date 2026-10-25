// Package dbtest gives one test its own Postgres schema.
//
// It lives outside kit/db so that kit/db exports no DDL door: an application
// connection can only reach the database through db.Run or db.RunSystem, and
// the owner connection a test needs for CREATE TABLE is a plain *sql.DB opened
// here. Nothing in kit/ imports this package; only tests do.
//
// The schema travels in the connection URL, as options=-csearch_path=<schema>,
// so every connection a pool opens from it lands in the same place, and so does
// everything db.Migrate creates: the ledger and the tenancy helper functions are
// private to the test rather than shared through public.
package dbtest

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver "pgx"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/internal/syscap"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// URLs creates the schema and returns the two connection URLs that reach it:
// the first owns it and holds the DDL rights, the second is the unprivileged
// role row-level security binds. The schema is dropped when the test ends.
//
// It fails rather than skips when the environment names no database. A suite
// that quietly skips the isolation tests proves nothing.
func URLs(t *testing.T) (adminURL, appURL string) {
	t.Helper()
	return URLsFor(t)
}

// URLsFor is URLs for tests and benchmarks, with the same automatic cleanup.
func URLsFor(t testing.TB) (adminURL, appURL string) {
	t.Helper()
	ns, err := createNamespace(t.Context(), schemaName(t.Name()))
	if ns != nil {
		t.Cleanup(func() {
			if err := ns.close(t.Context()); err != nil {
				t.Errorf("dbtest: cleanup: %v", err)
			}
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	return ns.adminURL, ns.appURL
}

// namespaceStatements are the statements that make one schema this namespace's own: an empty
// schema of that name, the application role's USAGE on it, and the default privileges that hand
// every table and sequence created inside it to that role as it appears, the way
// deploy/postgres/init.sql does for the public schema. A schema and a role are quoted here and
// nowhere else, so a caller cannot prepare one namespace two ways.
func namespaceStatements(schema, role string) []string {
	s, r := quote(schema), quote(role)
	return []string{
		"DROP SCHEMA IF EXISTS " + s + " CASCADE",
		"CREATE SCHEMA " + s,
		"GRANT USAGE ON SCHEMA " + s + " TO " + r,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA " + s + " GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO " + r,
		"ALTER DEFAULT PRIVILEGES IN SCHEMA " + s + " GRANT USAGE, SELECT ON SEQUENCES TO " + r,
	}
}

// Schema creates an isolated database schema with the kernel's tables and the
// named capabilities — a module's exported Migrations source, such as
// task.Migrations, and the sources of the modules it depends on (auth needs
// user). It returns an owner handle for assertions and an application
// connection subject to row-level security. A test that names no capability
// gets the tenancy, outbox and limits tables and nothing else.
func Schema(t *testing.T, extra ...db.MigrationSource) (admin *sql.DB, app *db.Conn) {
	t.Helper()
	f, err := openSchema(t.Context(), schemaName(t.Name()), t.Logf, extra...)
	t.Cleanup(func() {
		if err := f.close(t.Context()); err != nil {
			t.Errorf("dbtest: cleanup: %v", err)
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	return f.admin, f.app
}

// migrateTries and migratePause are this fixture's patience with a migration that
// was refused rather than one that failed: see migrate.
const (
	migrateTries = 4
	migratePause = time.Second
)

// migrate reports bounded contention retries through the test's log.
func migrate(t testing.TB, ctx context.Context, adminURL string, sources ...db.MigrationSource) error {
	t.Helper()
	return migrateWithLog(ctx, adminURL, t.Logf, sources...)
}

func migrateWithLog(ctx context.Context, adminURL string, logf func(string, ...any), sources ...db.MigrationSource) error {
	try := 0
	return pastContention(ctx, migrateTries, migratePause, func(ctx context.Context) error {
		try++
		if try > 1 && logf != nil {
			logf("dbtest: the migration was refused by a lock, running it again: attempt %d of %d", try, migrateTries)
		}
		return db.MigrateWithOptions(ctx, adminURL, db.MigrateOptions{}, sources...)
	})
}

// pastContention runs attempt until it answers something other than db.ErrContended,
// the tries run out, or ctx ends. Anything else the attempt answers — success, or a
// migration that really failed — is returned at once and untouched.
func pastContention(ctx context.Context, tries int, pause time.Duration, attempt func(context.Context) error) error {
	var err error
	for try := 1; try <= tries; try++ {
		if err = attempt(ctx); err == nil || !errors.Is(err, db.ErrContended) {
			return err
		}
		if try == tries {
			break
		}
		select {
		case <-ctx.Done():
			return err
		case <-time.After(pause):
		}
	}
	return err
}

// DeploymentSchema is the namespace an unqualified statement in this session
// lands in, read back through an open owner connection: under the URLs this
// package returns that is the schema it created for t, which is what public is
// to an installation — the namespace the kernel's SQL lands in, and the one
// whose default privileges the deployment pinned.
//
// It is exported because the name is this package's own fact and every caller
// that opens a module's schema needs the namespace it is opening beside: a test
// that derives a module-owner name from it, or that compares a module schema's
// privileges against the deployment's answer, otherwise re-derives it by hand.
// Such a caller cuts a schema name out of this answer, and then role names out
// of that, so the answer is one namespace rather than a list of them.
//
// current_schema() is what makes that true. It is the namespace the server
// resolved the session's path to, where current_setting('search_path') is the
// raw setting the URL asked for — and a setting is a list: the default is
// "$user", public, so an installation whose pins live in public resolves to
// public whatever its path names first. A caller handed the setting would write
// a comma into the middle of an identifier. Nothing else is checked, because the
// server's answer is a schema name by construction and this package makes the
// schema its URLs name; the failure a caller can actually reach is the one no
// prefix check could see, and it is stated below rather than left to whatever
// the driver says about a NULL.
//
// The name comes from the server rather than from the connection URL for the
// same reason the runner reads a file's checksum rather than trusting its name:
// the answer has to be what the database resolved, not what this package meant.
func DeploymentSchema(t testing.TB, admin *sql.DB) string {
	t.Helper()
	var schema sql.NullString
	if err := admin.QueryRowContext(t.Context(), "SELECT current_schema()").Scan(&schema); err != nil {
		t.Fatalf("dbtest: read the schema this connection lands in: %v", err)
	}
	if !schema.Valid {
		t.Fatal("dbtest: this session resolves to no schema at all, which is what a search_path naming " +
			"no schema this role may use answers; every URL this package returns names the schema it made")
	}
	return schema.String
}

// Open returns an owner pool on rawURL with four open connections and one idle
// connection, closed when the test ends. An invalid URL fails the test.
func Open(t *testing.T, rawURL string) *sql.DB {
	t.Helper()
	return OpenFor(t, rawURL)
}

// OpenFor is Open for tests and benchmarks, with the same automatic cleanup.
func OpenFor(t testing.TB, rawURL string) *sql.DB {
	t.Helper()
	pool, err := openOwner(rawURL)
	if err != nil {
		t.Fatalf("dbtest: open %q: %v", rawURL, err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return pool
}

// openOwner bounds assertion, setup and cleanup pools alike. Four connections
// permit a held lock, a concurrent writer and an observer with one slot to spare.
func openOwner(rawURL string) (*sql.DB, error) {
	pool, err := sql.Open("pgx", rawURL)
	if err != nil {
		return nil, err
	}
	pool.SetMaxOpenConns(4)
	pool.SetMaxIdleConns(1)
	return pool, nil
}

// Role creates a login role that owns nothing and holds nothing except what the
// caller grants it, in the installation the given owner connection stands in, and
// returns its name and a DSN that lands in that installation — the same database and the same
// namespace the handle reached.
//
// The name is the namespace's with this fixture's prefix on it, and a namespace too long for one
// PostgreSQL identifier keeps its head — as much of it as holds whole characters — and gains a
// digest of the whole name rather than losing its tail: two installations whose names differ only
// past the limit — the copies of one prepared installation differ only in how many times their case
// asked — take two roles, and each grant lands on the installation it was written for. The
// namespace comes back through the owner connection, so that name is built from whatever the caller
// called its installation, not only from the names this fixture makes for itself.
//
// The grant is aimed at the database the connection stands in, read from the server, rather than at
// the database the environment names, because a test may hold a copy of an installation its package
// prepared (Clone) rather than the one the URLs name. A privilege lives in the database it was
// granted in, and PostgreSQL refuses to drop a role that still holds one anywhere: a fixture that
// cleaned up through the environment's own database would revoke nothing, lose the role, and say
// nothing about either.
//
// It exists because the tree has two roles and some rights belong to neither: the
// owner is a superuser that row-level security does not bind, and the application
// role must hold INSERT or it cannot write at all. A boundary that fences an
// expiry away from the role that appends (modules/audit/migrations/000048) therefore
// cannot be tested from either handle — the second tenant's rows would be visible
// from the first's session as the owner, and invisible as the app role, and both
// answers would be about the wrong role. Each grants argument is the privilege list
// and the object, spelled the way GRANT spells it, so the test says what the
// deployment would have to grant and nothing else.
//
// The password is the one the test cluster already uses, read from
// PLATFORMKIT_TEST_DATABASE_URL rather than invented: the role is a fixture in a
// disposable schema, dropped with it, and a second secret in the environment would
// be a second thing to rotate for no gain.
//
// A cleanup this fixture cannot finish is reported rather than swallowed: the role is cluster-wide,
// so one left behind by a package outlives the database that held it and stands in the way of the
// next run's grant of the same name.
func Role(t *testing.T, admin *sql.DB, grants ...string) (name, dsn string) {
	t.Helper()
	baseApp := mustEnv(t, "PLATFORMKIT_TEST_DATABASE_URL")
	baseAdmin := mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL")
	schema, database := DeploymentSchema(t, admin), sessionDatabase(t, admin)
	schemaSQL, appPassword := quote(schema), ""
	if parsed, err := url.Parse(baseApp); err == nil && parsed.User != nil {
		appPassword, _ = parsed.User.Password()
	}
	name = identifier(rolePrefix + strings.TrimPrefix(schema, "t_"))
	must := func(query string) {
		t.Helper()
		if _, err := admin.ExecContext(t.Context(), query); err != nil {
			t.Fatalf("dbtest: %s: %v", query, err)
		}
	}
	// A password is a string literal, not an identifier: double the quotes and
	// single-quote it, which is what the standard says and what CREATE ROLE takes.
	literal := "'" + strings.ReplaceAll(appPassword, "'", "''") + "'"
	must("CREATE ROLE " + quote(name) + " LOGIN PASSWORD " + literal)
	must("GRANT USAGE ON SCHEMA " + schemaSQL + " TO " + quote(name))
	for _, g := range grants {
		must("GRANT " + g + " TO " + quote(name))
	}
	grantedIn := aimedAt(t, baseAdmin, database)
	t.Cleanup(func() {
		cleaner, err := openOwner(grantedIn)
		if err != nil {
			return
		}
		defer cleaner.Close()
		ctx := context.WithoutCancel(t.Context())
		if _, err := cleaner.ExecContext(ctx, "DROP OWNED BY "+quote(name)); err != nil {
			t.Errorf("dbtest: revoke what the role %s holds in %s: %v", name, database, err)
		}
		if _, err := cleaner.ExecContext(ctx, "DROP ROLE IF EXISTS "+quote(name)); err != nil {
			t.Errorf("dbtest: the role %s is still there: %v", name, err)
		}
	})
	return name, withSchema(t, swapUser(t, aimedAt(t, baseApp, database), name), schema)
}

// sessionDatabase is the database an open owner connection stands in, read back through it.
//
// It comes from the server for the reason DeploymentSchema reads the namespace from it: a fixture
// that asks the environment instead names the database the process started on, which is not the one
// a caller handed a handle opened against a copy of a prepared installation. Both are needed
// together — a privilege belongs to a database and a namespace — and only the server holds the pair
// this connection actually reached.
func sessionDatabase(t testing.TB, admin *sql.DB) string {
	t.Helper()
	var database string
	if err := admin.QueryRowContext(t.Context(), "SELECT current_database()").Scan(&database); err != nil {
		t.Fatalf("dbtest: read the database this connection lands in: %v", err)
	}
	return database
}

// swapUser returns rawURL with its login role replaced, password kept.
func swapUser(t testing.TB, rawURL, role string) string {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("dbtest: %q is not a URL: %v", rawURL, err)
	}
	if u.User == nil {
		t.Fatalf("dbtest: %q names no role", rawURL)
	}
	pw, _ := u.User.Password()
	u.User = url.UserPassword(role, pw)
	return u.String()
}

// RoleOf is the login role of a connection URL, so a test names the same role
// the app connects as instead of hard-coding it a second time.
func RoleOf(t *testing.T, rawURL string) string {
	t.Helper()
	return roleOf(t, rawURL)
}

func roleOf(t testing.TB, rawURL string) string {
	t.Helper()
	name, err := loginRole(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return name
}

// loginRole is roleOf's contract with no testing.TB to report to: the role a connection URL logs in
// as, which is the role a namespace's grants and default privileges name.
func loginRole(rawURL string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("%q is not a URL: %w", rawURL, err)
	}
	if u.User == nil || u.User.Username() == "" {
		return "", fmt.Errorf("%q names no role", rawURL)
	}
	return u.User.Username(), nil
}

// aimedAt is intoDatabase with a testing.TB to report a URL it cannot aim at a database.
func aimedAt(t testing.TB, rawURL, database string) string {
	t.Helper()
	target, err := intoDatabase(rawURL, database)
	if err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	return target
}

// withSchema returns rawURL with the test schema as the only search_path. pgx
// forwards `options` to the startup packet, so it applies to every connection a
// pool opens from the URL, including the ones kit/app and db.Migrate open for
// themselves. A statement would bind one session instead, which is why the
// schema travels in the URL and not in a SET.
//
// The application_name goes with it, and it is the same string. Tests share one
// database, so a test that asks pg_stat_activity a question — is anything of
// mine idle in a transaction right now? — is otherwise asking about every
// package running beside it, and answers whatever they happen to be doing. With
// this, `application_name = current_setting('search_path')` on the owner
// connection names exactly this test's own backends, and nobody else's.
func withSchema(t testing.TB, rawURL, schema string) string {
	t.Helper()
	target, err := schemaURL(rawURL, schema)
	if err != nil {
		t.Fatalf("dbtest: %q is not a URL: %v", rawURL, err)
	}
	return target
}

// schemaURL is withSchema's contract with no testing.TB to report to: the namespace a pool opened
// from the URL lands in, and the application_name that says so in pg_stat_activity.
func schemaURL(rawURL, schema string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("options", "-csearch_path="+schema)
	q.Set("application_name", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func mustEnv(t testing.TB, name string) string {
	t.Helper()
	v, err := requiredURL(name)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// run makes a schema name unique to this test binary.
//
// `go test ./...` runs one process per package, in parallel, and two packages
// with a test of the same name — a conformance suite is exactly that — would
// otherwise create and drop the same schema underneath each other. The id is
// per process rather than per test so that a name stays readable in psql while
// somebody is looking at it.
var run = strconv.FormatUint(rand.Uint64(), 36)

// schemaName turns a test name into a Postgres identifier.
func schemaName(test string) string {
	name := "t_" + run + "_" + sanitize(test)
	if len(name) > identifierLimit { // the Postgres identifier limit
		name = name[:identifierLimit]
	}
	return name
}

// identifierLimit is how many bytes one PostgreSQL identifier may hold: NAMEDATALEN minus one. The
// server shortens anything longer to this without saying so, which is why a name built out of
// another name is shortened here, where the reason can be written down.
const identifierLimit = 63

// rolePrefix is what a login role this fixture creates is named after the namespace it holds rights
// in: prefix the namespace and you have the role, which is what lets a person reading pg_roles and
// pg_stat_activity match a session to its installation without asking. Clone leaves this much room
// in a namespace of its own for the same reason: a namespace that fills the identifier limit would
// leave the role named after it nothing.
const rolePrefix = "r_"

// digestWidth is the characters a digest answers in. It is a constant because the two places that
// reserve room for one inside an identifier do so before they know the name.
const digestWidth = 8

// digestOf is fnv-1a of a name, as the eight hex digits an identifier can carry beside a cut.
func digestOf(name string) string {
	digest := fnv.New32a()
	_, _ = digest.Write([]byte(name))
	return fmt.Sprintf("%0*x", digestWidth, digest.Sum32())
}

// identifier is a name the server will hold as one identifier: within the limit it comes back as it
// was given, and a longer one keeps its head and ends in a digest of the whole name. The head stops
// at a whole character, because the limit counts bytes and an identifier may hold characters that
// take several of them: a cut inside one leaves a partial character the server refuses back, so
// CREATE ROLE answers `invalid byte sequence for encoding "UTF8"` for a namespace this fixture
// created itself and read back in full. What the shortened head cannot spell, the digest answers.
//
// It is for a name built out of another name, where the cut lands where the caller has no more
// room: Role derives a login role's name from the namespace it holds rights in, so two namespaces
// that differ only past the limit — the copies of one prepared installation, which differ only in
// how many times their case asked — would arrive at the cluster as one role name. The second CREATE
// ROLE is then refused as already existing, and its grants would have landed on the installation
// that got there first. Cutting at the limit instead is what did that; PostgreSQL shortens in
// silence, so nothing else said so. The digest is over the whole name, so one name answers one
// identifier every time: a caller that has to spell its role out in a GRANT derives it twice.
func identifier(name string) string {
	if len(name) <= identifierLimit {
		return name
	}
	tail := "_" + digestOf(name)
	head := identifierLimit - len(tail)
	// A byte the cut lands on that continues the character before it means the cut is inside it.
	for head > 0 && !utf8.RuneStart(name[head]) {
		head--
	}
	return name[:head] + tail
}

// sanitize is a test name as a Postgres identifier can hold it: lower case, and every character the
// grammar does not admit becomes one underscore, so what comes back holds one byte per character and
// a caller may shorten it by bytes. Nothing truncates it here, so a caller with room keeps the name.
func sanitize(test string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(test) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func quote(id string) string { return `"` + strings.ReplaceAll(id, `"`, `""`) + `"` }

// systemToken is the capability a test needs to write the control plane.
//
// It is minted here rather than in a module's test because kit/internal/syscap
// is closed to everything outside kit/, which is the point: a module cannot
// give itself cross-tenant access, and neither can a module's test. This
// package is already the place where a test is handed what the application
// deliberately withholds — the owner connection, the DDL rights — so it is the
// place for this too.
var systemToken = syscap.NewSystemToken("a test writing across tenants")

// SystemToken is that capability, for a test of something the application hands
// one to: a module's control-plane routes take api.SystemToken(), and a job
// that has to cross tenants takes it in Module.Routes, so a test of either has
// to be able to stand where the kernel stands.
func SystemToken() tenancy.SystemToken { return systemToken }

// System runs fn in a cross-tenant transaction, for a test that has to reach
// the tables no tenant owns: the tenant registry, and the outbox as the relay
// sees it.
func System(ctx context.Context, conn *db.Conn, fn func(context.Context, db.Tx[db.System]) error) error {
	return db.RunSystem(ctx, conn, systemToken, fn)
}

// Hold runs stmt on a session of its own, inside one transaction, and keeps
// whatever lock that statement took until the caller releases it or the test
// ends — whichever comes first. It answers once the lock is held, because the
// statement that took it has already returned.
//
// This is how a test slows a store on purpose. The alternative, a Go timer in the
// test's own goroutine, waits for a sleep rather than for a queue, and the
// behaviour worth testing — a statement that cannot get the lock it asked for
// because somebody else is holding it — is exactly what a loaded machine does by
// itself. A caller that wants a row rather than a table takes it by name, with
// UPDATE … WHERE key = $1; one that wants every statement to wait locks the table
// instead, since a plain read never queues behind a row lock.
//
// The transaction is rolled back rather than committed, so the holder leaves no
// mark on what the test reads afterwards, and the sleep that holds the lock is
// cut short when the test ends rather than running out its bound.
func Hold(t testing.TB, admin *sql.DB, hold time.Duration, stmt string, args ...any) (release func()) {
	t.Helper()
	ctx := context.WithoutCancel(t.Context())
	session, err := admin.Conn(ctx)
	if err != nil {
		t.Fatalf("dbtest: open a session to hold %q: %v", stmt, err)
	}
	if _, err := session.ExecContext(ctx, "BEGIN"); err != nil {
		session.Close()
		t.Fatalf("dbtest: begin the holder: %v", err)
	}
	if _, err := session.ExecContext(ctx, stmt, args...); err != nil {
		session.Close()
		t.Fatalf("dbtest: hold %q: %v", stmt, err)
	}
	released := make(chan struct{})
	sleeping, stop := context.WithCancel(ctx)
	go func() {
		defer close(released)
		defer session.Close()
		_, _ = session.ExecContext(sleeping, "SELECT pg_sleep($1)", hold.Seconds())
		// Roll back whether the sleep ran out or was cut short: a transaction whose
		// statement was interrupted is aborted, and an aborted transaction is still
		// holding the lock somebody else is waiting for.
		_, _ = session.ExecContext(context.WithoutCancel(sleeping), "ROLLBACK")
	}()
	var once sync.Once
	release = func() { once.Do(func() { stop(); <-released }) }
	t.Cleanup(release)
	return release
}
