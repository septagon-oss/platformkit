package contracts

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	// The refusals come from kit/fault rather than kit/crud: this package names no
	// transaction, and kit/crud re-exports the same values, so a caller that already
	// wrote crud.ErrInvalid still matches what is returned here.
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

var roleName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// MaxRoleName is how long a role name may be. It is the bound the roles route's
// path parameter already declares and the screen's control already advertises,
// written where the rule lives so that SeedRoles and any other caller are held
// to the same one — a name nothing bounded was a column somebody could fill.
const MaxRoleName = 64

// ValidRoleName normalizes a role name and checks the identifier shared by
// role administration and tenant provisioning.
func ValidRoleName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !roleName.MatchString(name) {
		return "", fmt.Errorf("%w: role %q is not a lower-case identifier", fault.ErrInvalid, name)
	}
	if len(name) > MaxRoleName {
		return "", fmt.Errorf("%w: a role name is at most %d characters, and %q is %d",
			fault.ErrInvalid, MaxRoleName, name, len(name))
	}
	return name, nil
}

// CheckedAdministration refuses one write a tenant cannot undo from inside the
// product: the one that takes PermissionRoleManage away from the last role that
// grants it.
//
// After that write nobody in the tenant can change a role again — not through
// an admin screen and not through PUT /api/v1/auth/roles/{name}. Signing in as
// the operator does not undo it either — an ordinary session does not cross
// tenants; the separately authorized cross-tenant invite route below does.
//
// One supported way back in survives, and it is worth being exact about what it
// recovers. POST /api/v1/tenant/tenants/{id}/invite runs in a system
// transaction and provisions somebody holding a role the application names —
// "admin" in this repository's composition, and the route exists at all only
// where an application wires that capability. So it recovers a tenant whose
// people lost their grants. It does not recover a tenant where the named role
// is itself the one that was emptied, because the role it hands out is that
// role. And it recovers nothing in the operator's own tenant, because it
// declares tenant:manage as an operator permission, held through that tenant's
// own roles. There, what is left is SQL.
//
// It is the last grant leaving, and not a demand that one exist: a write in a
// tenant whose roles already grant it to nobody passes, because refusing there
// would take away the repair as well as the damage. others is read only on the
// path that can refuse, so an ordinary write does not pay for that read — it
// does pay for the caller holding its reads and its write together, which is
// one statement whatever it is writing. See internal.SetRole, which takes an
// advisory lock on the tenant before its first read; without it two
// administrators standing down at once both pass this and the tenant ends with
// neither.
//
// # What this does not close
//
// It counts roles. It does not count the people holding them, because who holds
// a role is the user module's table and this module does not read it.
//
// The user module has a floor of its own over the same property, and the two do
// not compose into it. This one asks whether a role still grants role:manage;
// that one asks whether a person still holds a role that does. Somebody who can
// sign in holds a role that grants role:manage — the property both are for — is
// asked by neither. Two sequences reach a locked-out tenant with every
// individual write permitted and no concurrency at all. Only the first is a case
// in this repository; the second is described here and has no test behind it:
//
//   - One write, and the tested one. apps.platformkit's
//     TestTheTwoFloorsStillDoNotComposeIntoOneInvariant asserts it and fails the
//     day the hole closes. Create a role granting role:manage and give it to
//     nobody: nothing is taken away, so both floors allow it. Then empty the role
//     everybody actually holds. This check sees the new role still granting and
//     allows. Everybody now holds a role that grants nothing.
//   - Two writes, in either order, and no case. Each of two people holds a
//     different administering role. Stripping the first one's roles passes the
//     user module's floor because the second is still there; emptying that second
//     role passes this one because the first role still grants. Nobody can
//     administer.
//
// Serializing the two does not help and was never meant to. The advisory lock
// both modules take — see internal.administrationLock — stops two concurrent
// writes each verifying what the other is falsifying, which is a real defect
// and a different one. Writes that are each individually correct are still each
// individually correct in a queue.
//
// # The shape of the fix, which is not in this change
//
// Ask the composed question on this side too, the way the user module already
// asks it on theirs: it takes the roles that grant role:manage from this module
// and counts the people who can still sign in holding one. This module changes
// what a role grants and counts only roles, which is the asymmetry. The
// symmetric answer is a narrow capability the application supplies — does any
// person who can still sign in hold one of these roles — asked here against the
// state this write would leave, exactly as the permission catalogue is handed
// to SetRole rather than looked up. Then one property is checked once instead
// of two halves of it checked separately. That crosses two modules and a
// composition and deserves its own review.
//
// # Where a lockout leaves you
//
// A customer's tenant is recoverable: the operator can still invite an
// administrator into it through the control plane, which is the qualified
// statement above. The operator's own tenant is not, because the control plane
// is guarded by operator permissions held through that tenant's own roles, so
// tenant administration and the price list go with it.
//
// The roles path is not the only way there, and this comment would be
// overstating its own importance if it implied otherwise:
// POST /api/v1/tenant/tenants/{id}/suspend against the operator's own tenant is
// one request, after which every operator host answers as though no site were
// served, and modules/tenant has no route that reverses it. Neither floor
// touches that one.
//
// So this is one door of several, closed. A tenant is only as reachable as the
// person who can still sign in and administer it, and nothing in this
// repository yet checks that such a person exists.
func CheckedAdministration(name string, was, want Permissions, others func() ([]*Role, error)) error {
	manage := tenancy.Grant{Permission: PermissionRoleManage}
	if !Grants(was, manage) || Grants(want, manage) {
		return nil
	}
	rest, err := others()
	if err != nil {
		return err
	}
	for _, other := range rest {
		if other.Name != name && Grants(other.Grants, manage) {
			return nil
		}
	}
	return fmt.Errorf("%w: %q is the last role that grants %s, and a tenant that grants it to no role cannot change its roles again",
		fault.ErrInvalid, name, PermissionRoleManage)
}

