package contracts

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

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

// Reach is one state of one tenant as the last-administrator rule sees it: the
// people who could sign in and administer it in that state, and nothing else.
//
// Every door builds two of these — the state before the write and the state it
// would leave — and hands them to CheckedAdministration, which is the only
// reader. The ids are what Service.Holders answers and what User.CanAdminister
// decides, so the two modules that guard this property count the same people by
// applying one predicate rather than by keeping two lists in step.
type Reach struct {
	// CanAdminister are the people who could sign in and administer this
	// tenant in this state: active, not deleted, holding one of the role names
	// that grant the permission which manages roles. An empty list is the fact
	// that nobody could, which is the only thing the rule looks at.
	CanAdminister []uuid.UUID
}

// CheckedAdministration is the one check every write that can move the
// invariant runs, inside administrationLock, on the state it is about to leave.
//
// The invariant, in one sentence: in one tenant, at every commit, at least one
// person who is active and not deleted holds at least one role that, in that
// same transaction's view of the roles table, grants the permission that manages
// roles — and no write may be the write that makes that false.
//
// Both halves used to live in one module each, and each asked only its own
// question: this module counted the people holding a role whose names it was
// handed, and modules/auth counted the roles granting the permission, never
// asking who held one. Each was right about its own rows, and the composed
// question was asked by nobody, so two permitted writes — create a role
// granting role:manage and give it to nobody, then empty the role everybody
// holds — reached a tenant nobody inside can administer, with no concurrency at
// all. That is the sequence apps/platformkit's
// TestTheTwoFloorsComposeIntoOneInvariant now asserts is refused. The question
// is asked once, here, over the people and the grants together, and every door
// that can move it answers it before it writes.
//
// It refuses the write that takes the last one away and never the write that
// finds none: after is what the tenant would be left with, before is what it is
// now, and a tenant that already has nobody is a tenant that needs a grant, so
// refusing its writes would take the repair away with the damage. lastWayBack is
// the caller's answer to "does this write destroy the last thing that could
// still restore the invariant" — the appointment a person is being moved out
// of, for the doors in this module; the grant being removed, for the door in
// modules/auth. It is what keeps the tenant whose only holders have not accepted
// their invitations from having the thread they hang from cut, and it is never
// set by a write that adds.
//
// leaving is the caller's own sentence about what its write takes away, because
// only the caller knows which row it is moving: LeavingAdministration is this
// module's words, and modules/auth's contracts.CheckedAdministration is the
// other door's. The value is crud.ErrInvalid, so both doors answer 422 with the
// rule, what it would leave, and the repair — and the repair is never itself
// refused, because a write that adds a grant or activates somebody returns here
// at the first line.
//
// # Who records the refusal
//
// This function writes nothing: it is a decision over two sets of ids with no
// transaction of its own, and the caller's is about to be rolled back. Each door
// therefore records its own refusal beside itself — internal.Service.floor
// publishes user.administration_refused, and modules/auth's SetRole publishes
// auth.administration_refused, each in a detached transaction because the
// refused write takes its own transaction down with it. A door that returns this
// error and writes neither has a refusal that nobody will ever read, which is why
// the two records are named on the rule rather than left to the files that call it.
//
// # Who can still repair it, exactly
//
// A customer's tenant is not beyond help, and saying otherwise would be an
// overclaim. The installation's operator can put a fresh administrator into one
// with POST /api/v1/tenant/tenants/{id}/invite, which runs in a system
// transaction and needs no session at the customer's host; it was verified by
// running it against a tenant whose only administrator had been stripped. Two
// things bound that, and a contract has to say them. The route hands out a role
// by name, chosen by the application's adapter — "admin" in this repository's
// composition — so it recovers a tenant whose people lost their grants, not one
// where the named role is itself the role that was emptied; and it exists only
// where an application wires the capability. The operator's own tenant is the
// case with no way back inside the product: that route declares an operator
// permission held through the operator tenant's own roles, so once that tenant
// has nobody who administers it the control plane is shut and what is left is
// SQL. POST /api/v1/tenant/tenants/{id}/suspend against it is one request with
// no reverse route at all, which predates this rule and is outside it — a floor
// under who may administer a tenant cannot help a tenant that is no longer
// served.
//
// The caller must hold the reads, the decision and the write together —
// internal.Service.floor and modules/auth's internal.SetRole each take the
// advisory lock on the tenant before the deciding read, on the same key — or two
// administrators standing down at once both pass this and the tenant ends with
// neither.
//
// What this cannot see is the same list it always was. It reads two sets of ids
// somebody hands it, so it is only as true as the caller's reads: an
// administrator who has forgotten a password nobody can reset for them is a
// lockout no row shows, and a module that someday renames or deletes a role will
// have to answer this rule about the name it removes, because nothing here does.
func CheckedAdministration(before, after Reach, lastWayBack bool, leaving string) error {
	if len(after.CanAdminister) > 0 {
		return nil // somebody would still be able to act; this write takes nothing last
	}
	if len(before.CanAdminister) == 0 && !lastWayBack {
		return nil // the write found nobody, and a write that finds none is not refused
	}
	return fmt.Errorf("%w: this would leave nobody who can sign in and administer this tenant: %s; grant it to somebody already active first",
		crud.ErrInvalid, leaving)
}

