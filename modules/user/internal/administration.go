package internal

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// administrationLock is the key every write that could leave a tenant unable to
// administer itself takes, and the reason it is not named after this module.
//
// The property is one — this tenant can still administer itself — and two
// modules guard it from opposite sides. This one counts the people holding a
// role that can administer; modules/auth counts the roles that grant it. On
// separate keys the two floors never see each other: emptying a second
// administering role and standing the last administrator down are each valid
// alone, each verifies exactly what the other is in the middle of falsifying,
// and together they arrive at the tenant both floors exist to prevent.
//
// So the string is neither module's name, and it is written out in both rather
// than exported from one — a module reaching into another for a lock name is a
// dependency where a convention does the job, and a test on each side pins it
// so the convention cannot drift quietly. Changing it means changing both, in
// one delivery. The hash function is as much of the agreement as the string:
// the same key through hashtext is a different lock from the same key through
// hashtextextended, and nothing would report the difference.
//
// # Ordering, and why two opposite orders are safe
//
// Here the subject's row is locked first and this second, because kit/rest
// reads and locks the row before any hook of ours can run and we cannot get in
// front of it. modules/auth takes this lock first, before its own first read.
// Opposite orders deadlock only if some transaction wants both locks and they
// disagree about which comes first, and none does: nothing on that side touches
// a user row, and nothing on this side touches a roles row except through the
// Administration the application wires, which is called under this lock. The
// cycle stays open only while that holds, so this is the comment to check
// before adding a path that takes a roles row and then asks for this — or, on
// this side, a second user's row once this lock is held. Every door today is
// one request about one person, and the invitation route's Invite and SetRoles
// are the same new row.
func administrationLock(tenant tenancy.Tenant) string {
	return "administration/" + tenant.ID.String()
}

// floor refuses the write that would leave this tenant with nobody who can
// still sign in and administer it. It is the shared half of the three doors
// that reach that state — SetRoles, Deactivate and the delete route's hook —
// so the rule is read once and the three cannot disagree. The decision itself
// is contracts.CheckedAdministration, which the fake calls too.
//
// Every caller has already taken the subject's row FOR UPDATE by the time it
// gets here — SetRoles and Deactivate through lockedUser, the delete route
// through crud.GetForUpdate — so before is the latest committed version of that
// row and cannot change underneath this decision. administrationLock is where
// the order of the two locks is argued.
//
// The advisory lock is what makes the rest of the decision and the write one
// step. kit/db sets no isolation level, so this is read committed and two
// administrators standing down at once touch different primary keys: each read
// a tenant that still had somebody else who could administer it, both were
// allowed, and the tenant ended with neither. Both were right twice about a
// fact that stopped being true in between. pg_advisory_xact_lock is held until
// the transaction commits or rolls back, is keyed on the tenant so one
// customer's administrators do not queue behind another's, and needs no table,
// no row and no migration. modules/file takes the same kind of lock over a
// quota for the same kind of reason.
//
// hashtextextended over 64 bits rather than hashtext over 32, and that is what
// makes "do not queue behind another's" true rather than nearly true: two
// tenant ids colliding in a space of four billion is something somebody finds
// by looking, and the cost is one customer's administrator waiting on another
// customer's write. Row-level security confines both transactions either way,
// so a collision would cost waiting and never reading.
func (s *Service) floor(ctx context.Context, tx db.Tx[db.Tenant], before, after *contracts.User) error {
	tenant := db.TenantOf(tx)
	if err := tx.DB().WithContext(ctx).
		Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, administrationLock(tenant)).Error; err != nil {
		return fmt.Errorf("user: lock this tenant's administration: %w", err)
	}
	administering, err := s.administering.Administering(ctx, tx)
	if err != nil {
		return fmt.Errorf("user: ask which roles can administer %s: %w", tenant.Slug, err)
	}
	return contracts.CheckedAdministration(before, after, administering, func() ([]*contracts.User, error) {
		return otherAdministrators(tx, before.ID, administering)
	})
}

// RefuseLastAdministrator is the delete route's half of the floor: the row is
// already gone as far as this transaction is concerned, so the state the write
// would leave is no row at all.
//
// It is rest.Spec.AfterDelete rather than a Service command because deleting a
// user is the generated CRUD route and not a lifecycle command — there is no
// Delete method for the rule to live in. The hook runs inside the request's
// transaction, so returning an error rolls the delete back and the caller gets
// a 422 naming the rule.
func (s *Service) RefuseLastAdministrator(ctx context.Context, tx db.Tx[db.Tenant], u *contracts.User) error {
	return s.floor(ctx, tx, u, nil)
}

// otherAdministrators is somebody else in this tenant who could administer it
// today, or nobody.
//
// The predicate is contracts.User.CanAdminister written in SQL — active, not
// deleted, holding one of these roles — and not Administers, which is the
// other half of the floor and deliberately wider. The rule checks it again in
// Go, so the two can only disagree by refusing a write that would have been
// allowed, never by allowing one that would lock a tenant out. Two rows rather
// than one for the same reason: a hedge that costs nothing.
//
// There is no index on roles, so this is a scan of the tenant's own users. It
// is behind the rule rather than in front of it, so only a write that is taking
// an administrator away pays for it.
func otherAdministrators(tx db.Tx[db.Tenant], subject uuid.UUID, administering []string) ([]*contracts.User, error) {
	if len(administering) == 0 {
		return nil, nil
	}
	var out []*contracts.User
	err := tx.DB().
		Where("id <> ? AND deleted_at IS NULL AND status = ? AND roles && ?::text[]",
			subject, contracts.StatusActive, pq.StringArray(administering)).
		Limit(2).Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("user: look for another administrator: %w", err)
	}
	return out, nil
}
