package dbtest

import (
	"fmt"
	"hash/fnv"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// readyTemplate is the template database this process prepared, named by
// RunInOwnDatabaseWithTemplate before the first case starts. Empty means the package named no
// template, which is what a case asking for a clone of one then reports.
var readyTemplate string

// Clone gives one test a database of its own holding a copy of what the package's
// TestMain prepared, and returns the two URLs URLs hands a test — an owner URL and
// the unprivileged application role's — landing in a namespace of the test's own.
//
// The copy is the server copying files, so the migration and the ledger arrive
// without being replayed, and it carries the row-level security policies and the
// grants, because they are in the files. The template is never among the URLs a case
// is given and cannot be asked for by name, so nothing a case writes reaches the next
// one. The namespace inside the copy is renamed to one derived from the test, which
// is what application_name and Role would otherwise answer for every case at once.
//
// It fails the case when no database is configured or the copy is refused.
func Clone(t testing.TB) (adminURL, appURL string) {
	t.Helper()
	if readyTemplate == "" {
		t.Fatalf("dbtest: Clone: this package prepared no template; its TestMain must call RunInOwnDatabaseWithTemplate")
	}
	baseAdmin := mustEnv(t, "PLATFORMKIT_TEST_ADMIN_URL")
	baseApp := mustEnv(t, "PLATFORMKIT_TEST_DATABASE_URL")
	name := cloneName(t.Name())

	admin := OpenFor(t, baseAdmin)
	// A case may ask more than once — a helper that hands it one installation per module it is
	// reading is exactly that — and each ask is an installation of its own. URLsFor answers the same
	// ask by dropping the namespace it handed out last time; a copy has no reason to destroy what the
	// previous ask is still holding, so the asks of one case are numbered and each is dropped when
	// the case ends.
	if n := asksFor(t.Name()).Add(1); n > 1 {
		name = name + "_" + strconv.FormatInt(n-1, 10)
	}

	if _, err := admin.ExecContext(t.Context(), "CREATE DATABASE "+quote(name)+" TEMPLATE "+quote(readyTemplate)); err != nil {
		t.Fatalf("dbtest: clone %s from %s: %v\n\ttwo answers refuse a copy: the name is already taken, which means the asks of one case are not numbered, and a session stands in the template, which means a pool opened on it was never closed", name, readyTemplate, err)
	}
	t.Cleanup(func() {
		if err := removeOwnDatabase(baseAdmin, name); err != nil {
			t.Errorf("dbtest: the cloned database %s is still there: %v", name, err)
		}
	})

	// The copy names the namespace the preparation used, and that name is inside the copied files:
	// every clone of one template would otherwise share it, and so share the role name derived from
	// it. The rename is DDL in the new database, which nothing outside it can see.
	owner, err := databaseURL(baseAdmin, name, templateSchema)
	if err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	rename := OpenFor(t, owner)
	if _, err := rename.ExecContext(t.Context(), "ALTER SCHEMA "+quote(templateSchema)+" RENAME TO "+quote(name)); err != nil {
		t.Fatalf("dbtest: rename the copied namespace of %s: %v", name, err)
	}
	_ = rename.Close()

	if adminURL, err = databaseURL(baseAdmin, name, name); err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	if appURL, err = databaseURL(baseApp, name, name); err != nil {
		t.Fatalf("dbtest: %v", err)
	}
	return adminURL, appURL
}

// asksFor is how many installations one case has been given. The count is per name rather than per
// *testing.T because the name is what the database is called by, and it is the second ask under one
// name that would otherwise ask the server for a database that is already there.
func asksFor(name string) *atomic.Int64 {
	value, _ := asks.LoadOrStore(name, new(atomic.Int64))
	return value.(*atomic.Int64)
}

var asks sync.Map // test name → *atomic.Int64

// databaseURL is raw pointed at database and landing in schema, both of which pgx reads from the
// URL it is given: the path for the database, `options` for the namespace.
func databaseURL(raw, database, schema string) (string, error) {
	target, err := intoDatabase(raw, database)
	if err != nil {
		return "", err
	}
	return schemaURL(target, schema)
}

// askRoom is the characters a repeated ask keeps for its own number, beside the two digits a case
// that asks twice needs.
const askRoom = 4

// cloneName is a test name as a database name: this process's own run id, so two packages started
// at once cannot name one database; the test's own name, so a person reading psql can see which
// case a database belonged to; and a digest of the whole name, because one PostgreSQL identifier
// cuts a long test name and two cases cut at the same character would otherwise share a database.
func cloneName(test string) string {
	// One PostgreSQL identifier, minus this process's prefix, run id, separators and digest, minus
	// the room a second ask under the same name needs, minus the two characters the login role this
	// namespace names costs: Role derives a role name from the namespace by prefixing it, so a
	// namespace that fills the identifier limit leaves its role short of the limit by that much, and
	// two copies whose namespaces differ only in the ask number would be one role. PostgreSQL
	// shortens a long identifier without saying so; this budget is what stops it having to.
	budget := identifierLimit - len("pkc_") - len(run) - 2 - digestWidth - askRoom - len(rolePrefix)
	name := sanitize(test)
	if len(name) > budget {
		name = name[:budget]
	}
	digest := fnv.New32a()
	_, _ = digest.Write([]byte(test))
	return fmt.Sprintf("pkc_%s_%s_%08x", run, name, digest.Sum32())
}
