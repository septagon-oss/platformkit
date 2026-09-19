package contracts

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// Administration answers, for one tenant, which of its roles grant the
// permission a tenant needs in order to change its roles again.
//
// It is declared here, in the consumer, and satisfied by the application,
// because who may administer a tenant is the auth module's table and this
// module must not read another module's rows. modules/auth.AdministeringRoles is
// what goes inside the adapter below in the reference application, and
// apps/platformkit/modules.go is where that is decided — the same shape as
// notification's Recipients.
//
// One method, and it is an interface rather than a bare func type for one reason
// that a reader should not have to rediscover: `user.Deps` is published, it was
// `struct{}` at v1.1.0, and a struct that holds a func stops being comparable —
// no `==`, no map key, a fact apidiff reports and Go enforces. It is not made an
// interface to talk a checker out of a finding: an interface holding a func value
// would satisfy the checker and still panic the first time two `Deps` were
// compared, because comparing interfaces compares their dynamic values. It is an
// interface so that the shipped implementation can be a pointer, which is
// comparable in fact. See AdministrationFunc, and the note there about a harness
// that wants to answer in one line.
//
// user.Deps.Administration is required: a composition that supplies none gets a
// panic naming this, rather than an application whose floor is quietly missing.
type Administration interface {
	// Administering names this tenant's roles that grant the permission to
	// change a role again. An error is not an empty answer: the write is refused
	// rather than read as "nobody grants this", which is TestAnAnswerNobodyGotIs
	// NotAFreePass in both modules.
	Administering(ctx context.Context, tx db.Tx[db.Tenant]) ([]string, error)
}

// AdministrationFunc is the minimal adapter from a function to Administration.
// Take its address: `&AdministrationFunc{Ask: auth.AdministeringRoles}`.
//
// The pointer is the whole design, not decoration around a func. A named func
// type with an Administering method would satisfy this interface just as well and
// would be a lie about comparability: comparing two values that hold one compares
// interfaces, comparing interfaces compares dynamic values, and comparing a func
// panics at run time — so `user.Deps` would look comparable, pass the published
// API gate, and fall over the first time anybody wrote `deps == other` or used one
// as a map key. A *AdministrationFunc compares as the pointer it is.
//
// A harness with no roles table still answers in one line, which is what the
// func shape was for:
//
//	&usercontracts.AdministrationFunc{Ask: func(context.Context, db.Tx[db.Tenant]) ([]string, error) {
//		return []string{"owner"}, nil
//	}}
type AdministrationFunc struct {
	Ask func(ctx context.Context, tx db.Tx[db.Tenant]) ([]string, error)
}

var _ Administration = (*AdministrationFunc)(nil)

// Administering asks Ask. A missing Ask is reported as an error and never as an
// empty list, because "this tenant has no role granting the permission" and "this
// adapter was built without a function" are different facts with opposite
// consequences: the first must refuse nothing, and reading the second as the first
// is the silently missing floor this whole type exists to prevent. user.Module
// refuses the same shape at composition, so a wiring mistake is caught at boot and
// not at a customer's first guarded write.
func (f *AdministrationFunc) Administering(ctx context.Context, tx db.Tx[db.Tenant]) ([]string, error) {
	if f == nil || f.Ask == nil {
		return nil, errors.New("user: contracts.Administration adapter carries no function to ask")
	}
	return f.Ask(ctx, tx)
}

// Administers and CanAdminister are the two sides of the floor, and they are
// deliberately not the same predicate. Administers answers "is this write
// taking one of the tenant's administrators away"; CanAdminister answers "is
// there one left who could act". Those are different questions and one rule
// using one predicate for both is how this floor was first written and how it
// was wrong.
//
// The asymmetry runs the safe way in each direction. Administers is generous,
// so removing somebody who has not accepted their invitation yet is still a
// write the floor looks at. CanAdminister is strict, so somebody who has not
// accepted theirs cannot hold the floor up for everybody else.
//
// Administers reports whether this person is one of the tenant's
// administrators: not deleted, not deactivated, and holding one of the roles in
// administering. administering comes from Administration and is the auth
// module's answer, so this module never decides what a role grants — only who
// holds one.
//
// Invited, pending and unverified all count here, because each of them is
// somebody the tenant appointed and a write that removes them is removing an
// appointment. The two that do not are the two nobody inside the product can
// move back: kit/crud's soft delete hides the row from every read including
// ByEmail, and the kernel refuses a session whose user is not active.
func (u *User) Administers(administering []string) bool {
	return u != nil && u.DeletedAt == nil && u.Status != StatusInactive && u.holds(administering)
}

