// Package migrations_test holds one test, and it is about the whole directory
// rather than about any file in it: every table this schema creates is either
// protected by the tenant policy migrations/000001 describes or declared exempt
// in the comment the convention uses.
//
// It is here and not in kit/db because the claim is about the SQL. kit/db's
// tests prove that the mechanism works — that a forgotten WHERE returns nothing
// and that FORCE is what binds the owner; this one proves that every table
// actually uses it, which is the half a new migration can get wrong by saying
// nothing at all. A table with no policy is not refused by Postgres, is not
// refused by the compiler, and returns every tenant's rows to every tenant.
package migrations_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/migrations"
)

// exemption is the marker a table that belongs to no tenant carries, in its
// own COMMENT, so that "this one is deliberate" is written where the table is
// and is greppable from outside the repository.
const exemption = "platformkit:tenant-scoping-exempt"

// ledger is golang-migrate's own bookkeeping. It is created by the migration
// tool rather than by any file here, so it cannot carry a comment of ours, and
// it holds one row saying which version was applied — no tenant's data and
// nothing a policy would narrow.
const ledger = "schema_migrations"

// TestEveryTableIsScopedOrExemptOnPurpose.
func TestEveryTableIsScopedOrExemptOnPurpose(t *testing.T) {
	adminURL, _ := dbtest.URLs(t)
	if err := db.Migrate(t.Context(), adminURL, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rows, err := dbtest.Open(t, adminURL).QueryContext(t.Context(), `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity,
			coalesce(obj_description(c.oid, 'pg_class'), ''),
			coalesce((SELECT string_agg(pg_get_expr(p.polqual, p.polrelid), ' ')
				FROM pg_policy p WHERE p.polrelid = c.oid), '')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = current_schema() AND c.relkind = 'r'
		ORDER BY c.relname`)
	if err != nil {
		t.Fatalf("walk the schema: %v", err)
	}
	defer rows.Close()

	seen := 0
	for rows.Next() {
		var name, comment, policies string
		var enabled, forced bool
		if err := rows.Scan(&name, &enabled, &forced, &comment, &policies); err != nil {
			t.Fatalf("read a table: %v", err)
		}
		if name == ledger {
			continue
		}
		seen++
		switch {
		case !enabled:
			t.Errorf("%s has no row-level security; every tenant would see every tenant's rows", name)
		case !forced:
			// ENABLE exempts the table's owner from its own policy, and the
			// application role owns any table it creates itself.
			t.Errorf("%s is ENABLE without FORCE, so its owner escapes its own policy", name)
		case strings.Contains(policies, "platformkit_tenant_match(tenant_id)"):
			// The ordinary shape: scoped by the column, by the one predicate.
		case strings.Contains(comment, exemption):
			// Deliberate, and said where the table is. The two tables that
			// carry it answer the question before there is a transaction to ask
			// it in; their policies still narrow a tenant transaction to one row.
			if policies == "" {
				t.Errorf("%s is declared exempt and has no policy at all; exempt from the column, not from the door", name)
			}
		default:
			t.Errorf("%s is neither scoped by platformkit_tenant_match(tenant_id) nor declared %q in its COMMENT; "+
				"its policy is %q", name, exemption, policies)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("walk the schema: %v", err)
	}
	// A query that found nothing would pass every case above, which is the one
	// way this test could be worthless.
	if seen < 10 {
		t.Errorf("the schema has %d tables, which is fewer than migrations/ creates", seen)
	}
}
