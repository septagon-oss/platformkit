package auth_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestTheSeededRolesAreTheCompositionsAndNothingElse is the case that would
// have caught the defect this file was written for.
//
// An installation served for a night with an administrator holding
// billing:catalog and no billing module to exercise it against, and said so in
// a warning on every hour. Nothing asked the question the warning answered,
// because the operator grants were a literal in the application and the
// composition was somewhere else entirely.
//
// So this asks it: for a composition, what contracts.SeededRoles says the
// seeded roles are is compared, permission by permission, with the rows
// SeedRoles actually wrote — over two compositions that differ by one module,
// which is the difference the defect was invisible to. A permission in one and
// not the other fails, naming the composition, the role and the permission.
func TestTheSeededRolesAreTheCompositionsAndNothingElse(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	tasks := module.Module{Name: "task", Permissions: []module.Permission{{Key: "task:read"}}}
	tenants := module.Module{Name: "tenant", Permissions: []module.Permission{{Key: "tenant:manage", Operator: true}}}
	billing := module.Module{Name: "billing", Permissions: []module.Permission{{Key: "billing:catalog", Operator: true}}}

	for _, composition := range []struct {
		name string
		mods []module.Module
		// absent is the permission this composition does not compose the owner
		// of, so no role of it may name it however the seeder is called.
		absent string
	}{
		{"tenant, task and billing", []module.Module{tasks, tenants, billing}, ""},
		{"the same composition without billing", []module.Module{tasks, tenants}, "billing:catalog"},
		{"neither operator module", []module.Module{tasks}, "tenant:manage"},
	} {
		declared := module.Grants(composition.mods)
		tenant := tenancy.Tenant{ID: uuid.New(), Slug: "operator", Operator: true}
		want, err := contracts.SeededRoles(declared, nil, tenant)
		if err != nil {
			t.Fatalf("%s: what this composition seeds: %v", composition.name, err)
		}
		var got []contracts.Role
		err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			if err := auth.SeedRoles(ctx, tx, tenant, declared, nil); err != nil {
				return err
			}
			return tx.DB().Where("tenant_id = ?", tenant.ID).Order("name").Find(&got).Error
		})
		if err != nil {
			t.Fatalf("%s: seed: %v", composition.name, err)
		}
		if len(got) != len(want) {
			t.Fatalf("%s: seeded %d roles, and this composition's roles are %d: %+v", composition.name, len(got), len(want), got)
		}
		for i, role := range want {
			if got[i].Name != role.Name {
				t.Fatalf("%s: seeded the role %q where this composition's is %q", composition.name, got[i].Name, role.Name)
			}
			for _, p := range role.Grants {
				if !slices.Contains([]string(got[i].Grants), p) {
					t.Errorf("%s: role %q: this composition grants %q and the seeded role does not", composition.name, role.Name, p)
				}
			}
			for _, p := range got[i].Grants {
				if !slices.Contains([]string(role.Grants), p) {
					t.Errorf("%s: role %q: the seeded role grants %q and this composition does not", composition.name, role.Name, p)
				}
			}
			if composition.absent != "" && slices.Contains([]string(got[i].Grants), composition.absent) {
				t.Errorf("%s: role %q was seeded %q, which no module of it defines", composition.name, role.Name, composition.absent)
			}
		}
	}
}

