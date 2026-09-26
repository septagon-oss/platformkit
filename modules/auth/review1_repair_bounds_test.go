package auth_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// was is the catalogue the older seeder was handed: the installation still
// composed the module that owned billing:catalog. now is the same installation
// after that module left, which is the state the repair exists for.
var (
	repairWas = []tenancy.Grant{{Permission: "role:manage"}, {Permission: "task:read"},
		{Permission: "content:read"},
		{Permission: "tenant:manage", Operator: true}, {Permission: "billing:catalog", Operator: true}}
	repairNow = []tenancy.Grant{{Permission: "role:manage"}, {Permission: "task:read"},
		{Permission: "tenant:manage", Operator: true}}
)

// TestTheRepairOfOneTenantIsNotTheRepairOfAnother pins the tenancy of the new
// command: RepairSeededRoles runs in one tenant's request transaction, so the
// rows it reports and the rows it removes are that tenant's and no other's.
//
// Both tenants are seeded the same dead grant by the same older catalogue. The
// repair is then run, with --remove, inside the second tenant's transaction
// only. The first tenant's administrator has to come out of it holding exactly
// what it held, and the second's report may not name a role of the first's.
func TestTheRepairOfOneTenantIsNotTheRepairOfAnother(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	first := tenancy.Tenant{ID: uuid.New(), Slug: "services-law", Operator: true}
	second := tenancy.Tenant{ID: uuid.New(), Slug: "pets", Operator: true}

	for _, tenant := range []tenancy.Tenant{first, second} {
		err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return auth.SeedRoles(ctx, tx, tenant, repairWas, nil)
		})
		if err != nil {
			t.Fatalf("seed %s: %v", tenant.Slug, err)
		}
	}

	var found map[string][]string
	err := db.Run(tenancy.WithTenant(t.Context(), second), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var e error
		found, e = auth.RepairSeededRoles(ctx, tx, svc, repairNow, nil, true)
		return e
	})
	if err != nil {
		t.Fatalf("repair %s: %v", second.Slug, err)
	}
	// The probe that the case reached the refusal at all: the tenant it did run
	// in has to have had its own grant taken away.
	if !slices.Equal(found["admin"], []string{"billing:catalog"}) {
		t.Fatalf("the repair of %s reported %v; it was supposed to find its own dead grant", second.Slug, found)
	}
	if grants := adminGrants(t, conn, svc, second); slices.Contains(grants, "billing:catalog") {
		t.Errorf("the repair of %s left its own dead grant: %v", second.Slug, grants)
	}
	if grants := adminGrants(t, conn, svc, first); !slices.Contains(grants, "billing:catalog") {
		t.Errorf("repairing %s reached into %s's administrator: %v", second.Slug, first.Slug, grants)
	}
}

// TestTheRepairLeavesAGrantTheSeederNeverWrote is the brief's third
// deliverable, read as it is written: the command "never touches a tenant's
// custom roles, only grants the seeder itself created".
//
// A role's *name* belonging to the seeder is not the same as a *grant* being
// the seeder's. The built-in member role is seeded granting nothing at all, and
// the built-in administrator is seeded the wildcard plus the composition's
// operator permissions — so an ordinary permission sitting in either of them
// was put there by an administrator through SetRole, while the module that
// owned it was still composed and the write was legal. When that module leaves,
// the grant is exactly as dead as the one on a role the tenant named itself,
// and exactly as much the customer's: it is what somebody decided, and the
// hourly sweep is the only thing entitled to say so.
//
// The reachability probe is the administrator's own seeded grant, which the
// same run has to remove: the case cannot pass by the repair doing nothing.
func TestTheRepairLeavesAGrantTheSeederNeverWrote(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "services-law", Operator: true}

	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return auth.SeedRoles(ctx, tx, tenant, repairWas, nil)
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// What an administrator did while the module was still composed: gave the
	// built-in member role a permission the seeder had left it without. Through
	// SetRole, which is the only door there is, so this is a write the product
	// permitted.
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, e := svc.SetRole(ctx, tx, contracts.RoleMember, []string{"task:read", "billing:catalog"}, repairWas)
		return e
	}); err != nil {
		t.Fatalf("the administrator's own edit: %v", err)
	}

	var found map[string][]string
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var e error
		found, e = auth.RepairSeededRoles(ctx, tx, svc, repairNow, nil, true)
		return e
	}); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !slices.Equal(found["admin"], []string{"billing:catalog"}) {
		t.Fatalf("the repair reported %v; it was supposed to find the administrator's seeded dead grant", found)
	}
	if grants := adminGrants(t, conn, svc, tenant); slices.Contains(grants, "billing:catalog") {
		t.Fatalf("the repair left the grant its own seeder wrote: %v", grants)
	}

	var member []string
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		roles, e := svc.Roles(ctx, tx)
		if e != nil {
			return e
		}
		for _, r := range roles {
			if r.Name == contracts.RoleMember {
				member = []string(r.Grants)
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("read the member role back: %v", err)
	}
	if !slices.Contains(member, "billing:catalog") {
		t.Errorf("the repair removed %q from the member role: %v. The seeder writes that role with no grants at all,"+
			" so every permission in it is a customer's decision and not a grant the seeder created", "billing:catalog", member)
	}
}

