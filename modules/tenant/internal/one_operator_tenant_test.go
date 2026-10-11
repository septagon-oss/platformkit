package internal_test

// one_operator_tenant_test.go pins the two database facts that every sentence
// about "the operator's own tenant" in this module is finally backed by.
//
// modules/tenant/internal/service.go reads which tenant is the installation with
// `WHERE operator AND deleted_at IS NULL` and no ORDER BY, on the strength of one
// comment — "The predicate is the one the partial unique index tenants_operator
// serves" (service.go:504) — and migrations/000012_operator.up.sql:32 is the only
// thing that makes that a single row rather than whichever row the planner reached
// first. Nothing in the tree names that index: `grep -rn tenants_operator --include=*.go`
// answers with the comment and no assertion, so dropping the CREATE UNIQUE INDEX
// line would leave every case here green while the mirror row went to whichever of
// two candidate installations a query happened to like.
//
// The second fact is the one the whole control plane's safety argument points at.
// kit/httpx refuses an operator route by reading `tenancy.Tenant.Operator`
// (kit/httpx/authorize.go:106), and that field is `tenants.operator`, a plain
// boolean column. migrations/000006_tenant.up.sql:51-53 closes the door on it with
// `WITH CHECK (platformkit_is_system())`, and the two cases that write that column
// today (review_r1_the_refusal_writes_nothing_test.go:62 and
// review_r2_a_refused_add_host_writes_no_host_row_test.go:62) write it from a
// *system* transaction, to take the installation away. No case asks whether a
// tenant's own transaction can put the column back, which is the difference between
// "a customer cannot reach the control plane" being a database fact and being a
// fact about which routes happen to be mounted.

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestATenantCannotWriteWhichTenantIsTheInstallation is the escalation case the
// operator grant is worth: a customer's own transaction tries to set the column
// that says it is the installation, on its own row and on the installation's.
// Neither reaches the column — the first is refused by the policy's WITH CHECK,
// the second never finds a row to refuse because the policy's USING hides the
// installation from a tenant that is not it — and the row the tenant holds stays a
// customer's row.
func TestATenantCannotWriteWhichTenantIsTheInstallation(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil, "")
	installed(t, conn, svc)

	var operator, customer uuid.UUID
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		ids, err := operatorIDs(t, tx)
		if err != nil {
			return err
		}
		operator = ids[0]
		created, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"})
		if err != nil {
			return err
		}
		customer = created.ID
		return nil
	}); err != nil {
		t.Fatalf("install an installation with one customer: %v", err)
	}

	// The tenant's own transaction, on its own row: the shape of write that would
	// turn every operator route into an open door if the policy let it through.
	// The tenant's own transaction, on its own row: the shape of write that would
	// turn every operator route into an open door if the policy let it through. The
	// refused statement goes last, because Postgres aborts a transaction at the
	// first error and anything after it would be a case about SQLSTATE 25P02.
	ownErr := db.Run(tenancy.WithTenant(t.Context(), tenancy.Tenant{ID: customer}), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			// The installation's row is not even visible here, so there is no row for
			// a statement to flip — the USING half of the policy, answered first.
			var seen int64
			if err := tx.DB().Table("tenants").Where("operator").Count(&seen).Error; err != nil {
				return err
			}
			if seen != 0 {
				t.Errorf("the customer's transaction sees %d operator rows, want none of its own", seen)
			}
			return tx.DB().Exec("UPDATE tenants SET operator = true WHERE id = ?", customer).Error
		})
	if ownErr == nil {
		t.Error("the customer's transaction wrote operator = true on its own row and nothing refused it; " +
			"migrations/000006_tenant.up.sql's tenants_scope WITH CHECK (platformkit_is_system()) is what stands between a customer and the control plane")
	} else if !strings.Contains(ownErr.Error(), "row-level security") {
		t.Errorf("the write to tenants.operator failed with %v, want the row-level security refusal WITH CHECK states", ownErr)
	}

	var stillCustomer, installationIsOperator bool
	if err := admin.QueryRowContext(t.Context(),
		`SELECT operator FROM tenants WHERE id = $1`, customer).Scan(&stillCustomer); err != nil {
		t.Fatalf("read back the customer's operator column: %v", err)
	}
	if stillCustomer {
		t.Errorf("tenant %s reads operator = true from the owner's connection, want the refusal to have written nothing", customer)
	}
	if err := admin.QueryRowContext(t.Context(),
		`SELECT operator FROM tenants WHERE id = $1`, operator).Scan(&installationIsOperator); err != nil {
		t.Fatalf("read back the installation's operator column: %v", err)
	}
	if !installationIsOperator {
		t.Error("the installation's own row lost the operator column, which no case here asked it to lose")
	}
}