// TestRepairTakesTheSeedersDeadGrantsAndLeavesTheTenantsOwn is the other half:
// the installations already seeded before the line above was true.
//
// The command lists before it removes, removes only the roles this module's
// seeder owns, and is idempotent. The role a customer made is left exactly as
// it was, undeclared grant and all — it was legal when it was written and what
// its author meant by it is not this command's to decide; the hourly sweep goes
// on saying so.
func TestRepairTakesTheSeedersDeadGrantsAndLeavesTheTenantsOwn(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "operator", Operator: true}
	was := []tenancy.Grant{{Permission: "role:manage"}, {Permission: "tenant:manage", Operator: true},
		{Permission: "billing:catalog", Operator: true}}
	now := []tenancy.Grant{{Permission: "role:manage"}, {Permission: "tenant:manage", Operator: true}}

	// The installation as the old seeder left it, and one role of the tenant's
	// own naming the same departed permission.
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return auth.SeedRoles(ctx, tx, tenant, was, []contracts.Role{
			{Name: "finance", Grants: contracts.Permissions{"role:manage"}}})
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.SetRole(ctx, tx, "finance", []string{"role:manage", "billing:catalog"}, was)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	for _, step := range []struct {
		remove bool
		want   []string
		admin  []string
	}{
		{false, []string{"billing:catalog"}, []string{"*", "billing:catalog", "tenant:manage"}},
		{true, []string{"billing:catalog"}, []string{"*", "tenant:manage"}},
		{true, nil, []string{"*", "tenant:manage"}},
	} {
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			found, err := auth.RepairSeededRoles(ctx, tx, svc, now, nil, step.remove)
			if err != nil {
				return err
			}
			if !slices.Equal(found["admin"], step.want) {
				t.Errorf("remove=%v: reported %v for admin, want %v", step.remove, found["admin"], step.want)
			}
			if _, touched := found["finance"]; touched {
				t.Errorf("remove=%v: reported a role the seeder never wrote: %v", step.remove, found)
			}
			roles, err := svc.Roles(ctx, tx)
			if err != nil {
				return err
			}
			for _, r := range roles {
				switch r.Name {
				case "admin":
					if !slices.Equal([]string(r.Grants), step.admin) {
						t.Errorf("remove=%v: admin grants %v, want %v", step.remove, r.Grants, step.admin)
					}
				case "finance":
					if !slices.Contains([]string(r.Grants), "billing:catalog") {
						t.Errorf("remove=%v: the repair took a grant out of the tenant's own role: %v", step.remove, r.Grants)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("remove=%v: %v", step.remove, err)
		}
	}
}

// TestARoleTheRepairCannotFinishIsLeftWhole is the case behind the one branch
// the narrowing needs: an initial role holding a dead grant of the seeder's
// beside a dead grant of the tenant's own.
//
// The application's literal named finance content:read, so the seeder wrote that
// one; an administrator added task:read while the module owning it was still
// composed, so that one is theirs. Both modules then leave. The repair cannot
// take the first without the second — SetRole goes through
// contracts.CheckedPermissions, which refuses any list still naming a permission
// no module defines — and the second is not its to take. So the role is left
// whole and out of either report: the listing `repair-roles` prints before it
// removes stops at the same line the write does, so a run that names nothing has
// not proved there is nothing anywhere, and the hourly sweep goes on naming both.
//
// The reachability probe is the administrator's own seeded operator grant, which
// the same run does remove: the case cannot pass by the repair doing nothing.
func TestARoleTheRepairCannotFinishIsLeftWhole(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "operator", Operator: true}
	was := []tenancy.Grant{{Permission: "role:manage"}, {Permission: "task:read"},
		{Permission: "content:read"}, {Permission: "billing:catalog", Operator: true}}
	now := []tenancy.Grant{{Permission: "role:manage"}}
	initial := []contracts.Role{{Name: "finance", Grants: contracts.Permissions{"content:read"}}}

	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return auth.SeedRoles(ctx, tx, tenant, was, initial)
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, e := svc.SetRole(ctx, tx, "finance", []string{"content:read", "task:read"}, was)
		return e
	}); err != nil {
		t.Fatalf("the administrator's own edit: %v", err)
	}

	var found, listed map[string][]string
	var roles []*contracts.Role
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var e error
		if listed, e = auth.RepairSeededRoles(ctx, tx, svc, now, initial, false); e != nil {
			return e
		}
		if found, e = auth.RepairSeededRoles(ctx, tx, svc, now, initial, true); e != nil {
			return e
		}
		roles, e = svc.Roles(ctx, tx)
		return e
	}); err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !slices.Equal(found["admin"], []string{"billing:catalog"}) {
		t.Fatalf("reported %v for admin, want [billing:catalog]", found["admin"])
	}
	if _, touched := found["finance"]; touched {
		t.Errorf("reported a role it cannot repair: %v", found)
	}
	if _, seen := listed["finance"]; seen || !slices.Equal(listed["admin"], []string{"billing:catalog"}) {
		t.Errorf("the listing a --remove previews reported %v; want admin [billing:catalog] and no finance", listed)
	}
	for _, r := range roles {
		switch r.Name {
		case "admin":
			if !slices.Equal([]string(r.Grants), []string{"*"}) {
				t.Errorf("admin grants %v, want [*]", r.Grants)
			}
		case "finance":
			if !slices.Equal([]string(r.Grants), []string{"content:read", "task:read"}) {
				t.Errorf("the repair wrote a role it could not finish: %v", r.Grants)
			}
		}
	}
}

