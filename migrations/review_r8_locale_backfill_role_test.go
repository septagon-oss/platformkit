package migrations_test

// Review round 8's failing case: the ledger's one row write has to be executable
// by the role that actually holds the migrate door.
//
// migrations/000028_tenant_locale.up.sql gives every existing tenant a language:
//
//	INSERT INTO tenant_locales (tenant_id, locale)
//	SELECT id, default_locale FROM tenants ON CONFLICT … DO NOTHING;
//
// and it does that in the same transaction as the `ALTER TABLE tenant_locales …
// FORCE ROW LEVEL SECURITY` and the policy whose `WITH CHECK` is
// `platformkit_is_system()`. The runner sets that marker on a `RunSystem`
// transaction (kit/db/tx.go) and on a backfill (kit/db/backfill.go), and on
// nothing in between: a `.up.sql` is executed by whatever role `migrate_url`
// names, with no GUC on the session. `config.example.yaml` describes that role as
// "the owner, which holds the DDL rights" — which is what a deployment that does
// not hand its schema to a superuser has. For that role the policy the file just
// installed refuses the file's own write:
//
//	ERROR: new row violates row-level security policy for table "tenant_locales"
//
// which is fail-closed, and which is why this is filed LOW rather than as the
// release-breaking thing it would be in the other direction. The case below is
// the runtime shape of that measurement, in the schema this package already
// builds: make the application role the owner of the table — neither superuser
// nor BYPASSRLS, which `dbtest` guarantees — and run the row-writing statements
// the file carries as that role.
//
// It has a passing branch two ways, which is the point: the file can set the
// marker the way a data half does, or the write can move to the release's own
// `data: backfill` file, which the runner executes under `RunSystem`. Under
// either cure the branch below stops executing a raw write and checks instead
// that the file names the shape that makes a write run as the system — and a
// tenant's default language is served either way, which the reference app's own
// bootstrap and round 3's two-tenant case already hold.

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db/dbtest"
)