// TestTheInstallationNamesOneOperatorTenantToAuditFrom pins the single row the
// mirror is scoped to, from both ends: the database refuses a second live operator
// row under the constraint the code's comment names, and the mirrors two verbs on
// two different customers wrote all name that one row.
func TestTheInstallationNamesOneOperatorTenantToAuditFrom(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, auth.Migrations)
	svc := internal.NewService(nil, nil, "")
	installed(t, conn, svc)

	var operator uuid.UUID
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		ids, err := operatorIDs(t, tx)
		if err != nil {
			return err
		}
		operator = ids[0]
		if _, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: "acme", Name: "Acme", Host: "acme.example.com"}); err != nil {
			return err
		}
		if _, err := svc.Create(ctx, tx, contracts.NewTenant{Slug: "beta", Name: "Beta", Host: "beta.example.com"}); err != nil {
			return err
		}
		// Start the assertion from the verbs, not from everything the install wrote.
		if err := tx.DB().Exec("DELETE FROM platformkit_outbox").Error; err != nil {
			return err
		}
		acme, err := svc.Suspend(ctx, tx, idOf(t, tx, "acme"))
		if err != nil {
			return err
		}
		_, err = svc.Rename(ctx, tx, acme.ID, contracts.Rename{Name: "Acme, renamed"})
		return err
	}); err != nil {
		t.Fatalf("two customers and two verbs: %v", err)
	}
	var second error
	if err := dbtest.System(t.Context(), conn, func(_ context.Context, tx db.Tx[db.System]) error {
		// The write `installation()` could not disambiguate against: a second live
		// operator row, in a transaction of its own because Postgres aborts a
		// transaction at its first error. It is asked of the database directly
		// because no route and no service method can ask it — NewTenant.Operator is
		// json:"-" — which is exactly why the index, and not the absence of a
		// caller, is the guard.
		return tx.DB().Exec(
			`INSERT INTO tenants (slug, name, operator) VALUES ('second-installation', 'Second installation', true)`).Error
	}); err != nil {
		second = err
	}
	if second == nil {
		t.Error("a second live operator tenant was accepted; migrations/000012_operator.up.sql's tenants_operator index is the only thing that makes `WHERE operator AND deleted_at IS NULL LIMIT 1` name one row")
	} else if !strings.Contains(second.Error(), "tenants_operator") {
		t.Errorf("the second operator tenant failed with %v, want the refusal to come from the index the code names", second)
	}

	var scopes []uuid.UUID
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventLifecycleRecorded).
			Pluck("tenant_id", &scopes).Error
	}); err != nil {
		t.Fatalf("read the mirrors back: %v", err)
	}
	if len(scopes) != 2 {
		t.Fatalf("the two verbs left %d operator mirrors, want one per verb", len(scopes))
	}
	for _, s := range scopes {
		if s != operator {
			t.Errorf("a lifecycle mirror is scoped to %s, want the installation's own tenant %s", s, operator)
		}
	}
	var live int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM tenants WHERE operator AND deleted_at IS NULL`).Scan(&live); err != nil {
		t.Fatalf("count the live operator tenants: %v", err)
	}
	if live != 1 {
		t.Errorf("the installation has %d live operator tenants, want the one every mirror names", live)
	}
}

// idOf is the tenant's id by slug, inside the transaction that is already holding
// the row the verb is about to take.
func idOf(t *testing.T, tx db.Tx[db.System], slug string) uuid.UUID {
	t.Helper()
	var ids []uuid.UUID
	if err := tx.DB().Table("tenants").Where("slug = ?", slug).Pluck("id", &ids).Error; err != nil {
		t.Fatalf("read the id of %q: %v", slug, err)
	}
	if len(ids) != 1 {
		t.Fatalf("the slug %q names %d tenants, want one", slug, len(ids))
	}
	return ids[0]
}

// operatorIDs is every tenant the installation says is itself — the value
// service.go's `installation()` reads with no ORDER BY, so the case reads it the
// same way and asserts there is one to read.
func operatorIDs(t *testing.T, tx db.Tx[db.System]) ([]uuid.UUID, error) {
	t.Helper()
	var ids []uuid.UUID
	if err := tx.DB().Table("tenants").Where("operator AND deleted_at IS NULL").Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	if len(ids) != 1 {
		t.Fatalf("the installation names %d operator tenants, want the one every mirror is scoped to", len(ids))
	}
	return ids, nil
}
