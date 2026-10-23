package contracts

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	// The refusals come from kit/fault rather than kit/crud: this package names no
	// transaction, and kit/crud re-exports the same values, so a caller that already
	// wrote crud.ErrInvalid still matches what is returned here.
	"errors"
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
//
// Its refusals are fault.Reason values, so the rule that refused survives as a
// named verdict rather than as prose: the sentence here is the one this file is
// written in, which is what a problem document and a log line print, and the key is
// what whoever renders the refusal for a reader looks their own copy up under. See
// CheckedPermissions for the same choice.
func ValidRoleName(name string) (string, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !roleName.MatchString(name) {
		return "", &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.auth.role.name",
			Text: "role %q is not a lower-case identifier", Args: []any{name}}
	}
	if len(name) > MaxRoleName {
		return "", &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.auth.role.name.length",
			Text: "a role name is at most %d characters, and %q is %d",
			Args: []any{MaxRoleName, name, len(name)}}
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
	leaving := "role %q is the last role that grants %s"
	if len(after) > 0 {
		leaving += " that any active person holds"
	}
	refusal := usercontracts.CheckedAdministrationWithArgs(usercontracts.Reach{CanAdminister: reached},
		usercontracts.Reach{CanAdminister: left}, len(after) == 0, leaving, name, PermissionRoleManage)
	if refusal == nil {
		return nil
	}
	// The verdict is the user module's, because whether anybody is left who can
	// sign in is its fact and not this module's. Its sentence is keyed here so the
	// person refused one is answered in the language the rest of the page is in —
	// and its arguments travel with it, because the sentence a screen owns for the
	// same verdict names the role and the permission rather than quoting prose.
	reason, named := errors.AsType[*fault.Reason](refusal)
	if !named {
		return refusal
	}
	return &fault.Reason{Kind: fault.ErrInvalid,
		Key:  "invalid.auth.role.last_administration",
		Text: reason.Text, Args: reason.Args}
}

// CheckedPermissions normalises a permission list and refuses the two ways one can be
// wrong. Both refusals name themselves (fault.Reason) rather than only saying so in
// prose: the sentence below is what a problem document and a log line carry, and the
// key is what the screen offering this write answers with, in the language of the
// person it refused. This module owns the rule and so owns both; who renders it is not
// this module's business, which is why the key is a name and not an import. The result is sorted and deduplicated, so "the same permissions in
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
			return nil, &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.auth.permission.undeclared",
				Text: "no module defines the permission %q, so a role naming it would grant nothing",
				Args: []any{p}}
		case declared[i].Operator && !tenant.Operator:
			return nil, &fault.Reason{Kind: fault.ErrInvalid, Key: "invalid.auth.permission.operator",
				Text: "%q belongs to the operator of this installation, and %s is not it",
				Args: []any{p, tenant.Slug}}
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
//   - Anything else — no name in defaults matches it, the built-in member
//     included: an application whose literal does name member falls under the
//     bullet above and not this one — : the seeder wrote it holding nothing, so
//     every permission in it is somebody's decision. A customer's tenant is not
//     the operator's either, so its administrator's row is only ever seeded the
//     wildcard and nothing in it is the seeder's.
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
// one role that holds those is the one this function builds. Both rules are
// about the permission and not the way the literal spells it, so both read it
// normalised — the same normalisation CheckedPermissions is about to apply, and
// the same one SeededGrants applies to read this literal backwards. A rule that
// compared the raw text held only for the one spelling out of five a person
// plausibly types, and the other four were seeded. Everything else goes through
// CheckedPermissions, which is where "no module defines it" is refused — the
// rule that used to be a warning an hour later.
//
// "grants nothing" is read from that same normalised list and not from the raw
// literal: CheckedPermissions drops an entry that trims to nothing, so a list of
// blanks is a list of nothing and is refused here. Measured against the raw
// literal it was a list of one, and the role was seeded holding nothing.
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
		if seen[name] {
			return nil, fmt.Errorf("%w: initial role %q is reserved or duplicated", fault.ErrInvalid, name)
		}
		for _, p := range role.Grants {
			p = strings.ToLower(strings.TrimSpace(p))
			if p == Wildcard || slices.Contains(operator, p) {
				return nil, fmt.Errorf("%w: initial role %q cannot grant %q", fault.ErrInvalid, name, p)
			}
		}
		grants, err := CheckedPermissions(role.Grants, declared, tenant)
		if err != nil {
			return nil, fmt.Errorf("initial role %q: %w", name, err)
		}
		if len(grants) == 0 {
			return nil, fmt.Errorf("%w: initial role %q grants nothing", fault.ErrInvalid, name)
		}
		seen[name], roles[name] = true, grants
	}
	out := make([]Role, 0, len(roles))
	for _, name := range slices.Sorted(maps.Keys(roles)) {
		out = append(out, Role{Name: name, Grants: roles[name]})
	}
	return out, nil
}