// CheckedPermissions normalises a permission list and refuses the two ways one can be
// wrong. The result is sorted and deduplicated, so "the same permissions in
// another order" is the same value and SetRole can tell that nothing changed.
func CheckedPermissions(permissions []string, declared []tenancy.Grant, tenant tenancy.Tenant) (Permissions, error) {
	out := make(Permissions, 0, len(permissions))
	for _, p := range permissions {
		p = strings.ToLower(strings.TrimSpace(p))
		switch {
		case p == "":
			continue
		case slices.Contains(out, p):
			continue
		case p == Wildcard:
			// The wildcard is not in the declared list and never will be: it is
			// the rule rather than a permission, and it grants every ordinary
			// permission and no operator one. See contracts.Grants.
			out = append(out, p)
			continue
		}
		i := slices.IndexFunc(declared, func(g tenancy.Grant) bool { return g.Permission == p })
		switch {
		case i < 0:
			return nil, fmt.Errorf("%w: no module defines the permission %q, so a role naming it would grant nothing",
				fault.ErrInvalid, p)
		case declared[i].Operator && !tenant.Operator:
			return nil, fmt.Errorf("%w: %q belongs to the operator of this installation, and %s is not it",
				fault.ErrInvalid, p, tenant.Slug)
		}
		out = append(out, p)
	}
	slices.Sort(out)
	return out, nil
}

// OperatorGrants is the permissions of a catalogue that belong to the operator
// of the installation: the ones no wildcard satisfies, which a role has to name
// to hold. Sorted, so the row a seeder writes does not depend on the order the
// modules happened to be composed in.
//
// declared is the composition's own catalogue (kit/module.Grants), which is why
// this answers the question the hourly warning used to ask an hour late: a
// permission is in it exactly when a composed module defines it.
func OperatorGrants(declared []tenancy.Grant) []string {
	out := make([]string, 0, len(declared))
	for _, g := range declared {
		if g.Operator {
			out = append(out, g.Permission)
		}
	}
	slices.Sort(out)
	return out
}

