package internal

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// administrationLock is the key every write that could leave a tenant unable to
// administer itself takes, and the reason it is not named after this module.
//
// The property is one — this tenant can still administer itself — and two
// modules check halves of it. This one counts the roles that grant role:manage;
// the user module counts the people holding such a role. On separate keys they
// do not see each other at all: two concurrent writes that each verify what the
// other is falsifying both pass, so emptying a second administering role and
// standing the last administrator down are each valid alone and together reach
// the tenant both floors are for.
//
// What this lock buys is that those two cannot happen at the same time. It does
// not make the state unreachable, and a reader of this comment must not take it
// that way: the same two writes one after the other are each still permitted,
// because each half is asked in isolation and the composed question is asked by
// nobody. contracts.CheckedAdministration says what is left open and what the
// fix would look like. This is the concurrent case, closed.
//
// So the string is deliberately neither module's. It is written out in both
// rather than imported from one, because a module reaching into another for a
// lock name is a dependency where a shared convention will do — and it is
// pinned by a test on each side, so an edit to one cannot silently
// desynchronise them. Changing it means changing it in both, in one delivery.
// The hash matters as much as the string: the same key through hashtext rather
// than hashtextextended is a different lock.
//
// # Ordering
//
// This is the only advisory lock any path through this file takes, and it is
// taken before the first read and before any row lock — the write below is what
// takes the roles row. The user module's delete route locks the subject row
// first, because kit/rest reads and locks it before any hook runs, and takes
// this lock after. The two orders are opposite and that is fine, because no
// transaction wants both: nothing here touches a user row, and nothing there
// touches a roles row outside this lock. A path that took a row lock and then
// asked for this one would close the cycle; there is none, and this comment is
// where to check before adding one.
func administrationLock(tenant tenancy.Tenant) string {
	return "administration/" + tenant.ID.String()
}

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

	// Everything below reads this tenant's roles and then writes one, and the
	// floor underneath is only as good as those two being one step.
	//
	// kit/db sets no isolation level, so this is read committed and the two
	// writes touch different primary keys: two administrators standing down at
	// once — two tabs, or one double-submit — each read a tenant that still had
	// somebody else who could administer it, both were allowed, and the tenant
	// ended with nobody. The floor was right twice about a fact that stopped
	// being true in between.
	//
	// pg_advisory_xact_lock is held until this transaction commits or rolls
	// back, so the reads and the write are one step; it is keyed on the tenant,
	// so one customer's administrators do not queue behind another's; and it
	// needs no table, no row and no migration. modules/file takes the same kind
	// of lock over a quota for the same kind of reason.
	//
	// hashtextextended over 64 bits and not hashtext over 32, which is what
	// makes the sentence about not queueing behind another customer true rather
	// than nearly true: a colliding pair of tenant ids is easy to find in a
	// space of four billion — a review found one in two hundred thousand
	// samples and measured the second tenant waiting two seconds on the first
	// one's key. Row-level security confines the transaction either way, so the
	// cost of a collision is waiting and never reading; it is still one
	// customer's administrator held up by another's.
	if err := tx.DB().WithContext(ctx).
		Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, administrationLock(tenant)).Error; err != nil {
		return nil, fmt.Errorf("auth: lock this tenant's roles: %w", err)
	}

	var was contracts.Permissions
	role := &contracts.Role{TenantID: tenant.ID, Name: name}
	switch err := crud.Classify(tx.DB().Where("tenant_id = ? AND name = ?", tenant.ID, name).Take(role).Error); {
	case err == nil:
		was = slices.Clone(role.Grants)
		if slices.Equal([]string(was), []string(want)) {
			// The same list again changes nothing and publishes nothing: a
			// retried click must not appear twice in an audit of who was given
			// what.
			return role, nil
		}
	case !errors.Is(err, crud.ErrNotFound):
		// A role nobody has yet is the ordinary case and reads as not found.
		// Anything else is a read that did not happen, and it used to be
		// dropped here: was stayed empty, the check below concluded that
		// nothing was leaving, and the write went ahead regardless. A guard
		// whose precondition is a read nobody looked at is a guard that passes
		// by accident.
		return nil, fmt.Errorf("auth: read the role %q: %w", name, err)
	}
	// The one write a tenant cannot undo from inside the product. The query is
	// behind the rule rather than in front of it, so only a write that is
	// taking role:manage away pays for it. See contracts.CheckedAdministration.
	if err := contracts.CheckedAdministration(name, was, want, func() ([]*contracts.Role, error) {
		return s.Roles(ctx, tx)
	}); err != nil {
		return nil, err
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
