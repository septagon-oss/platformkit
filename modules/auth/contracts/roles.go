package contracts

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

var roleName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// ValidRoleName normalizes a role name and checks the identifier shared by
// role administration and tenant provisioning.
func ValidRoleName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !roleName.MatchString(name) {
		return "", fmt.Errorf("%w: role %q is not a lower-case identifier", crud.ErrInvalid, name)
	}
	return name, nil
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
				crud.ErrInvalid, p)
		case declared[i].Operator && !tenant.Operator:
			return nil, fmt.Errorf("%w: %q belongs to the operator of this installation, and %s is not it",
				crud.ErrInvalid, p, tenant.Slug)
		}
		out = append(out, p)
	}
	slices.Sort(out)
	return out, nil
}
