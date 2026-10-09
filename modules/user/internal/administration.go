package internal

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// detachedWriteBudget bounds the detached write of a refusal record. It is the same
// two seconds modules/auth gives the record of a failed login
// (internal.Service.recordFailure), for the same reason: the caller is already
// holding the tenant's administration lock and waiting for its answer, so a
// trail write that outlives the request would make the refusal the expensive
// part of the call. A write that does not finish inside it is logged and lost.
const detachedWriteBudget = 2 * time.Second

// The three doors the last-administrator rule stands in front of, as the trail
// names them. They are the values of AdministrationRefused.Attempt, which the
// payload's own enums tag carries to subscribers.
const (
	attemptRoles  = "roles"
	attemptStatus = "status"
	attemptDelete = "delete"
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
// disagree about which comes first, and none does.
//
// What the composed check changed about that argument, and it has to be read
// rather than assumed safe: modules/auth's SetRole now reads this tenant's users
// rows to count who holds a granting role. It reads them read-only, under the
// key it already holds, never FOR UPDATE, never locking and never writing one —
// and a plain SELECT cannot wait on a row lock, because MVCC hands it the last
// committed version. The two orders therefore still cannot form a cycle. A path
// that took a users row lock after this key would close it, and that is the
// change to refuse in review; the canary is apps/platformkit's
// TestTheAdministrationLockQueuesTheOtherModuleBehindIt, which fails on the
// deadlock or on its pg_locks deadline if they ever stop being safe.
//
// Nothing on this side touches a roles row except through the Administration the
// application wires, which is called under this lock, and no door here takes a
// second user's row: every door today is one request about one person, and the
// invitation route's Invite and SetRoles are the same new row.
func administrationLock(tenant tenancy.Tenant) string {
	return "administration/" + tenant.ID.String()
}

// floor refuses the write that would leave this tenant with nobody who can
// still sign in and administer it. It is the shared half of the three doors
// that reach that state — SetRoles, Deactivate and the delete route's hook —
// so the rule is read once and the three cannot disagree. The decision itself
// is contracts.CheckedAdministration, which the fake calls too, and attempt
// names which of the three doors is asking so the record below can say it.
//
// A door this function refuses is recorded (recordRefusal) — the attempt is the
// only thing about the write that survives, and the brief asks for it by name.
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
func (s *Service) floor(ctx context.Context, tx db.Tx[db.Tenant], attempt string, before, after *contracts.User) error {
	tenant := db.TenantOf(tx)
	if err := tx.DB().WithContext(ctx).
		Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, administrationLock(tenant)).Error; err != nil {
		return fmt.Errorf("user: lock this tenant's administration: %w", err)
	}
	administering, err := s.administering.Administering(ctx, tx)
	if err != nil {
		return fmt.Errorf("user: ask which roles can administer %s: %w", tenant.Slug, err)
	}
	// This door's own cheap gate, in front of the read and about this row alone:
	// a write that cannot shrink the set of people who could administer the
	// tenant — because the person it moves never administered it, or still does
	// afterwards — pays for no scan and never reaches the rule.
	if !before.Administers(administering) || after.Administers(administering) {
		return nil
	}
	others, err := otherAdministrators(tx, before.ID, administering)
	if err != nil {
		return err
	}
	err = contracts.CheckedAdministration(
		contracts.UserReach(before, administering, others),
		contracts.UserReach(after, administering, others),
		// The appointment this write moves out of the grant is the last way back
		// whenever nobody can sign in today, which is what the rule's third
		// argument asks about. The gate above is what makes it true here.
		!after.Administers(administering),
		contracts.LeavingAdministration(before, administering))
	if err != nil {
		s.recordRefusal(ctx, attempt, before, administering)
	}
	return err
}

// recordRefusal puts the refused attempt on the trail, in a transaction of its
// own.
//
// It has to be outside the caller's, and that is not an oversight: the request
// transaction is about to roll back — a 422 is a response of 400 or worse, and
// kit/httpx does not commit those — so an outbox row written inside it would
// never exist. This is the second place in the application where an event is
// deliberately written outside the transaction of the thing it describes, and it
// copies the one precedent, modules/auth's recordFailure: db.Detached over the
// request's context, which keeps the tenant, the actor, the request id and the
// trace context while dropping the cancellation and the pending transaction; the
// pool connection kit/httpx put on the request; detachedWriteBudget, so the trail
// write cannot make the refusal the slow part of the call; and the request's own
// ctx given to Publish, so the row carries them. The reason is the precedent's
// own: the thing recorded is precisely the case where nothing else is written
// down. "Somebody tried to lock this tenant out" is a line an administrator reads
// afterwards, and it is about a write that did not happen.
//
// One record per refused attempt, with no idempotency key: a caller who clicks
// twice is refused twice, and that is two lines on the trail — which is what an
// attempt is worth reading. The record can outlive its refusal by nothing, because
// the refusal is a returned error and the caller's transaction rolls back with it;
// the detached row is the only thing that survives, and it survives on purpose.
//
// And a refusal this path cannot record is still a refusal. With no request
// connection on ctx — a job, a harness, a system transaction — there is nowhere
// to write, and the rule never was contingent on its paperwork: the caller gets
// the 422 either way, and where a write was attempted and failed the line is
// logged rather than the request made to succeed or fail on the trail's health.
func (s *Service) recordRefusal(ctx context.Context, attempt string, u *contracts.User, administering []string) {
	conn, ok := httpx.ConnFrom(ctx)
	if !ok {
		return
	}
	payload := contracts.AdministrationRefused{
		UserID: u.ID, Attempt: attempt, Roles: countingRoles(u, administering), At: db.Now(),
	}
	detached, cancel := context.WithTimeout(db.Detached(context.WithoutCancel(ctx)), detachedWriteBudget)
	defer cancel()
	err := db.Run(detached, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return events.Publish(ctx, tx, contracts.EventAdministrationRefused, payload)
	})
	if err != nil {
		slog.ErrorContext(ctx, "user: could not record a refused administration write",
			"userId", u.ID, "attempt", attempt, "error", err)
	}
}

// countingRoles names the roles that make this person one of the tenant's
// administrators: the grant the refused write would have taken away, which is the
// fact a reader of the trail is asking about. A refusal always finds somebody who
// counted, because the gate in floor is what got here; the list is never empty on
// this path. The contract spells out why nothing else is carried.
//
// It is this package's copy of the filter modules/user/contracts applies to the
// same rows in LeavingAdministration, and it stays here rather than widening the
// published contract for a helper: the sentence and the payload name the same
// roles, and both are tested from the outside by the composition case that reads
// the row this writes.
func countingRoles(u *contracts.User, administering []string) []string {
	out := make([]string, 0, len(u.Roles))
	for _, role := range u.Roles {
		if slices.Contains(administering, role) {
			out = append(out, role)
		}
	}
	return out
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
	return s.floor(ctx, tx, attemptDelete, u, nil)
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
