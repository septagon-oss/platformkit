package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// administrationLock is the key every write that could leave a tenant unable to
// administer itself takes, and the reason it is not named after this module.
//
// The property is one — this tenant can still administer itself — and two
// modules guard it from opposite sides. The decision is one rule, written once,
// in the user module's contracts: what a role grants is this table and who holds
// one is theirs, so each side asks the other and both call the same function.
// What this lock buys is that a write in one module cannot answer the rule from a
// state the other is in the middle of changing: emptying a second administering
// role and standing the last administrator down are each valid alone, each
// verifies exactly what the other is falsifying, and on separate keys they both
// passed — which is the concurrent case, closed here, and was closed in queue
// order and nothing else.
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
// takes the roles row. internal.readUnderRoleLock is the one other taker: the
// repair reads a roles row unlocked to find out whether this tenant holds a dead
// grant at all, and then takes this lock, reads again and derives its write from
// that second read — so a healthy installation is never made to queue behind a
// repair that changes nothing, and the read that decides a write is still one
// taken under the lock. The user module's delete route locks the subject row
// first, because kit/rest reads and locks it before any hook runs, and takes
// this lock after. The two orders are opposite and that is fine, because no
// transaction wants both in a way that can wait.
//
// The composed floor changed what this side reads, so read this before adding
// anything: the check below asks who holds a granting role, which is rows of the
// user module's table. It asks them read-only, under the key this holds, with no
// FOR UPDATE, no lock and no write — and a plain SELECT cannot wait on a row
// lock, because MVCC reads the last committed version. The cycle stays open on
// that. A path here that took a users row lock after this key would close it,
// and apps/platformkit's TestTheAdministrationLockQueuesTheOtherModuleBehindIt
// is the case that would report it.
func administrationLock(tenant tenancy.Tenant) string {
	return "administration/" + tenant.ID.String()
}

// lockTenantRoles takes this tenant's administration lock for the current
// transaction, and is the one way anything in this package asks for it: every
// path that needs the lock holds the same key through the same call. It is a
// transaction lock — pg_advisory_xact_lock is held until this transaction commits
// or rolls back — so the reads that follow it and the write that follows those
// are one step, and no other transaction can move this tenant's roles between
// them. Asking again in one transaction is free, because PostgreSQL grants a
// transaction a lock it already holds: a path that locked and then writes through
// SetRoleChanged waits for nobody but itself.
//
// What the lock buys, and what it does not, is administrationLock's business. An
// earlier lock in a transaction opens no cycle this one closed, as long as the
// path takes no row lock first, which nothing here does. What it costs is that
// everything after it is inside the lock, which is why a caller that might not
// write at all does not ask until it knows it will.
func lockTenantRoles(ctx context.Context, tx db.Tx[db.Tenant], tenant tenancy.Tenant) error {
	if err := tx.DB().WithContext(ctx).
		Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, administrationLock(tenant)).Error; err != nil {
		return fmt.Errorf("auth: lock this tenant's roles: %w", err)
	}
	return nil
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
// It is SetRoleChanged with the one answer withheld. A caller that reports what
// it changed — the repair, which prints "removed" — needs that answer, and a
// caller that renders the role the route was about does not.
func (s *Service) SetRole(ctx context.Context, tx db.Tx[db.Tenant], name string, permissions []string, declared []tenancy.Grant) (*contracts.Role, error) {
	role, _, err := s.SetRoleChanged(ctx, tx, name, permissions, declared)
	return role, err
}

