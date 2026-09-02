package internal

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// roleName is the grammar of a role: a lower-case identifier, the same rule
// user/contracts applies to the names it stores, because they are the same
// names and a role a user can hold but nobody can define is a grant that never
// resolves.
var roleName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Roles is every role in this tenant, in name order, under the tenant's own
// policy — so this is the same query from every host and answers about one
// customer whichever administrator asks.
func (s *Service) Roles(_ context.Context, tx db.Tx[db.Tenant]) ([]*contracts.Role, error) {
	var out []*contracts.Role
	if err := tx.DB().Order("name").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("auth: read the roles: %w", err)
	}
	return out, nil
}

// SetRole writes what a role grants, creating it if it is new.
//
// Every permission is checked against the list the application declares, which
// the caller is handed by the kernel. A role naming a permission nothing
// defines is a grant that can never be exercised and reads, to whoever wrote
// it, exactly like one that can — the failure is silent and permanent, and it
// is the one an authorization screen makes easy to cause.
//
// An operator permission outside the operator's own tenant is refused for a
// sharper reason: the kernel would refuse every request under it anyway, so
// writing one is either a misunderstanding of what the permission is or an
// attempt to grant the installation to a customer. Both are 422s.
func (s *Service) SetRole(ctx context.Context, tx db.Tx[db.Tenant], name string, permissions []string, declared []tenancy.Grant) (*contracts.Role, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !roleName.MatchString(name) {
		return nil, fmt.Errorf("%w: role %q is not a lower-case identifier", crud.ErrInvalid, name)
	}
	tenant := db.TenantOf(tx)
	want, err := checked(permissions, declared, tenant)
	if err != nil {
		return nil, err
	}

	var was contracts.Permissions
	role := &contracts.Role{TenantID: tenant.ID, Name: name}
	err = tx.DB().Where("tenant_id = ? AND name = ?", tenant.ID, name).Take(role).Error
	if err == nil {
		was = slices.Clone(role.Grants)
		if slices.Equal([]string(was), []string(want)) {
			// The same list again changes nothing and publishes nothing: a
			// retried click must not appear twice in an audit of who was given
			// what.
			return role, nil
		}
	}
	at := db.Now()
	role.Grants, role.UpdatedAt = want, at
	if role.CreatedAt.IsZero() {
		role.CreatedAt = at
	}
	err = tx.DB().Exec(
		"INSERT INTO roles (tenant_id, name, permissions, created_at, updated_at) VALUES (?, ?, ?, ?, ?)"+
			" ON CONFLICT (tenant_id, name) DO UPDATE SET permissions = EXCLUDED.permissions, updated_at = EXCLUDED.updated_at",
		tenant.ID, name, want, role.CreatedAt, at).Error
	if err != nil {
		return nil, fmt.Errorf("auth: write the role %s: %w", name, err)
	}
	return role, events.Publish(ctx, tx, contracts.EventRoleSet, contracts.RoleSet{
		Role: name, Was: was, Now: want, At: at,
	})
}

// checked normalises a permission list and refuses the two ways one can be
// wrong. The result is sorted and deduplicated, so "the same permissions in
// another order" is the same value and SetRole can tell that nothing changed.
func checked(permissions []string, declared []tenancy.Grant, tenant tenancy.Tenant) (contracts.Permissions, error) {
	out := make(contracts.Permissions, 0, len(permissions))
	for _, p := range permissions {
		p = strings.ToLower(strings.TrimSpace(p))
		switch {
		case p == "":
			continue
		case slices.Contains(out, p):
			continue
		case p == contracts.Wildcard:
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

// Undeclared reports, for one tenant, every role row naming a permission the
// application does not define.
//
// kit/app calls it at boot, once, under system access, and logs what it finds.
// It is a warning and not a refusal: the rows belong to customers and were
// legal when they were written — a module removed from a composition takes its
// permissions with it — so a boot that refused would be an installation a
// deployment could brick by dropping a module. What it buys is that "this role
// grants nothing and nobody can see why" is a line in the log of the deploy
// that caused it rather than a support conversation months later.
func Undeclared(roles []*contracts.Role, declared []tenancy.Grant) map[string][]string {
	known := make(map[string]bool, len(declared)+1)
	known[contracts.Wildcard] = true
	for _, g := range declared {
		known[g.Permission] = true
	}
	out := map[string][]string{}
	for _, r := range roles {
		for _, p := range r.Grants {
			if !known[p] {
				out[r.Name] = append(out[r.Name], p)
			}
		}
	}
	return out
}

// RolesOf is every role of one named tenant, read from a cross-tenant
// transaction. It is the boot check's query and has no other caller: a request
// reads roles through Roles, in its own tenant's transaction, under the policy.
func RolesOf(tx db.Tx[db.System], tenantID uuid.UUID) ([]*contracts.Role, error) {
	var out []*contracts.Role
	if err := tx.DB().Where("tenant_id = ?", tenantID).Order("name").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("auth: read the roles of %s: %w", tenantID, err)
	}
	return out, nil
}