// CanAdminister reports whether this person could administer the tenant today:
// active, and holding one of administering.
//
// This is what the floor counts when it asks whether anybody is left, and it is
// narrower than Administers for a reason that was measured rather than
// imagined. An invitation is only a way back in if something delivers it. In
// apps/platformkit's own default configuration — config.example.yaml ships no
// mail.host — the mailer is notification.NewMailbox(), an in-process slice, and
// the set-password link is deliberately put in no row by the module that mints
// it, so it reaches nobody. Inviting an heir as an administrator and then
// standing down answered 200, and the heir's sign-in answered 401 while the
// outgoing administrator's next request answered 403. An invited heir held the
// floor up and could not in fact take over.
//
// Active rather than CanSignIn, and the first reason is the one that settles
// it: active is exactly the test Service.Open applies before it writes a
// session, so this predicate and the thing it is predicting agree by
// construction rather than by two lists being kept in step.
//
// The second reason is conditional and was stated here as fact, wrongly. Open
// looks only at the status, so an active person with no password hash would get
// a session — but no path in this repository produces one: every assignment of
// StatusActive either sets a hash or refuses without one, and the OIDC callback
// deliberately provisions nobody. A composition that did provision
// identity-provider accounts without a password would have such rows, and
// counting passwords would refuse writes in every tenant it served. This
// predicate does not depend on which of those a composition is.
//
// What it still cannot see is that an active administrator is a row and not a
// person. One who has forgotten their password in a tenant with no mail, or
// whose identity provider is gone, is a lockout nothing here reports.
func (u *User) CanAdminister(administering []string) bool {
	return u != nil && u.DeletedAt == nil && u.Status == StatusActive && u.holds(administering)
}

// holds is the half the two predicates share: does this person hold any of the
// roles that can administer the tenant.
func (u *User) holds(administering []string) bool {
	return slices.ContainsFunc(u.Roles, func(role string) bool {
		return slices.Contains(administering, role)
	})
}