// TestTheRepairReportsWhenTheApplicationsInitialRolesAreStale is the other
// bound: the command has to work in the installation it was written for.
//
// An application names its initial roles in one literal and passes the same one
// to SeedRoles and to the repair. When a module leaves the composition, the
// grant that left with it is in that literal too — that is the same deploy that
// creates the rows this command repairs. RepairSeededRoles asks
// contracts.SeededRoles for the names it owns, and SeededRoles refuses an
// initial role naming an undeclared permission, so the command answers the
// operator with the refusal rather than with the report it exists to print, and
// removes nothing anywhere.
//
// The report is the assertion, so the case passes only when the command does
// its job: the administrator's dead grant is named and taken away.
func TestTheRepairReportsWhenTheApplicationsInitialRolesAreStale(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "services-law", Operator: true}
	// The application's initial roles, as they were written when the module was
	// composed and as an installation seeded before this change still has them.
	stale := []contracts.Role{{Name: "finance", Grants: contracts.Permissions{"content:read"}}}

	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return auth.SeedRoles(ctx, tx, tenant, repairWas, stale)
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	var found map[string][]string
	err = db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var e error
		found, e = auth.RepairSeededRoles(ctx, tx, svc, repairNow, stale, false)
		return e
	})
	if err != nil {
		t.Fatalf("the repair refused the installation it exists for: %v (ErrInvalid: %v)", err, errors.Is(err, fault.ErrInvalid))
	}
	if !slices.Equal(found["admin"], []string{"billing:catalog"}) {
		t.Errorf("the repair reported %v for admin, want [billing:catalog]", found["admin"])
	}
	if !slices.Equal(found["finance"], []string{"content:read"}) {
		t.Errorf("the repair reported %v for finance, want [content:read]", found["finance"])
	}
}

// TestTwoRepairsOfOneRoleAreOneWrite pins the concurrency of the new command
// against the lock it inherits.
//
// RepairSeededRoles reads the tenant's roles *before* SetRole takes the
// tenant's administration lock, so two operators running the command at once
// both compute the same removal from the same stale read. What makes that safe
// is that SetRole re-reads under the lock and writes nothing when the list is
// already what it wants: the row ends correct, and the audit holds one
// auth.role_set and not two of the same change.
func TestTwoRepairsOfOneRoleAreOneWrite(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "services-law", Operator: true}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return auth.SeedRoles(ctx, tx, tenant, repairWas, nil)
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			errs <- db.Run(tenancy.WithTenant(t.Context(), tenant), conn,
				func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					_, e := auth.RepairSeededRoles(ctx, tx, svc, repairNow, nil, true)
					return e
				})
		}()
	}
	close(start)
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("a concurrent repair: %v", err)
		}
	}

	if grants := adminGrants(t, conn, svc, tenant); slices.Contains(grants, "billing:catalog") {
		t.Errorf("two repairs left the dead grant: %v", grants)
	}
	var n int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*) FROM platformkit_outbox WHERE name = $1 AND tenant_id = $2",
		contracts.EventRoleSet, tenant.ID).Scan(&n); err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	if n != 1 {
		t.Errorf("two repairs of one role published %d %s events, want 1", n, contracts.EventRoleSet)
	}
}

// TestTheRepairIsInTheAuditOrItIsNotAWrite pins what modules/auth/repair.go
// claims about the removal: it goes through SetRole, so domain state and the
// outbox row commit together and a run that only lists writes neither.
//
// A repair no audit could see would be the silent edit the command exists not
// to be, and a repair whose event committed without its row — or whose row
// committed without its event — would be worse than either.
func TestTheRepairIsInTheAuditOrItIsNotAWrite(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "services-law", Operator: true}
	if err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return auth.SeedRoles(ctx, tx, tenant, repairWas, nil)
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	events := func() int {
		t.Helper()
		var n int
		if err := admin.QueryRowContext(t.Context(),
			"SELECT count(*) FROM platformkit_outbox WHERE name = $1 AND tenant_id = $2",
			contracts.EventRoleSet, tenant.ID).Scan(&n); err != nil {
			t.Fatalf("read the outbox: %v", err)
		}
		return n
	}

	// Listing reports and writes nothing at all.
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		found, e := auth.RepairSeededRoles(ctx, tx, svc, repairNow, nil, false)
		if e == nil && !slices.Equal(found["admin"], []string{"billing:catalog"}) {
			t.Errorf("the listing reported %v, so this case never reached a removal", found)
		}
		return e
	}); err != nil {
		t.Fatalf("list: %v", err)
	}
	if n := events(); n != 0 {
		t.Errorf("listing published %d %s events", n, contracts.EventRoleSet)
	}

	// A removal whose transaction rolls back leaves neither the row nor the event.
	back := errors.New("the caller rolled back")
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, e := auth.RepairSeededRoles(ctx, tx, svc, repairNow, nil, true); e != nil {
			return e
		}
		return back
	})
	if !errors.Is(err, back) {
		t.Fatalf("rolled-back removal: %v", err)
	}
	if n := events(); n != 0 {
		t.Errorf("a rolled-back removal left %d %s events", n, contracts.EventRoleSet)
	}
	if grants := adminGrants(t, conn, svc, tenant); !slices.Contains(grants, "billing:catalog") {
		t.Errorf("a rolled-back removal still changed the administrator: %v", grants)
	}

	// The committed removal is one row and one event, together.
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, e := auth.RepairSeededRoles(ctx, tx, svc, repairNow, nil, true)
		return e
	}); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if n := events(); n != 1 {
		t.Errorf("the removal published %d %s events, want 1", n, contracts.EventRoleSet)
	}
	if grants := adminGrants(t, conn, svc, tenant); slices.Contains(grants, "billing:catalog") {
		t.Errorf("the removal left the dead grant: %v", grants)
	}
}

func adminGrants(t *testing.T, conn *db.Conn, svc contracts.Service, tenant tenancy.Tenant) []string {
	t.Helper()
	var out []string
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		roles, e := svc.Roles(ctx, tx)
		if e != nil {
			return e
		}
		for _, r := range roles {
			if r.Name == contracts.RoleAdmin {
				out = []string(r.Grants)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read %s's administrator: %v", tenant.Slug, err)
	}
	return out
}