// TestTheTenantLocaleBackfillRunsAsTheRoleThatMigrates is the case above.
func TestTheTenantLocaleBackfillRunsAsTheRoleThatMigrates(t *testing.T) {
	adminURL, appURL := dbtest.URLs(t)
	admin, _ := dbtest.Schema(t) // the migrated schema, the ledger applied as the owner
	role := dbtest.RoleOf(t, appURL)
	app := dbtest.Open(t, appURL) // a plain connection as the application role
	_ = adminURL

	// A tenant exists before the languages do — the situation the backfill is for.
	var id string
	if err := admin.QueryRowContext(t.Context(),
		`INSERT INTO tenants (slug, name) VALUES ('r8-backfill', 'R8 Backfill') RETURNING id`).
		Scan(&id); err != nil {
		t.Fatalf("a tenant before the migration role ran: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.ExecContext(t.Context(), `DELETE FROM tenant_locales WHERE tenant_id = $1`, id)
		_, _ = admin.ExecContext(t.Context(), `DELETE FROM tenants WHERE id = $1`, id)
	})

	file := readLocaleMigration(t)
	writes, marks := rowWrites(file)
	if len(writes) == 0 {
		// The cure: no raw write in the schema half. Then the file has to name the
		// shape that runs a write as the system, which is what a reader of the ledger
		// needs to see to know the languages are written at all.
		if !marks {
			t.Error("migrations/000028_tenant_locale.up.sql writes no rows and names no data: " +
				"backfill or data: exempt marker, so nothing gives an existing tenant its " +
				"default language")
		}
		return
	}

	// The migrate door held by the role that owns the tables, which is what
	// "the owner, which holds the DDL rights" says when the deployment does not
	// hand the door to a superuser. dbtest's app role is NOSUPERUSER NOBYPASSRLS.
	if _, err := admin.ExecContext(t.Context(), "ALTER TABLE tenant_locales OWNER TO "+role); err != nil {
		t.Fatalf("the migration role cannot be made the table's owner: %v", err)
	}
	if _, err := admin.ExecContext(t.Context(), `DELETE FROM tenant_locales WHERE tenant_id = $1`, id); err != nil {
		t.Fatalf("clearing the languages of one tenant: %v", err)
	}

	// What the file reads from is as much under the policy as what it writes to:
	// migrations/000001 says of its own helpers that "outside any transaction of
	// ours both helpers yield NULL or false, so the policy denies rather than
	// leaks", and `tenants` carries that policy. So the source of an
	// `INSERT … SELECT FROM tenants` is empty for a role that is not in a system
	// transaction, the write is accepted, and nothing is written.
	var visible int
	if err := app.QueryRowContext(t.Context(), `SELECT count(*) FROM tenants`).Scan(&visible); err != nil {
		t.Fatalf("asking the migration role how many tenants it can see: %v", err)
	}
	if visible == 0 {
		t.Error("the role that holds the migrate door sees none of the tenants the migration is about to " +
			"give languages to, so its `INSERT … SELECT … FROM tenants` writes nothing and Postgres reports " +
			"no error at all: the tenant that predates the column ends up served in no language the ledger says it serves")
	}

	for _, statement := range writes {
		if _, err := app.ExecContext(t.Context(), statement); err != nil {
			t.Errorf("the owner of tenant_locales, which is not a superuser, could not run the write "+
				"the migration carries (%s): %v — the policy this same file installs answers its own "+
				"backfill with a refusal, so on that migrate role the release aborts", short(statement), err)
			break
		}
	}

	var served int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM tenant_locales WHERE tenant_id = $1 AND locale = 'en'`, id).Scan(&served); err != nil {
		t.Fatalf("reading the languages the tenant is served in: %v", err)
	}
	if served != 1 {
		t.Errorf("the migration's backfill left the tenant served in %d languages rather than the one "+
			"its default_locale column names", served)
	}
}

// writeMarker is the migration's own marker for a data half, either the phase the
// runner drains (`data: backfill`) or the refusal of one (`data: exempt reason:`).
var writeMarker = regexp.MustCompile(`data:\s*(backfill|exempt)`)

// rowWrites is the statements in a migration that write rows, in the order the file
// states them, with the file's marker for a data half alongside. Statements run
// outside a transaction by this case, so a `set_config(..., true)` line the cure
// would add travels with them only inside the same session — which is why any
// `set_config` line in the file leads the list.
func rowWrites(file string) ([]string, bool) {
	marks := writeMarker.MatchString(file)
	var out []string
	for _, line := range strings.Split(file, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "--") || trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "SELECT set_config('platformkit.system_access'") ||
			strings.HasPrefix(trimmed, "PERFORM set_config('platformkit.system_access'") {
			out = append(out, trimmed)
		}
	}
	// The row writes themselves, whole statements rather than the one line the
	// INSERT begins with: the body runs to the semicolon that ends it.
	for _, chunk := range strings.Split(file, ";") {
		body := strings.TrimSpace(stripComments(chunk))
		if body == "" {
			continue
		}
		head := strings.ToUpper(body[:min(6, len(body))])
		if strings.HasPrefix(head, "INSERT") || strings.HasPrefix(head, "UPDATE") || strings.HasPrefix(head, "DELETE") {
			out = append(out, body+";")
		}
	}
	return out, marks
}

// stripComments drops whole-line comments from a statement chunk, which is all a
// migration's commentary is.
func stripComments(chunk string) string {
	var keep []string
	for _, line := range strings.Split(chunk, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "--") {
			continue
		}
		keep = append(keep, line)
	}
	return strings.TrimSpace(strings.Join(keep, "\n"))
}

// readLocaleMigration reads the file the case is about, from the package directory
// the test runs in. A ledger that renamed the file is a ledger where the case has
// nothing to say, and says so.
func readLocaleMigration(t *testing.T) string {
	t.Helper()
	names, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("the migration directory cannot be read: %v", err)
	}
	for _, name := range names {
		if strings.HasSuffix(name.Name(), ".up.sql") && strings.Contains(name.Name(), "tenant_locale") {
			body, err := os.ReadFile(name.Name())
			if err != nil {
				t.Fatalf("%s cannot be read: %v", name.Name(), err)
			}
			return string(body)
		}
	}
	t.Skip("no *_tenant_locale.up.sql in the ledger; this case names a file that is not there")
	return ""
}

// short is a statement read as far as a failing assertion needs.
func short(s string) string {
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
