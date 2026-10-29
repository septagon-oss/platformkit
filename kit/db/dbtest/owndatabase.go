package dbtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

// RunInOwnDatabase gives one package its own PostgreSQL database for the length of
// its run, and is a package's TestMain:
// `func TestMain(m *testing.M) { os.Exit(dbtest.RunInOwnDatabase(m)) }`.
//
// The reason is the queue, not the data. A migration run holds one advisory lock for
// its whole composition (kit/db's compositionLockKey), an advisory lock belongs to a
// database rather than a schema, and a module's schema is shared by every installation
// in that database: every package that migrates queues at the same door. One database
// per package divides that queue and costs nothing it was already paying.
//
// The URLs after this name the new database in the path, where pgx reads it, and a
// query naming a database of its own is refused rather than guessed at.
func RunInOwnDatabase(m *testing.M) (code int) {
	return runInOwnDatabase(m, nil)
}

// RunInOwnDatabaseWithTemplate is RunInOwnDatabase plus one prepared installation
// that the package's cases may each copy, and is a package's TestMain:
// `func TestMain(m *testing.M) { os.Exit(dbtest.RunInOwnDatabaseWithTemplate(m, prepare)) }`.
//
// It answers the half of the queue RunInOwnDatabase leaves: a package whose every case
// installs the same composition pays that migration, and its advisory lock, once per
// case. Here `prepare` installs it once into a database of its own, and a case asks
// for Clone, so Postgres copies the prepared files rather than replaying.
//
// `prepare` is handed the template's owner and application URLs and returns an error
// that fails the package before any case runs. When it returns, every connection this
// fixture opened is closed: a session left in the template breaks a later clone.
func RunInOwnDatabaseWithTemplate(m *testing.M, prepare func(adminURL, appURL string) error) (code int) {
	return runInOwnDatabase(m, prepare)
}

func runInOwnDatabase(m *testing.M, prepare func(adminURL, appURL string) error) (code int) {
	names := []string{"PLATFORMKIT_TEST_ADMIN_URL", "PLATFORMKIT_TEST_DATABASE_URL"}
	if os.Getenv(names[0]) == "" || os.Getenv(names[1]) == "" {
		return m.Run()
	}
	adminURL := os.Getenv(names[0])
	admin, err := openOwner(adminURL)
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
	if prepare != nil {
		// The name comes back whether the preparation finished or not: a refusal below the CREATE
		// left a database of this process's own holding a half-written installation, and the package
		// takes it back before it reports why it stopped.
		prepared, err := prepareTemplate(database, prepare)
		if prepared != "" {
			defer func() {
				if err := removeOwnDatabase(adminURL, prepared); err != nil {
					fmt.Fprintln(os.Stderr, "dbtest: template: the database is still there:", err)
					code = 1
				}
			}()
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "dbtest: own database: the template is not prepared:", err)
			return 1
		}
	}
	return m.Run()
}

// templateSchema is the namespace the prepared installation lives in, and it is the same string
// for every clone until that clone renames it: the name is inside the copied files, so a name a
// case derived from the copy would be one name for every case in the package.
const templateSchema = "t_template"

// templatePrepareBudget is how long the one installation a package prepares may take. It is a
// fixture's patience about a run that used to be repeated once per case, and it is generous
// because the package waits for it once whatever its size: the same work at one case's own
// patience, times the number of cases, is the wall clock this primitive exists to stop.
const templatePrepareBudget = 10 * time.Minute

// prepareTemplate makes the template database and hands `prepare` the URLs of the namespace this
// fixture creates in it. The caller removes the database when the package ends; a package that
// prepares one and never clones it loses the one CREATE DATABASE it costs.
//
// The name is answered beside a failure as well as beside a success, because a refused preparation
// left a database behind: every step below the CREATE can fail with the database standing, and the
// caller that takes it back is the one that also reports why it stopped. A caller handed no name
// would have to re-derive one to clean up, and two spellings of a name are a question this fixture
// answers for its caller rather than asks it.
func prepareTemplate(packageDatabase string, prepare func(adminURL, appURL string) error) (prepared string, err error) {
	database := packageDatabase + "_template"
	adminURL := os.Getenv("PLATFORMKIT_TEST_ADMIN_URL")
	appURL := os.Getenv("PLATFORMKIT_TEST_DATABASE_URL")
	admin, err := openOwner(adminURL)
	if err != nil {
		return "", err
	}
	defer admin.Close()
	ctx, cancel := context.WithTimeout(context.Background(), templatePrepareBudget)
	defer cancel()
	// A name a killed run left behind is taken back first: a template prepared onto a database some
	// other process still holds would be a copy of somebody else's installation. Nothing was created
	// here, so a failure answers no name: the caller leaves a database it cannot remove to the
	// sentence it prints rather than asking the cluster for it a second time.
	if err := removeOwnDatabase(adminURL, database); err != nil {
		return "", fmt.Errorf("the %s left by an earlier run is still there: %w", database, err)
	}
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+quote(database)); err != nil {
		return "", fmt.Errorf("create %s: %w", database, err)
	}
	// From here the database stands whatever the preparation answers for it, and the caller takes it
	// back either way.
	prepared = database
	// The namespace and its grants are the ones URLsFor hands a case, so `prepare` installs into
	// the namespace a case would have installed into itself.
	role, err := loginRole(appURL)
	if err != nil {
		return prepared, err
	}
	ownerURL, err := databaseURL(adminURL, database, templateSchema)
	if err != nil {
		return prepared, err
	}
	pool, err := openOwner(ownerURL)
	if err != nil {
		return prepared, err
	}
	defer pool.Close()
	for _, statement := range namespaceStatements(templateSchema, role) {
		if _, err := pool.ExecContext(ctx, statement); err != nil {
			return prepared, fmt.Errorf("prepare the template namespace: %w", err)
		}
	}
	appTarget, err := databaseURL(appURL, database, templateSchema)
	if err != nil {
		return prepared, err
	}
	if err := prepare(ownerURL, appTarget); err != nil {
		return prepared, err
	}
	// Every connection opened here is closed before this returns. Postgres refuses a clone while a
	// session stands in the template, so a pool some other part of the package leaked into it is
	// reported by the first case that asks, and the sentence it reads names the pool to close.
	readyTemplate = database
	return database, nil
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
	// Both spellings are refused: pgconn maps dbname onto database and reads either
	// (pgconn/config.go), so a URL that carries one would keep its own database and this
	// fixture would believe it had handed the caller a different one.
	for _, key := range []string{"dbname", "database"} {
		if _, found := queryValue(parsed.RawQuery, key); found {
			return "", fmt.Errorf("%s names its database in the query as well as the path; remove one", raw)
		}
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
// lock race and can take minutes when the server is busy: the same drop measured 0.4-0.9s between
// heavy phases on this host and did not answer inside 45s while every suite on it was tearing down
// at once, which is why the window is two minutes rather than the seconds an idle drop needs.
const (
	removeTries = 3
	removeTry   = 2 * time.Minute
)

func removeOwnDatabase(adminURL, database string) error {
	admin, err := openOwner(adminURL)
	if err != nil {
		return err
	}
	defer admin.Close()
	var last error
	for range removeTries {
		ctx, cancel := context.WithTimeout(context.Background(), removeTry)
		// A session that cannot be ended is not a failed removal: the DROP below either takes
		// the database, which is what the caller asked for, or reports itself on the next line.
		_, _ = admin.ExecContext(ctx, "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()", database)
		_, last = admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quote(database)+" WITH (FORCE)")
		cancel()
		if last == nil {
			return nil
		}
	}
	return last
}