// CheckedAdministration refuses the one write a tenant cannot undo from inside
// the product: the one that takes away the last person who could still sign in
// and administer it.
//
// before is the row as it stands and after is the row the write would leave;
// after is nil for a delete, which leaves no row at all. others is read only on
// the path that can refuse, so an ordinary write costs no query.
//
// After that write nobody inside the tenant can change a role again — not
// through the generated user screen, not through the roles screen, not through
// POST /api/v1/user/users/{id}/roles and not through
// PUT /api/v1/auth/roles/{name}. Every one of those is guarded by a permission
// the tenant's own roles no longer grant anybody.
//
// # Who can still repair it, exactly
//
// A customer's tenant is not beyond help, and saying otherwise would be the
// overclaim this comment exists not to make. The installation's operator can
// put a fresh administrator into one with POST /api/v1/tenant/tenants/{id}/invite,
// which runs in a system transaction and needs no session at the customer's
// host. Verified by running it: against a tenant whose only administrator had
// been stripped it answered 201 and left a second person holding the admin
// role.
//
// Two things bound that, and a contract has to say them even though the test
// did not need them. What the route hands out is a role by name, chosen by the
// application's adapter — "admin" in this repository's composition — so it
// recovers a tenant whose people lost their grants and not one where the named
// role is itself the role that was emptied, because that is the role it hands
// out. And the route exists at all only where an application wires the
// capability; modules/tenant mounts it if and only if it was given an inviter.
//
// The operator's own tenant is the case with no way back. That route declares
// tenant:manage as an operator permission, held through the operator tenant's
// own roles, so once that tenant has nobody who administers it the control
// plane is shut: no tenant can be created, none can be given an administrator,
// and the price list cannot be read. What is left there is SQL.
//
// Roles are not the only way there, and this comment used to imply they were.
// POST /api/v1/tenant/tenants/{id}/suspend on the operator's own tenant is one
// request: it answered 200, every operator host then answered 404 "no site is
// served", and the whole route surface has no resume or activate to undo it.
// That path predates this floor and neither floor touches it — a floor under
// who may administer a tenant cannot help with a tenant that is no longer
// served at all.
//
// The middle case — a tenant whose people still hold a role that still grants
// something — is the one this floor and modules/auth's jointly keep reachable.
//
// # What it refuses, stated so the two predicates do not make it a lie
//
// This used to read "the last administrator leaving, and not a demand that one
// exist: a write in a tenant that already has none passes". That was true when
// one predicate answered both questions and it is false now, and the state it
// is false in is reachable: in a tenant where every holder of an administering
// role is invited, pending or unverified, Administers says yes for each of them
// and CanAdminister says no for all of them, so removing any one of them is
// refused even though an identical second holder is sitting there and neither
// could sign in.
//
// What is actually refused is one thing: the write after which nobody who can
// sign in would hold a role that administers this tenant. In that state every
// removal of such a holder is refused, deliberately — those unaccepted
// invitations are the tenant's only thread, and dropping one is not repair.
//
// It is still never a dead end, because it never refuses a grant. Giving an
// administering role to somebody already active is not a write this rule can
// refuse — the subject held no such role, so it returns at the first line — and
// once it lands, the removal that was refused is allowed. That is the repair,
// and the refusal message names it rather than leaving somebody to find it.
//
// What it never demands is that an administrator exist before any write at all:
// somebody who holds no administering role is removed freely, whatever state
// the tenant is in.
//
// The caller must hold the reads and the write together — see
// internal.Service.floor, which takes an advisory lock on the tenant — or two
// administrators standing down at once both pass this and the tenant ends with
// neither.
//
// # What this does not close
//
// It counts people holding a role. It does not check what that role grants
// today, because that is the auth module's table: administering is a list of
// names handed in, and this rule is only as true as the answer it was given.
// Five consequences, written down rather than implied:
//
//   - Emptying the role itself reaches the same locked-out tenant from the
//     other side. PUT /api/v1/auth/roles/admin with no permissions leaves every
//     administrator holding a name that grants nothing, and this floor sees a
//     tenant full of administrators. modules/auth owns that door; on this
//     branch it is open, and closing it does not close this one either.
//
//   - The two floors do not compose into the invariant, and sharing a lock does
//     not make them. That was claimed here and it was false. Both take
//     "administration/<tenant id>" through
//     pg_advisory_xact_lock(hashtextextended(key, 0)) — the same literal
//     written out in both modules, pinned by a test on each side, and necessary
//     — but it only orders concurrent writes. It cannot make a check that never
//     asks the question answer it. Reproduced with both floors in one binary,
//     the shared key in place and no concurrency at all: create a role granting
//     role:manage, give it to nobody, then empty the role everybody holds. Two
//     200s, one actor, one write that matters, and a tenant that answers 403 to
//     its own roles screen. Sequentially the same from the other side: strip the
//     person holding the second administering role, then empty the first — each
//     floor verified a fact the other write then removed.
//
//     The join is this rule, for the writes it sees. It reads administering —
//     what the roles grant, from whoever owns roles — and counts the reachable
//     people holding them, which is the composed property itself, so every
//     write in this module is checked against it. That is the whole of the
//     claim: this rule is never consulted about an auth-module write, so it
//     holds the property for user-module writes and for no others. The mirror
//     is needed because of that boundary, not in spite of it. modules/auth's floor counts roles in isolation: a
//     role nobody holds satisfies it. The symmetric fix is the mirror of the
//     dependency this module already accepts — modules/auth asking who holds a
//     role before it changes what a role grants, and refusing the write after
//     which no reachable person holds one that grants role:manage. Until it
//     does, writes in that module can still reach the state this one refuses to
//     reach.
//
//   - Reachable counts an invited, pending or unverified person. If the last
//     administrator is a pending registration and nobody else holds
//     user:approve, the tenant is still unreachable — this floor permitted the
//     write that got there, because it saw somebody. See Reachable for why the
//     narrower rule is worse.
//
//   - It knows nothing about the world outside the database. An administrator
//     whose mailbox is gone, or who has forgotten a password nobody can reset
//     for them, is a lockout no row shows.
//
//   - It is per tenant and says nothing about the installation. The operator's
//     own tenant is protected exactly as much as a customer's, which is to say
//     that an operator tenant with one administrator is one row from being the
//     whole control plane's floor.
//
//   - Nothing in this repository renames or deletes a role, so a name somebody
//     holds cannot vanish from under them through an API today. A module that
//     adds either will need its own floor; this one will not catch it, because
//     it never reads the roles table.
//
// So this closes the user module's doors and says what is still open.
func CheckedAdministration(before, after *User, administering []string, others func() ([]*User, error)) error {
	if !before.Administers(administering) {
		return nil
	}
	if after.Administers(administering) {
		return nil
	}
	rest, err := others()
	if err != nil {
		return err
	}
	for _, other := range rest {
		if other.ID != before.ID && other.CanAdminister(administering) {
			return nil
		}
	}
	// What this says and does not say is the correction of a real defect. It
	// used to call the subject "the last person who can still sign in", which
	// is false whenever nobody could sign in to begin with — and it pointed
	// away from the one write that repairs the state instead of at it.
	return fmt.Errorf("%w: this would leave nobody who can sign in and administer this tenant: %s holds %s, and no active person would still hold it; grant it to somebody already active first",
		crud.ErrInvalid, before.Email, strings.Join(held(before, administering), ", "))
}

// held names the administering roles this person actually holds, so the refusal
// says which grant is leaving rather than listing every role in the tenant.
func held(u *User, administering []string) []string {
	out := make([]string, 0, len(u.Roles))
	for _, role := range u.Roles {
		if slices.Contains(administering, role) {
			out = append(out, role)
		}
	}
	return out
}