// TestTheOperatorsOwnAdministratorCannotSayWhoWroteADeadGrant is the one row
// where "it never takes a grant the seeder did not write" is not true, put here
// so that the sentence the doc comments carry is a sentence something checks.
//
// The operator's own administrator is the only role the seeder writes named
// permissions into: the wildcard, and the operator permissions of the catalogue
// it was handed. Once the module owning one of those leaves, neither the row nor
// the catalogue records whether the departed permission was an operator one, so
// the seeder's grant and one an operator typed into the roles screen are the
// same bytes and the branch takes both. Every other row is narrowed by
// contracts.SeededGrants and keeps what a hand put there — the cases in
// review2_repair_narrowing_test.go are those.
//
// It costs nothing that was granting anything: each grant taken is dead by
// definition, and the wildcard is still beside them granting every ordinary
// permission. The reachability probe is the initial role finance, whose own dead
// grant the same run removes, so this cannot pass by the repair doing nothing.
func TestTheOperatorsOwnAdministratorCannotSayWhoWroteADeadGrant(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	operator := tenancy.Tenant{ID: uuid.New(), Slug: "operator", Operator: true}
	seedForRepair(t, conn, operator)
	handEdit(t, conn, svc, operator, contracts.RoleAdmin,
		[]string{contracts.Wildcard, "task:read", "tenant:manage", "billing:catalog"})

	found := repair(t, conn, svc, operator, true)
	took := slices.Sorted(slices.Values(found[contracts.RoleAdmin]))
	if !slices.Equal(took, []string{"billing:catalog", "task:read"}) {
		t.Errorf("reported %v for the administrator, want the seeder's billing:catalog and the"+
			" hand's task:read, which this row cannot tell apart", took)
	}
	if grants := roleGrants(t, conn, svc, operator, contracts.RoleAdmin); !slices.Equal(grants,
		[]string{contracts.Wildcard, "tenant:manage"}) {
		t.Errorf("the administrator holds %v, want the wildcard and the one operator permission"+
			" a composed module still declares", grants)
	}
	if grants := roleGrants(t, conn, svc, operator, "finance"); !slices.Equal(grants, []string{"role:manage"}) {
		t.Errorf("finance holds %v, want [role:manage]: the run removed nothing it was meant to", grants)
	}
}

// TestAnInitialRoleIsHeldToItsRulesHoweverItsLiteralSpellsThem is those two
// rules read as the rules the doc comment says they are — about the permission,
// not about the way one spelling happens to write it.
//
// SeededRoles refuses an initial role naming the wildcard or an operator
// permission and then hands the same list to CheckedPermissions, which
// lower-cases and trims before it decides anything. So a literal one space or
// one capital away from canonical walked past both rules and was seeded: " * "
// reached CheckedPermissions, which takes the wildcard by design, and
// " Tenant:Manage " reached it in the operator's own tenant, where every
// operator permission is declared and so taken. A rule that held for one of the
// spellings a person plausibly types and not for the other four is not a rule,
// and the row it wrote could not be told apart from the refused one afterwards.
//
// Every refusal below is one only those two rules can produce — each of these
// spellings is one CheckedPermissions lets through, which is why the case runs
// over the operator's tenant as well as a customer's (a customer's is refused
// "tenant:manage" by the other rule, with a different sentence) and why it reads
// the sentence back. The accepted half is the probe that this normalises and does
// not tighten: the same letters around an ordinary permission still seed, spelled
// the way the row holds them, which is what SeededGrants then matches on.
func TestAnInitialRoleIsHeldToItsRulesHoweverItsLiteralSpellsThem(t *testing.T) {
	for _, tenant := range []tenancy.Tenant{
		{ID: uuid.New(), Slug: "acme"},
		{ID: uuid.New(), Slug: "operator", Operator: true},
	} {
		for _, spelled := range []string{"*", " * ", "\t*\n", "tenant:manage", " Tenant:Manage ", "TENANT:MANAGE"} {
			roles, err := contracts.SeededRoles(composed,
				[]contracts.Role{{Name: "finance", Grants: contracts.Permissions{spelled, "task:read"}}}, tenant)
			switch {
			case err == nil:
				t.Errorf("%s: an initial role granting %q seeded %v: neither the wildcard nor an"+
					" operator permission belongs in one, however the literal spells it", tenant.Slug, spelled, roles)
				continue
			case !errors.Is(err, crud.ErrInvalid):
				t.Errorf("%s: initial role granting %q: %v, want ErrInvalid", tenant.Slug, spelled, err)
			case !strings.Contains(err.Error(), "cannot grant"):
				t.Errorf("%s: initial role granting %q refused with %v, which is not the initial-role"+
					" rule's refusal: what seeded that grant is the rule that should have refused it", tenant.Slug, spelled, err)
			}
		}
	}
	roles, err := contracts.SeededRoles(composed,
		[]contracts.Role{{Name: " Finance ", Grants: contracts.Permissions{" Task:Read "}}},
		tenancy.Tenant{ID: uuid.New(), Slug: "acme"})
	if err != nil {
		t.Fatalf("an ordinary permission spelled with capitals and spaces: %v", err)
	}
	finance := slices.IndexFunc(roles, func(r contracts.Role) bool { return r.Name == "finance" })
	if finance < 0 {
		t.Fatalf("the seeder wrote %v with no finance in it", roles)
	}
	if !slices.Equal([]string(roles[finance].Grants), []string{"task:read"}) {
		t.Errorf(`finance holds %v, want [task:read]: " Task:Read " is the same permission`+
			" spelled differently, and the row is what CheckedPermissions makes of it", roles[finance].Grants)
	}
}