// SetRoleChanged is the write behind SetRole, and it reports whether this call
// changed a row.
//
// The write already knows: it re-reads the role under the tenant's lock and
// does nothing when the row holds the list it was handed, so that a retried
// click appears once in an audit. Until now that knowledge stopped there, which
// made the second answer free for a caller that has to say what it wrote — and
// it has to say it about a row rather than about its own arithmetic: a line
// saying "removed" is a claim that this call moved something, which is exactly
// the one fact only this write can supply. RepairSeededRoles is that caller.
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
func (s *Service) SetRoleChanged(ctx context.Context, tx db.Tx[db.Tenant], name string, permissions []string, declared []tenancy.Grant) (*contracts.Role, bool, error) {
	name, err := contracts.ValidRoleName(name)
	if err != nil {
		return nil, false, err
	}
	tenant := db.TenantOf(tx)
	want, err := contracts.CheckedPermissions(permissions, declared, tenant)
	if err != nil {
		return nil, false, err
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
	if err := lockTenantRoles(ctx, tx, tenant); err != nil {
		return nil, false, err
	}

	var was contracts.Permissions
	role := &contracts.Role{TenantID: tenant.ID, Name: name}
	switch err := crud.Classify(tx.DB().Where("tenant_id = ? AND name = ?", tenant.ID, name).Take(role).Error); {
	case err == nil:
		was = slices.Clone(role.Grants)
		if slices.Equal([]string(was), []string(want)) {
			// The same list again changes nothing and publishes nothing: a
			// retried click must not appear twice in an audit of who was given
			// what. That is also the whole answer to "what did you change": no.
			return role, false, nil
		}
	case !errors.Is(err, crud.ErrNotFound):
		// A role nobody has yet is the ordinary case and reads as not found.
		// Anything else is a read that did not happen, and it used to be
		// dropped here: was stayed empty, the check below concluded that
		// nothing was leaving, and the write went ahead regardless. A guard
		// whose precondition is a read nobody looked at is a guard that passes
		// by accident.
		return nil, false, fmt.Errorf("auth: read the role %q: %w", name, err)
	}
	// The one write a tenant cannot undo from inside the product, which is one
	// rule the user module owns and this module asks through the door above. The
	// two reads are behind the rule's own gate rather than in front of it, so only
	// a write that is taking role:manage away pays for them.
	if err := contracts.CheckedAdministration(name, was, want, func() ([]*contracts.Role, error) {
		return s.Roles(ctx, tx)
	}, func(names []string) ([]uuid.UUID, error) {
		// Who holds a name is the user module's column, asked in this tenant's
		// transaction under the key this file already holds, read-only.
		return s.users.Holders(ctx, tx, names)
	}); err != nil {
		if errors.Is(err, crud.ErrInvalid) {
			// The rule refused it. A read that did not happen also refuses the
			// write, but it is not this rule refusing, and recording it would put
			// a lockout attempt on the trail every time a query failed.
			s.recordRefusal(ctx, name, was, want)
		}
		return nil, false, err
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
		return nil, false, fmt.Errorf("auth: write the role %s: %w", name, err)
	}
	if err := events.Publish(ctx, tx, contracts.EventRoleSet, contracts.RoleSet{
		Role: name, Was: was, Now: want, At: at,
	}); err != nil {
		// State and outbox row commit together or neither does, and a change
		// that will not be committed changed nothing.
		return nil, false, err
	}
	return role, true, nil
}

// recordRefusal puts a write the last-administrator rule stopped on the trail,
// in a transaction of its own.
//
// It has to be outside the caller's: the request transaction is about to roll
// back — a 422 is a response of 400 or worse, and kit/httpx does not commit those
// — so a row written inside it would never exist, and the attempt is the only
// fact about this write anybody would ever read. This is this module's own
// precedent, not a new mechanism: recordFailure does the same for a failed login,
// and copies the same four things — db.Detached over the request's context, which
// keeps the tenant, the actor, the request id and the trace context while dropping
// the cancellation and the pending transaction; the pool connection kit/httpx put
// on the request; detachedWriteBudget, so the trail write cannot make the refusal
// the slow part of the call; and the request's own ctx given to Publish, so the
// row carries them. With no request connection on ctx — a job, a harness, a system
// transaction — there is nowhere to write and the rule still refuses: the answer
// a caller gets was never contingent on its paperwork.
//
// It logs rather than fails, for the same reason recordFailure does: the write is
// already refused, and a trail that could not take the row does not make the
// caller's request any less refused. The failure is not silent, though — it is a
// logged line naming the role.
func (s *Service) recordRefusal(ctx context.Context, name string, was, want contracts.Permissions) {
	conn, ok := httpx.ConnFrom(ctx)
	if !ok {
		return
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), detachedWriteBudget)
	defer cancel()
	err := db.Run(detached, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, contracts.EventAdministrationRefused, contracts.AdministrationRefused{
			Role: name, Was: was, Now: want, At: db.Now(),
		})
	})
	if err != nil {
		slog.ErrorContext(ctx, "auth: could not record a refused role write",
			"role", name, "error", err)
	}
}

// Undeclared reports, for one tenant, every role row naming a permission the
// application does not define.
//
// It has two callers and they read one list for different reasons. The hourly
// sweep runs it once per tenant, inside that tenant's own transaction, and logs
// everything it finds; RepairSeededRoles runs it and then narrows what it finds
// to the grants this module's own seeder wrote, which is the only part an
// operator can ask to have taken away. A run of that repair which is going to
// write asks twice: once with an unlocked read, to find out whether this tenant
// holds anything to take at all, and once under the tenant's role lock, whose
// answer is the one it derives its write from.
//
// What this function answers is a warning and not a refusal: the rows belong to
// customers and were legal when they were written — a module removed from a
// composition takes its permissions with it — so a sweep that refused would turn
// dropping a module into an installation somebody has to repair by hand. What it
// buys is that "this role grants nothing and nobody can see why" is a line in the
// log within the hour of the deploy that caused it rather than a support
// conversation months later.
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
