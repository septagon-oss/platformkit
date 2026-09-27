package main

import (
	"context"
	"slices"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
)

// TestACustomerTenantIsSeededTheWildcardAndNoOperatorGrant pins the half of the
// brief's invariant that nothing in this repository checks at the seam the
// defect actually lived in.
//
// "A grant is only ever as wide as the composition" has two halves, and the
// branch is loud about the first: the operator's own administrator is seeded the
// operator permissions of the composed modules and nothing beyond them. The
// second half is the one that would be a privilege escalation rather than a dead
// row, and it is silent in the application's own suite. contracts.Grants answers
// an operator permission only by naming it — "letting the wildcard answer for an
// operator permission would hand every customer the installation" — so an
// operator permission seeded into a tenant that is not the operator's is a live
// grant of the control plane to a customer's administrator, not inert bytes. The
// seeder guards it with one `if tenant.Operator`, and the composition's hook is
// what hands that seeder the tenant and the catalogue.
//
// The module's own case (TestRoleProvisioningNeedsOnlyItsTransaction) checks the
// branch over a synthetic catalogue of one operator permission. apps/platformkit's
// TestTheBootstrapSeedsWhatThisFileComposes says out loud that it runs against
// the operator's tenant only, "so a case run against any other tenant would pass
// on an empty list". This is that other tenant: created by the real hook inside
// the real composition, read back through the composed service, and compared
// with the whole operator catalogue of this file's own modules.
//
// The reachability probe asks only for what the correct code prints: the same
// run reads the operator's tenant back and requires it to hold an operator
// permission of the same catalogue. A hook that computed nothing, a composition
// that composed no operator permission and a catalogue that arrived empty all
// fail that check rather than passing on emptiness.
func TestACustomerTenantIsSeededTheWildcardAndNoOperatorGrant(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	c := compose(cfg)

	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// The installation's own tenant, which the bootstrap created through the
	// same hook, and a customer created the way the control plane creates one.
	var operator, customer tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var e error
		operator, e = c.tenants.ByHost(ctx, tx, acmeHost)
		if e != nil {
			return e
		}
		if _, e = c.tenants.Create(ctx, tx, tenantcontracts.NewTenant{
			Slug: "globex", Name: "Globex", Host: globexHost,
		}); e != nil {
			return e
		}
		customer, e = c.tenants.ByHost(ctx, tx, globexHost)
		return e
	})
	if err != nil {
		t.Fatalf("create the customer tenant: %v", err)
	}
	if !operator.Operator {
		t.Fatalf("%s is not the operator's tenant, so nothing here would be widened by a leak", operator.Slug)
	}
	if customer.Operator {
		t.Fatalf("%s came back as the operator's tenant, which is the wrong world for this case", customer.Slug)
	}

	declared := module.Grants(c.modules)
	operatorPermissions := authcontracts.OperatorGrants(declared)
	if len(operatorPermissions) == 0 {
		t.Fatal("this composition declares no operator permission: the case has nothing to leak")
	}

	roles := func(tenant tenancy.Tenant) map[string][]string {
		t.Helper()
		out := map[string][]string{}
		err := db.Run(httpx.WithConn(tenancy.WithTenant(t.Context(), tenant), conn), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				held, e := c.auth.Roles(ctx, tx)
				if e != nil {
					return e
				}
				for _, r := range held {
					out[r.Name] = []string(r.Grants)
				}
				return nil
			})
		if err != nil {
			t.Fatalf("read the roles of %s: %v", tenant.Slug, err)
		}
		return out
	}

	// The probe: the operator's own administrator really is seeded the wide
	// list, so the assertions below are not the silence of an empty catalogue.
	held := roles(operator)
	admin := held[authcontracts.RoleAdmin]
	widened := 0
	for _, p := range operatorPermissions {
		if slices.Contains(admin, p) {
			widened++
		}
	}
	if widened == 0 {
		t.Fatalf("%s's administrator holds %v and none of the %d operator permissions this composition"+
			" declares, so nothing here could show a leak", operator.Slug, admin, len(operatorPermissions))
	}

	// The invariant, for a tenant that is not the operator's.
	held = roles(customer)
	if !slices.Equal(held[authcontracts.RoleAdmin], []string{authcontracts.Wildcard}) {
		t.Errorf("%s's administrator is seeded %v, want the wildcard alone: an operator permission in a"+
			" customer's administrator is a live grant of the control plane, because contracts.Grants"+
			" answers an operator permission only by naming it", customer.Slug, held[authcontracts.RoleAdmin])
	}
	for name, grants := range held {
		for _, p := range grants {
			if slices.Contains(operatorPermissions, p) {
				t.Errorf("%s: role %q is seeded %q, an operator permission, and %s is not the operator's tenant",
					customer.Slug, name, p, customer.Slug)
			}
			if !slices.ContainsFunc(declared, func(g tenancy.Grant) bool { return g.Permission == p }) &&
				p != authcontracts.Wildcard {
				t.Errorf("%s: role %q is seeded %q, which no module of this composition defines; the seeded"+
					" grant set is the composition's and nothing else", customer.Slug, name, p)
			}
		}
	}
}
