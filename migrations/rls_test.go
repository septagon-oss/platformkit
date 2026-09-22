// Package migrations_test holds the tests that are about every migration in
// the repository rather than about any one file: every table the kernel's
// schema and the reference modules' schemas create is either protected by the
// tenant policy migrations/000001 describes or declared exempt in the comment
// the convention uses, and a schema a module owns is a schema both halves of
// that claim have to reach. module_schema_test.go adds the claims about the
// kernel function that hands a module its schema to the application role, and
// about the walk below, which is exported so the catalog's own test stops
// copying it.
//
// It is here and not in kit/db because the claim is about the SQL. kit/db's
// tests prove that the mechanism works — that a forgotten WHERE returns nothing
// and that FORCE is what binds the owner; this one proves that every table
// actually uses it, which is the half a new migration can get wrong by saying
// nothing at all. A table with no policy is not refused by Postgres, is not
// refused by the compiler, and returns every tenant's rows to every tenant.
package migrations_test

import (
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/billing"
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/user"
)

// everything is the kernel's schema and every reference module's, in the order
// apps/platformkit composes them: the claim below is about every table this
// repository creates, whichever owner now carries the file.
var everything = []db.MigrationSource{migrations.Source, user.Migrations, notification.Migrations, auth.Migrations,
	task.Migrations, billing.Migrations, content.Migrations, site.Migrations, file.Migrations, audit.Migrations}

// exemption is the marker a table that belongs to no tenant carries, in its
// own COMMENT, so that "this one is deliberate" is written where the table is
// and is greppable from outside the repository.
const exemption = "platformkit:tenant-scoping-exempt"

// The runner owns migration history and the cursor of a drain that has not
// finished, and revokes application access to both. They hold schema metadata —
// an owner and a version, a primary key — and no tenant's row, so neither carries
// a tenant policy: a policy would have to name a tenant the row does not have.
// The one door on them is the REVOKE, and it is checked rather than asserted —
// below, in this walk, from the catalog, and by running the statements from an
// application connection in kit/db/review_guarantees_test.go. A table added to
// this list without that door is a table somebody else can write.
var runnerTables = []string{"schema_migrations", "schema_migration_backfill"}

// TestEveryTableIsScopedOrExemptOnPurpose.
func TestEveryTableIsScopedOrExemptOnPurpose(t *testing.T) {
	adminURL, appURL := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, everything...); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	admin := dbtest.Open(t, adminURL)
	tables, problems := tenantScope(t, admin)
	for _, problem := range problems {
		t.Error(problem)
	}
	// Skipping the policy question is not skipping the table. The runner's two
	// tables are exempt from the tenant column because they hold no tenant's row,
	// and the claim that replaces the policy is that the application role holds
	// nothing on them. It is asked here, of the list that skips them, so the list
	// cannot grow into a table an application can forge a release in — and of
	// every name on it, because a name that stopped naming a table has to fail
	// rather than stop being asked.
	app := dbtest.RoleOf(t, appURL)
	for _, name := range runnerTables {
		var granted bool
		err := admin.QueryRowContext(t.Context(), `
			SELECT has_table_privilege($1, c.oid, 'SELECT,INSERT,UPDATE,DELETE')
			FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = current_schema() AND c.relkind = 'r' AND c.relname = $2`,
			app, name).Scan(&granted)
		if errors.Is(err, sql.ErrNoRows) {
			t.Errorf("%s is on the runner's own list and names no table of this schema; the list and the runner have come apart", name)
			continue
		}
		if err != nil {
			t.Fatalf("read the door on %s: %v", name, err)
		}
		if granted {
			t.Errorf("%s is on the runner's own list and the application role still holds a privilege on it; the REVOKE is the only door these tables have", name)
		}
	}
	// A query that found nothing would pass every case above, which is the one
	// way this test could be worthless.
	if len(tables) < 10 {
		t.Errorf("the schema has %d tables, which is fewer than the kernel and the modules create", len(tables))
	}
}

// tenantScope asks the one question the tenant-scope claim asks of every table
// the walk sees, and separates "this table is a problem" from "this table was
// never looked at". The two have to be separable: a walk that quietly listed
// nothing would report no problems at all, which is how the claim survives the
// arrival of a schema it cannot see. It returns the tables schema-qualified,
// because under one schema per owner a bare table name is not an address.
//
// It runs on the connection the DDL ran on, and that is not incidental:
// schema_migrations resolves through the caller's search_path, and a policy's
// expression deparses qualified or bare depending on whether the schema its
// function lives in is on that path.
//
// The one walk is dbtest.TenantTablesSQL, which is exported because a module's
// own schema test asks the same question; the door the runner's two tables rely
// on is a different question and is asked by the test above, not here.
func tenantScope(t *testing.T, admin *sql.DB) (tables, problems []string) {
	t.Helper()
	rows, err := admin.QueryContext(t.Context(), dbtest.TenantTablesSQL)
	if err != nil {
		t.Fatalf("walk the schema: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var schema, name, comment, policies string
		var enabled, forced bool
		if err := rows.Scan(&schema, &name, &enabled, &forced, &comment, &policies); err != nil {
			t.Fatalf("read a table: %v", err)
		}
		if slices.Contains(runnerTables, name) {
			// Exempt from the tenant column, because these two hold no tenant's
			// row. The claim that replaces the policy — that the application role
			// holds nothing on them — is the caller's, asked of this same list.
			continue
		}
		table := schema + "." + name
		tables = append(tables, table)
		switch {
		case !enabled:
			problems = append(problems, fmt.Sprintf(
				"%s has no row-level security; every tenant would see every tenant's rows", table))
		case !forced:
			// ENABLE exempts the table's owner from its own policy, and the
			// application role owns any table it creates itself.
			problems = append(problems, fmt.Sprintf(
				"%s is ENABLE without FORCE, so its owner escapes its own policy", table))
		case strings.Contains(policies, "platformkit_tenant_match(tenant_id)"):
			// The ordinary shape: scoped by the column, by the one predicate.
		case strings.Contains(comment, exemption):
			// Deliberate, and said where the table is. The two tables that
			// carry it answer the question before there is a transaction to ask
			// it in; their policies still narrow a tenant transaction to one row.
			if policies == "" {
				problems = append(problems, fmt.Sprintf(
					"%s is declared exempt and has no policy at all; exempt from the column, not from the door", table))
			}
		default:
			problems = append(problems, fmt.Sprintf(
				"%s is neither scoped by platformkit_tenant_match(tenant_id) nor declared %q in its COMMENT; "+
					"its policy is %q", table, exemption, policies))
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("walk the schema: %v", err)
	}
	return tables, problems
}
