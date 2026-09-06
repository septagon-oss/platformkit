package internal

import (
	"context"
	"fmt"
	"slices"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

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
	name, err := contracts.ValidRoleName(name)
	if err != nil {
		return nil, err
	}
	tenant := db.TenantOf(tx)
	want, err := contracts.CheckedPermissions(permissions, declared, tenant)
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

// Undeclared reports, for one tenant, every role row naming a permission the
// application does not define.
//
// The hourly sweep is its one caller, once per tenant, inside that tenant's own
// transaction; it logs what it finds. It is a warning and not a refusal: the
// rows belong to customers and were legal when they were written — a module
// removed from a composition takes its permissions with it — so a sweep that
// refused would turn dropping a module into an installation somebody has to
// repair by hand. What it buys is that "this role grants nothing and nobody can
// see why" is a line in the log within the hour of the deploy that caused it
// rather than a support conversation months later.
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
