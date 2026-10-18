package auth_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// review9SeededRow pins the one direction of the brief's invariant no other case
// in this repository asserts against an independent expectation.
//
// TestTheSeededRolesAreTheCompositionsAndNothingElse compares the rows SeedRoles
// wrote with contracts.SeededRoles over the same catalogue — the seeder read
// against the seeder's own decision. That catches the defect this change fixed
// (a caller handing over a permission no composed module declares) and the one
// named permission leaving with its module, but both sides of the comparison are
// produced by OperatorGrants, so a mirror defect inside that function is invisible
// to it: an inverted filter hands the operator's administrator every ordinary
// permission and no operator one, which reads as *almost right* — the wildcard
// keeps every ordinary route answering — and takes tenant administration away
// from the only tenant that can hold it. Nothing in the comparison could tell
// that row from the correct one; both would be compared against what the same
// function said.
//
// So this case names the expected row itself, permission by permission, and reads
// it back out of Postgres rather than out of the function that wrote it.
//
// Limits: it pins what one catalogue seeds. It says nothing about which
// permissions a catalogue should hold — that is the modules' manifests — nor
// about the repair, which has its own cases in this package.
func TestTheOperatorTenantIsSeededEveryOperatorPermissionOfTheCatalogueAndNothingElse(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)

	// A catalogue written out here and not derived from any function under test:
	// two operator permissions, two ordinary ones, in an order a composition
	// would plausibly produce (module order, not alphabetical).
	catalogue := []tenancy.Grant{
		{Permission: "role:manage"},
		{Permission: "task:read"},
		{Permission: "tenant:manage", Operator: true},
		{Permission: "billing:catalog", Operator: true},
	}
	operator := tenancy.Tenant{ID: uuid.New(), Slug: "operator", Operator: true}
	defaults := []contracts.Role{{Name: "finance", Grants: contracts.Permissions{"role:manage"}}}

	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return auth.SeedRoles(ctx, tx, operator, catalogue, defaults)
	})
	if err != nil {
		t.Fatalf("seed the operator's tenant: %v", err)
	}

	want := map[string][]string{
		// The wildcard and both operator permissions — the positive direction of
		// "as wide as the composition": a grant the catalogue names as the
		// operator's that the row does not hold is an installation nobody can
		// administer through the control plane.
		"admin":   {"*", "billing:catalog", "tenant:manage"},
		"finance": {"role:manage"},
		// Nothing else, in any order and under any name: no ordinary permission
		// is named beside a wildcard that already grants it.
		"member": nil,
	}
	got := seededRoles(t, conn, operator)
	if len(got) != len(want) {
		t.Fatalf("the seeder wrote %d roles (%s); this catalogue seeds %d",
			len(got), roleNames(got), len(want))
	}
	for _, role := range got {
		expect, ok := want[role.Name]
		if !ok {
			t.Fatalf("the seeder wrote the role %q, which this catalogue and no initial role names", role.Name)
		}
		have := slices.Clone([]string(role.Grants))
		slices.Sort(have)
		if strings.Join(have, " ") != strings.Join(expect, " ") {
			t.Errorf("role %q holds [%s] and this catalogue seeds [%s]",
				role.Name, strings.Join(have, " "), strings.Join(expect, " "))
		}
		delete(want, role.Name)
	}
	for name := range want {
		t.Errorf("the seeder wrote no role %q, which this catalogue seeds", name)
	}
}

// TestACustomersTenantRowHoldsTheWildcardAndNoNamedPermission is the same
// assertion read from the other side of the tenancy boundary: the operator
// permissions of the catalogue are nobody else's grant, and the row must hold the
// wildcard alone — a named permission in a customer's administrator is the shape
// the hourly warning was written about, and it is the shape the repair is allowed
// to take back only in the operator's own row.
func TestACustomersTenantRowHoldsTheWildcardAndNoNamedPermission(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	catalogue := []tenancy.Grant{
		{Permission: "role:manage"},
		{Permission: "tenant:manage", Operator: true},
		{Permission: "billing:catalog", Operator: true},
	}
	customer := tenancy.Tenant{ID: uuid.New(), Slug: "acme"}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return auth.SeedRoles(ctx, tx, customer, catalogue, nil)
	}); err != nil {
		t.Fatalf("seed a customer's tenant: %v", err)
	}
	for _, role := range seededRoles(t, conn, customer) {
		have := slices.Clone([]string(role.Grants))
		slices.Sort(have)
		expect := []string{"*"}
		if role.Name == contracts.RoleMember {
			expect = nil
		}
		if strings.Join(have, " ") != strings.Join(expect, " ") {
			t.Errorf("role %q of a customer's tenant holds [%s], want [%s]",
				role.Name, strings.Join(have, " "), strings.Join(expect, " "))
		}
	}
}

// seededRoles reads the rows the seeder wrote back off the database, in the
// system transaction the seeder itself used: the assertion below is about the
// row, so it is read from the row and not from what produced it.
func seededRoles(t *testing.T, conn *db.Conn, tenant tenancy.Tenant) []contracts.Role {
	t.Helper()
	var got []contracts.Role
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Where("tenant_id = ?", tenant.ID).Order("name").Find(&got).Error
	})
	if err != nil {
		t.Fatalf("read the seeded roles back: %v", err)
	}
	return got
}

func roleNames(roles []contracts.Role) string {
	out := make([]string, 0, len(roles))
	for _, r := range roles {
		out = append(out, r.Name)
	}
	slices.Sort(out)
	return strings.Join(out, " ")
}
