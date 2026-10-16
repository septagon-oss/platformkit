package contracts

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	// The refusals come from kit/fault rather than kit/crud: this package names no
	// transaction, and kit/crud re-exports the same values, so a caller that already
	// wrote crud.ErrInvalid still matches what is returned here.
	"github.com/septagon-oss/platformkit/kit/fault"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
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

// CheckedAdministration is this module's door onto the one rule, which lives in
// the user module as usercontracts.CheckedAdministration: the write that would
// leave this tenant with nobody who can sign in and administer it.
//
// What it adds to that rule is this module's own share. The rule counts people;
// this door works out who the people would be on both sides of a change to what
// a role grants, and answers for the sentence the refusal finishes with.
//
// The gate in front of it is about this row alone and stays cheap: a write that
// does not remove PermissionRoleManage from the role it writes can only grow the
// set of roles that grant it, so it is never the one that takes it away, and it
// pays for neither of the two reads below.
//
// Behind the gate the question is the composed one, which is what this door did
// not ask until now. It used to ask how many of the tenant's roles would still
// grant role:manage — a number that a role held by nobody satisfies, and did:
// create a role granting it, give it to nobody, then empty the role every
// administrator actually holds, and both writes were allowed and the tenant was
// locked. Now the roles that grant are turned into the people who hold them, by
// asking holders for each side's names, and the answer is the same answer the
// user module's three doors get, because both come out of one rule and one
// predicate (User.CanAdminister, which is what Service.Holders applies).
//
// roles and holders are read under the caller's lock — internal.SetRole takes the
// tenant's advisory key before its first read — and both are asked about the
// state this write would leave, not the one it found: name's grants are replaced
// by want before either side is decided. A role name nobody holds is legal and is
// exactly the state this door now sees.
//
// It refuses the last grant leaving, and not a demand that one exist: a write in
// a tenant whose people have already lost every grant passes, because refusing
// there would refuse the repair with the damage. The exception is the write that
// leaves no role granting role:manage at all (len(after) == 0 below), which is
// refused even then: while a granting role exists, an unaccepted invitation is
// still a thread somebody can pull, and emptying the last granting role cuts it.
// usercontracts.CheckedAdministration carries the whole argument, including who
// can still repair a tenant from outside it and which tenant cannot be repaired
// at all.
func CheckedAdministration(name string, was, want Permissions,
	roles func() ([]*Role, error), holders func([]string) ([]uuid.UUID, error)) error {
	manage := tenancy.Grant{Permission: PermissionRoleManage}
	if !Grants(was, manage) || Grants(want, manage) {
		return nil
	}
	every, err := roles()
	if err != nil {
		return err
	}
	var before, after []string
	for _, role := range every {
		grants := role.Grants
		if role.Name == name {
			grants = want
		}
		if Grants(role.Grants, manage) {
			before = append(before, role.Name)
		}
		if Grants(grants, manage) {
			after = append(after, role.Name)
		}
	}
	reached, err := holders(before)
	if err != nil {
		return err
	}
	left, err := holders(after)
	if err != nil {
		return err
	}
	leaving := fmt.Sprintf("role %q is the last role that grants %s", name, PermissionRoleManage)
	if len(after) > 0 {
		leaving += " that any active person holds"
	}
	return usercontracts.CheckedAdministration(
		usercontracts.Reach{CanAdminister: reached},
		usercontracts.Reach{CanAdminister: left},
		len(after) == 0, leaving)
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
