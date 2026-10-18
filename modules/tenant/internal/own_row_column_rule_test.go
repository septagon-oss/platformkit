package internal_test

// own_row_column_rule_test.go is the statement behind the column rule.
//
// 000071 split `tenants_scope` by command so a tenant transaction could change its own
// languages at all, and left the remaining question to a trigger: PostgreSQL has no
// column-level policy, so a policy that says "your own row" cannot also say "but only
// these columns of it". `tenants_language_only` answers that, and a trigger no case
// runs a statement through is a rule a reader cannot point at — the migration file said
// the refusal was proved "at the application role" while nothing here did it.
//
// So this runs it. The columns it refuses are read out of the catalog, not written out
// here: the trigger permits three columns and refuses whatever else the table holds, and
// a case that named the refused ones would stop covering the rule the day someone added
// a fourth fact about the customer — which is how this table grows. Each refused column
// is tried in a transaction of its own, because an exception aborts the one it is raised
// in, and the three permitted columns stay writable by that same transaction — which is
// the half that makes this a column rule rather than a plain refusal to write.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
)

// languageColumns are the columns tenants_language_only permits a tenant transaction:
// the two the self-service command writes and the one every write moves. Everything
// else the catalog says this table has is refused below.
var languageColumns = map[string]bool{"default_locale": true, "locale_revision": true, "updated_at": true}

// changed is how this case moves a column of each type the catalog answers with, in a
// way that cannot leave it where it was: a nullable column that holds nothing still
// gains a value, so a NULL old value never reads as "nothing changed" and pass the rule.
//
// A type outside this map fails the case rather than being skipped. A skipped column is
// a column the rule was never proved on, which is the failure this file exists to
// prevent; it is cheaper to teach the case a new type than to trust a trigger over a
// column it was never run against.
func changed(column, dataType string) (string, error) {
	switch dataType {
	case "text", "character varying", "character", "citext":
		return fmt.Sprintf("%[1]s = COALESCE(%[1]s, '') || '-rule-check'", column), nil
	case "boolean":
		return fmt.Sprintf("%s = NOT COALESCE(%s, false)", column, column), nil
	case "bigint", "integer", "smallint", "numeric":
		return fmt.Sprintf("%s = COALESCE(%s, 0) + 1", column, column), nil
	case "timestamp with time zone", "timestamp without time zone":
		return fmt.Sprintf("%s = COALESCE(%s, now()) + interval '1 day'", column, column), nil
	case "uuid":
		return fmt.Sprintf("%s = gen_random_uuid()", column), nil
	case "jsonb", "json":
		return fmt.Sprintf("%s = COALESCE(%s, '{}'::jsonb) || '{\"rule\":\"check\"}'", column, column), nil
	case "ARRAY":
		// The one array this table has is a text[] (oidc_roles); another element
		// type will fail here loudly, which is this file's standing answer to a
		// column the case has not been taught yet.
		return fmt.Sprintf("%s = array_append(COALESCE(%s, ARRAY[]::text[]), 'rule-check')", column, column), nil
	}
	return "", fmt.Errorf("this case cannot move a %s column: add it to changed() rather than skipping the column", dataType)
}

func TestATenantTransactionWritesItsOwnLanguageColumnsAndNoOtherColumn(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	svc := internal.NewService(nil, nil, "")
	installed(t, conn, svc)
	var tenant *contracts.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		tenant, err = svc.Create(ctx, tx, contracts.NewTenant{
			Slug: "acme", Name: "Acme", Host: "acme.example.com",
		})
		return err
	}); err != nil {
		t.Fatalf("create the tenant: %v", err)
	}

	// The catalog is the list. `tenants` is one table in one schema here, so the
	// question is asked once, in the schema this fixture created.
	type column struct{ name, dataType string }
	rows, err := owner.QueryContext(t.Context(), `
		SELECT column_name, data_type FROM information_schema.columns
		WHERE table_name = 'tenants' AND table_schema = current_schema()
		ORDER BY ordinal_position`)
	if err != nil {
		t.Fatalf("read the catalog of tenants columns: %v", err)
	}
	defer rows.Close()
	// Read the whole list before writing anything: each case below opens its own
	// transaction, and a held cursor would be one less connection for them.
	var columns []column
	for rows.Next() {
		var c column
		if err := rows.Scan(&c.name, &c.dataType); err != nil {
			t.Fatalf("read the catalog: %v", err)
		}
		if !languageColumns[c.name] {
			columns = append(columns, c)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the catalog: %v", err)
	}
	// An empty list is not a pass: it means the query read nothing, and a rule
	// proved over no columns is proved over nothing. `tenants` always holds at
	// least the columns this task's own migration reads.
	if len(columns) < 5 {
		t.Fatalf("the catalog named %d refused columns of tenants, want the table's own columns", len(columns))
	}
	// Each refused column in a transaction of its own: the trigger raises 42501,
	// which aborts the transaction it was raised in, so one statement over the
	// whole list would report the first refusal and the rest as
	// aborted-transaction errors.
	for _, c := range columns {
		set, err := changed(c.name, c.dataType)
		if err != nil {
			t.Fatalf("the %s column (%s): %v", c.name, c.dataType, err)
		}
		err = db.Run(tenancy.WithTenant(t.Context(), tenant.Tenancy()), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				result := tx.DB().Exec("UPDATE tenants SET "+set+" WHERE id = ?", tenant.ID)
				if result.Error == nil {
					t.Errorf("a tenant transaction wrote %s; the column rule did not run", set)
				} else if !strings.Contains(result.Error.Error(), "a tenant transaction may change only its own languages") {
					t.Errorf("writing %s gave %v, want the stated column rule", set, result.Error)
				}
				// The exception aborted this transaction, so it can only end by
				// rolling back; committing it would be a second error, of its own,
				// standing in for the refusal this case is about.
				return errRollback
			})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the %s transaction: %v", c.name, err)
		}
	}

	var name, status string
	var operator bool
	if err := owner.QueryRowContext(t.Context(),
		`SELECT name, status, operator FROM tenants WHERE id = $1`, tenant.ID).
		Scan(&name, &status, &operator); err != nil {
		t.Fatal(err)
	}
	if name != "Acme" || status != "active" || operator {
		t.Errorf("the refused writes left name=%q status=%q operator=%t, want Acme/active/false",
			name, status, operator)
	}

	// The columns the tenant's own language command writes are its own to write. A
	// rule that refused everything would satisfy the loop above and break the page.
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant.Tenancy()), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			result := tx.DB().Exec(
				`UPDATE tenants SET default_locale = 'pt-PT', locale_revision = locale_revision + 1, updated_at = now() WHERE id = ?`,
				tenant.ID)
			if result.Error != nil {
				return fmt.Errorf("the tenant's own language columns: %w", result.Error)
			}
			if result.RowsAffected != 1 {
				t.Errorf("writing the tenant's own language columns moved %d rows, want 1", result.RowsAffected)
			}
			return nil
		}); err != nil {
		t.Fatalf("%v", err)
	}
	var locale string
	var revision int64
	if err := owner.QueryRowContext(t.Context(),
		`SELECT default_locale, locale_revision FROM tenants WHERE id = $1`, tenant.ID).
		Scan(&locale, &revision); err != nil {
		t.Fatal(err)
	}
	if locale != "pt-PT" || revision != 1 {
		t.Errorf("the tenant's own declaration left default=%q revision=%d, want pt-PT and 1", locale, revision)
	}
}