// UserReach is this module's door onto that rule: the state of a tenant as
// CheckedAdministration sees it, from the person a write is moving and the other
// people in the tenant who could administer it. after is nil for a delete,
// which leaves no row at all, and the nil reaches CanAdminister's own nil check.
//
// others is every other person's row, read under the same lock: the subject is
// excluded by whoever read it, and everybody else is counted by the same
// predicate the subject is counted by. Both doors here go through this function —
// internal.Service.floor and the conformance fake alike — so the fake's cases
// are the service's cases and the two cannot drift.
//
// The query that fills others is behind this module's cheap gate, not in front
// of it: a write about somebody who never administered the tenant pays for no
// read at all.
func UserReach(u *User, administering []string, others []*User) Reach {
	var out Reach
	if u.CanAdminister(administering) {
		out.CanAdminister = append(out.CanAdminister, u.ID)
	}
	for _, other := range others {
		if other.CanAdminister(administering) {
			out.CanAdminister = append(out.CanAdminister, other.ID)
		}
	}
	return out
}

// LeavingAdministration names, for the refusal, what a write in this module
// takes away: this person, and the administering roles they would no longer
// hold. It says "would", because the sentence is read about a write that did not
// happen; the one before it called the subject "the last person who can still
// sign in", which is false whenever nobody could sign in to begin with, and
// pointed away from the grant that repairs the state.
func LeavingAdministration(u *User, administering []string) string {
	return fmt.Sprintf("%s holds %s, and no active person would still hold it",
		u.Email, strings.Join(held(u, administering), ", "))
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

// Granting answers whether the caller of the write in hand may put a role that
// administers this tenant onto anybody.
//
// It is a port for the same reason Administration is: who holds the permission
// that manages roles is the auth module's table, and this module may not read
// it. The reference application answers it from the Authorizer the API already
// holds, in apps/platformkit/modules.go.
//
// Without it, the roles screen is a self-service door: the generated command
// takes the Spec's own write permission, so anybody who may write a user may put
// an administering role on themselves — and the only floor beneath that was the
// one about not taking the last administrator away, which a first grant does not
// trip. SetRoles asks it only when the write *adds* an administering role the
// person does not already hold, so taking roles away, moving between two
// ordinary roles and re-saving the same set all stay possible for the
// administrator who may write users and nothing else — including the repair
// CheckedAdministration itself recommends, granting the role to somebody active
// first.
//
// user.Deps.Granting is required, in the same words as Deps.Administration: a
// composition that supplies none gets a panic naming this at boot rather than an
// application whose promotions are unguarded.
type Granting interface {
	// May reports whether the caller on ctx may grant an administering role. An
	// error is a decision that could not be made, and the write is refused: the
	// opposite of the floor's rule, because this is a door being opened and not
	// a lock being kept.
	May(ctx context.Context, tx db.Tx[db.Tenant]) (bool, error)
}

// GrantingFunc is the minimal adapter from a function to Granting. Take its
// address, as with AdministrationFunc, so two Deps values stay comparable.
type GrantingFunc struct {
	Ask func(context.Context, db.Tx[db.Tenant]) (bool, error)
}

func (f *GrantingFunc) May(ctx context.Context, tx db.Tx[db.Tenant]) (bool, error) {
	if f == nil || f.Ask == nil {
		return false, errors.New("user: Granting has no Ask")
	}
	return f.Ask(ctx, tx)
}

// RefuseUngrantable is the refusal a caller who may not promote anybody meets.
// It names the rule and the repair, and it is the same shape as the floor's so a
// form shows both above the same checkboxes.
func RefuseUngrantable(role string) error {
	return fmt.Errorf("%w: granting %q needs the permission that manages roles, which this caller does not hold",
		crud.ErrInvalid, role)
}