// SeededGrants narrows the grants of one role that no composed module declares
// to the ones this module's seeder is the writer of. gone is what
// internal.Undeclared found in role — a permission that left with its module and
// grants nothing now; the question here is who put it there, because a role's
// name belonging to the seeder is not the same as a grant in it being the
// seeder's.
//
// SeededRoles above is the whole of what the seeder writes, so this is the same
// three decisions read backwards:
//
//   - The built-in administrator, in the operator's own tenant: the seeder wrote
//     the wildcard and the operator permissions of the catalogue it was handed,
//     and a departed permission was one of those. A name beside the wildcard is
//     the only kind that can add anything to that row — the wildcard already
//     grants every ordinary permission — and an operator permission is what the
//     seeder puts there. So a dead grant is taken back, unless somebody has
//     since taken the wildcard off the row, in which case the ordinary names in
//     it are theirs and mean something and none of it is touched.
//   - An initial role the application names: the seeder wrote the list in that
//     literal, so a dead grant that literal still names is the seeder's. One it
//     does not name was added through SetRole by whoever administers the tenant.
//   - Anything else, the built-in member included: the seeder wrote it holding
//     nothing, so every permission in it is somebody's decision. A customer's
//     tenant is not the operator's either, so its administrator's row is only
//     ever seeded the wildcard and nothing in it is the seeder's.
//
// Limits. In the operator's own tenant the row does not record which writer put
// a permission in it, and an operator may have named one by hand — an ordinary
// permission as much as an operator one, since once the module owning it leaves
// nothing says any more which it was; the seeder and that hand are
// indistinguishable here, so the first branch takes both. It is dead either way,
// the wildcard beside it still grants every ordinary permission, the only tenant
// it can happen in is the operator's own, and the command that reads this lists
// before it removes so that a person decides. What is left over —
// every dead grant in every other role — stays where it is and goes on being
// reported by the hourly sweep, which is the only thing entitled to say what its
// author meant by it.
func SeededGrants(role Role, gone []string, defaults []Role, tenant tenancy.Tenant) []string {
	if role.Name == RoleAdmin {
		if !tenant.Operator || !slices.Contains(role.Grants, Wildcard) {
			return nil
		}
		return slices.Clone(gone)
	}
	i := slices.IndexFunc(defaults, func(d Role) bool {
		name, err := ValidRoleName(d.Name)
		return err == nil && name == role.Name
	})
	if i < 0 {
		return nil
	}
	ours := make([]string, 0, len(gone))
	for _, p := range gone {
		for _, named := range defaults[i].Grants {
			if strings.ToLower(strings.TrimSpace(named)) == p {
				ours = append(ours, p)
				break
			}
		}
	}
	return ours
}

// SeededRoles is what a composition provisions a new tenant with: the built-in
// administrator, the built-in member, and the initial roles the application
// names, each with the grants they are created holding.
//
// It is the whole of the seeder's decision and it opens no database, so an
// application can ask it before it has one — which is the point of it being
// here. SeedRoles writes exactly this and nothing else, so "what the seeder
// produces" and "what the composition says it should" are one function and
// cannot drift; the repair that removes a grant an older seeder wrote reads it
// for the role names it is allowed to touch.
//
// The administrator's operator grants are OperatorGrants(declared) and nothing
// else. A product that does not compose the module owning a permission never
// seeds it: a grant is only ever as wide as the composition. The wildcard is
// beside them rather than filtered with them — it is the rule and not a
// permission (see Grants), and a filter applied to it would take every ordinary
// grant away from every administrator in the installation.
//
// An initial role is a customer-shaped role and is held to two rules the
// administrator's is not. It may not name the wildcard, because a second role
// granting everything is an administrator nobody called one; and it may not
// name an operator permission, in the operator's own tenant either, because the
// one role that holds those is the one this function builds. Everything else
// goes through CheckedPermissions, which is where "no module defines it" is
// refused — the rule that used to be a warning an hour later.
func SeededRoles(declared []tenancy.Grant, defaults []Role, tenant tenancy.Tenant) ([]Role, error) {
	operator := OperatorGrants(declared)
	admin := Permissions{Wildcard}
	if tenant.Operator {
		admin = append(admin, operator...)
	}
	roles := map[string]Permissions{RoleAdmin: admin, RoleMember: {}}
	seen := map[string]bool{RoleAdmin: true}
	for _, role := range defaults {
		name, err := ValidRoleName(role.Name)
		if err != nil {
			return nil, err
		}
		if seen[name] || len(role.Grants) == 0 {
			return nil, fmt.Errorf("%w: initial role %q is reserved, duplicated or grants nothing", fault.ErrInvalid, name)
		}
		for _, p := range role.Grants {
			if p == Wildcard || slices.Contains(operator, p) {
				return nil, fmt.Errorf("%w: initial role %q cannot grant %q", fault.ErrInvalid, name, p)
			}
		}
		grants, err := CheckedPermissions(role.Grants, declared, tenant)
		if err != nil {
			return nil, fmt.Errorf("initial role %q: %w", name, err)
		}
		seen[name], roles[name] = true, grants
	}
	out := make([]Role, 0, len(roles))
	for _, name := range slices.Sorted(maps.Keys(roles)) {
		out = append(out, Role{Name: name, Grants: roles[name]})
	}
	return out, nil
}
