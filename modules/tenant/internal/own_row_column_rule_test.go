package internal_test

// own_row_column_rule_test.go is the statement behind the column rule.
//
// 000031 split `tenants_scope` by command so a tenant transaction could change its own
// languages at all, and left the remaining question to a trigger: PostgreSQL has no
// column-level policy, so a policy that says "your own row" cannot also say "but only
// these columns of it". `tenants_language_only` answers that, and a trigger no case
// runs a statement through is a rule a reader cannot point at — the migration file said
// the refusal was proved "at the application role" while nothing here did it.
//
// So this runs it: the five columns that describe the customer to the installation are
// refused to the customer's own transaction, each in its own transaction because an
// exception aborts the one it is raised in, and the three columns the language command
// writes stay writable by that same transaction — which is the half that makes this a
// column rule rather than a plain refusal to write.

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

func TestATenantTransactionWritesItsOwnLanguageColumnsAndNoOtherColumn(t *testing.T) {
	owner, conn := dbtest.Schema(t)
	var tenant *contracts.Tenant
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var err error
		tenant, err = internal.NewService(nil, nil).Create(ctx, tx, contracts.NewTenant{
			Slug: "acme", Name: "Acme", Host: "acme.example.com",
		})
		return err
	}); err != nil {
		t.Fatalf("create the tenant: %v", err)
	}

	// Each refused column in a transaction of its own: the trigger raises 42501, which
	// aborts the transaction it was raised in, so one loop over four columns would
	// report the first refusal and three aborted-transaction errors.
	for _, column := range []struct{ name, value string }{
		{"slug", "'renamed'"},
		{"name", "'Renamed'"},
		{"status", "'suspended'"},
		{"operator", "true"},
		{"created_at", "now() + interval '1 day'"},
	} {
		want := fmt.Sprintf("%s = %s", column.name, column.value)
		err := db.Run(tenancy.WithTenant(t.Context(), tenant.Tenancy()), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				result := tx.DB().Exec("UPDATE tenants SET "+want+" WHERE id = ?", tenant.ID)
				if result.Error == nil {
					t.Errorf("a tenant transaction wrote %s; the column rule did not run", want)
				} else if !strings.Contains(result.Error.Error(), "a tenant transaction may change only its own languages") {
					t.Errorf("writing %s gave %v, want the stated column rule", want, result.Error)
				}
				// The exception aborted this transaction, so it can only end by
				// rolling back; committing it would be a second error, of its own,
				// standing in for the refusal this case is about.
				return errRollback
			})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the %s transaction: %v", column.name, err)
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
