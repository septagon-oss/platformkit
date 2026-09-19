// Package usertest is the conformance suite for contracts.Service, and a fake
// that passes it.
package usertest

import (
	"context"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// Fixture is one case's world: a Service, the transaction its commands take,
// and a way to see what was published.
type Fixture struct {
	Ctx     context.Context
	Tx      db.Tx[db.Tenant]
	Service contracts.Service
	// Published is the names of the events published so far, in order. It is
	// what holds an idempotent command to saying nothing the second time.
	Published func() []string
	// Delete is the third door, and the reason it is a closure rather than a
	// method on Service is that deleting a user is generic CRUD: the route is
	// kit/rest's and the floor lives in the Spec's AfterDelete hook, so there
	// is no interface method for the suite to call. Every harness wires it —
	// the real one the way rest.Spec.deleteRow does, the fake its own — because
	// a door tested against one implementation is a door the other can differ
	// on quietly. It is required.
	Delete func(id uuid.UUID) error
}

// Harness builds one Fixture and calls run with it.
type Harness func(t *testing.T, run func(Fixture))

// RunService is the conformance suite. Every implementation of
// contracts.Service passes it, or it is not one.
func RunService(t *testing.T, h Harness) {
	t.Helper()
	all := cases()
	maps.Copy(all, registrationCases())
	maps.Copy(all, verificationCases())
	for name, run := range all {
		t.Run(name, func(t *testing.T) {
			h(t, func(f Fixture) { run(t, f) })
		})
	}
}

// good is a password that passes the length rule; short is one that does not.
const (
	good  = "correct horse battery staple"
	short = "hunter2hunt"
)

// Administering is the role this suite's tenant treats as the one that can
// change a role again — the stand-in for a role granting auth's role:manage.
//
// A harness wires its Service to an Administration that answers exactly this
// and nothing else, so the floor cases and the ordinary role cases can share
// one store: every other name in this suite — admin, member, support — grants
// nothing here, which is why granting and ungrunting them is never refused.
const Administering = "owner"

func cases() map[string]func(*testing.T, Fixture) {
	return map[string]func(*testing.T, Fixture){
		"invite creates somebody who cannot sign in yet": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "Ada@Acme.Example.com", "  Ada Lovelace  ")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if u.Status != contracts.StatusInvited {
				t.Errorf("status is %q, want %q", u.Status, contracts.StatusInvited)
			}
			if u.Email != "ada@acme.example.com" {
				t.Errorf("email is %q; an address that differs only in case is the same mailbox", u.Email)
			}
			if u.DisplayName != "Ada Lovelace" {
				t.Errorf("display name is %q, want it trimmed", u.DisplayName)
			}
			if u.CanSignIn() {
				t.Error("an invited user with no password can sign in")
			}
			published(t, f, contracts.EventInvited)
		},

		"an address belongs to one person in a tenant": func(t *testing.T, f Fixture) {
			if _, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", ""); err != nil {
				t.Fatalf("Invite: %v", err)
			}
			_, err := f.Service.Invite(f.Ctx, f.Tx, "ADA@acme.example.com", "")
			if !errors.Is(err, crud.ErrConflict) {
				t.Errorf("a second invitation to the same address = %v, want ErrConflict", err)
			}
			if _, ok := errors.AsType[*crud.UniqueConflict](err); !ok {
				t.Errorf("the duplicate address lost its unique conflict identity: %v", err)
			}
		},

		"invite refuses something that is not an address": func(t *testing.T, f Fixture) {
			for _, bad := range []string{"", "ada", "ada@", "@acme.example.com", "ada acme@example.com"} {
				if _, err := f.Service.Invite(f.Ctx, f.Tx, bad, ""); !errors.Is(err, crud.ErrInvalid) {
					t.Errorf("Invite(%q) = %v, want ErrInvalid", bad, err)
				}
			}
		},

		"setting a password makes an invited user active": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "Ada")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); err != nil {
				t.Fatalf("SetPassword: %v", err)
			}
			back, err := f.Service.Get(f.Ctx, f.Tx, u.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if back.Status != contracts.StatusActive || !back.CanSignIn() {
				t.Errorf("after SetPassword the user is %q and CanSignIn is %v", back.Status, back.CanSignIn())
			}
			if !back.CheckPassword(good) || back.CheckPassword(good+"!") {
				t.Error("the stored hash does not verify the password it was made from, or verifies another")
			}
			if strings.Contains(back.PasswordHash, good) {
				t.Error("the password is in the stored hash")
			}
			published(t, f, contracts.EventInvited, contracts.EventPasswordSet)
		},

		"a short password is refused": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, short); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("SetPassword with %d characters = %v, want ErrInvalid", len(short), err)
			}
			published(t, f, contracts.EventInvited)
		},

		"setting the same password again is still a password change": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			for range 2 {
				if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); err != nil {
					t.Fatalf("SetPassword: %v", err)
				}
			}
			// Two events, on purpose: somebody who changed their password twice
			// has to see both in their own audit trail.
			published(t, f, contracts.EventInvited, contracts.EventPasswordSet, contracts.EventPasswordSet)
		},

		"roles are granted, normalised, and granted once": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			got, err := f.Service.SetRoles(f.Ctx, f.Tx, u.ID, []string{" Member ", "admin", "admin"})
			if err != nil {
				t.Fatalf("SetRoles: %v", err)
			}
			if !slices.Equal([]string(got.Roles), []string{"admin", "member"}) {
				t.Errorf("roles are %v, want them trimmed, lower-cased, deduplicated and sorted", got.Roles)
			}
			// The same set in another order is the same set: no write, no event.
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, u.ID, []string{"member", "admin"}); err != nil {
				t.Fatalf("SetRoles again: %v", err)
			}
			published(t, f, contracts.EventInvited, contracts.EventRolesSet)
		},

		"the last person who can administer the tenant cannot stop": func(t *testing.T, f Fixture) {
			ada := administrator(t, f, "ada@acme.example.com")
			deleteDoor(t, f)
			// Two clicks on the generated user screen, and each of them used
			// to answer 200: after either, nobody in this tenant could change
			// a role again, through any door, and the installation's operator
			// could not repair it because a session does not cross into a
			// customer's tenant. What is left is SQL.
			_, err := f.Service.SetRoles(f.Ctx, f.Tx, ada.ID, nil)
			if !errors.Is(err, crud.ErrInvalid) {
				t.Fatalf("emptying the last administrator's roles = %v, want ErrInvalid", err)
			}
			if !strings.Contains(err.Error(), Administering) || !strings.Contains(err.Error(), ada.Email) {
				t.Errorf("the refusal names neither the role nor the person: %v", err)
			}
			_, err = f.Service.Deactivate(f.Ctx, f.Tx, ada.ID)
			if !errors.Is(err, crud.ErrInvalid) {
				t.Fatalf("deactivating the last administrator = %v, want ErrInvalid", err)
			}
			// The third door, which is not a command: DELETE {id} soft-deletes
			// the row, ByEmail stops finding it, and the tenant is as locked
			// out as by either of the two above.
			if err := f.Delete(ada.ID); !errors.Is(err, crud.ErrInvalid) {
				t.Fatalf("deleting the last administrator = %v, want ErrInvalid", err)
			}
			// Both refusals left the row alone, and said nothing: a write that
			// did not happen is not an event somebody has to explain.
			got, err := f.Service.Get(f.Ctx, f.Tx, ada.ID)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if !got.Administers([]string{Administering}) {
				t.Errorf("after two refused writes ada is %q holding %v", got.Status, got.Roles)
			}
			published(t, f, contracts.EventInvited, contracts.EventPasswordSet, contracts.EventRolesSet)
		},

		"a second administrator makes the first one ordinary again": func(t *testing.T, f Fixture) {
			// The rule must not be stricter than this, or a tenant that
			// appointed somebody by mistake could never take it back.
			ada := administrator(t, f, "ada@acme.example.com")
			grace := administrator(t, f, "grace@acme.example.com")
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, ada.ID, nil); err != nil {
				t.Fatalf("standing down while grace still administers = %v", err)
			}
			if _, err := f.Service.Deactivate(f.Ctx, f.Tx, grace.ID); !errors.Is(err, crud.ErrInvalid) {
				t.Fatalf("deactivating grace, now the last administrator = %v, want ErrInvalid", err)
			}
			// And somebody who cannot sign in does not hold the floor up for
			// anybody else: ada administers again, is deactivated, and grace is
			// the last one once more.
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, ada.ID, []string{Administering}); err != nil {
				t.Fatalf("granting the role back = %v", err)
			}
			if _, err := f.Service.Deactivate(f.Ctx, f.Tx, ada.ID); err != nil {
				t.Fatalf("deactivating ada while grace still administers = %v", err)
			}
			if _, err := f.Service.Deactivate(f.Ctx, f.Tx, grace.ID); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("a deactivated administrator still counted as one: %v", err)
			}
			if err := f.Delete(grace.ID); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("deleting the last administrator = %v, want ErrInvalid", err)
			}
		},

		"somebody who has not accepted their invitation does not hold the floor up": func(t *testing.T, f Fixture) {
			// The hole this case exists for was reproduced in the reference
			// application's own default configuration, which ships no mail
			// server: invite an heir as an administrator, stand down, and the
			// write answered 200 while the heir's sign-in answered 401 and the
			// outgoing administrator's next request answered 403. An
			// invitation is only a way back in if something delivers it, and
			// nothing here can know whether anything does.
			//
			// So the two halves of the floor are different predicates on
			// purpose. An invited heir is still somebody the floor protects —
			// removing them is removing an appointment — and is not somebody it
			// counts when it asks whether anybody is left.
			ada := administrator(t, f, "ada@acme.example.com")
			heir, err := f.Service.Invite(f.Ctx, f.Tx, "heir@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if heir, err = f.Service.SetRoles(f.Ctx, f.Tx, heir.ID, []string{Administering}); err != nil {
				t.Fatalf("appointing an heir: %v", err)
			}
			if heir.Status != contracts.StatusInvited {
				t.Fatalf("the heir is %q, and this case needs somebody who has not accepted", heir.Status)
			}
			if !heir.Administers([]string{Administering}) {
				t.Error("an invited heir is not one of the tenant's administrators")
			}
			if heir.CanAdminister([]string{Administering}) {
				t.Error("an invited heir counts as somebody who could administer today")
			}
			// Standing down behind them is refused: the heir cannot take over
			// until they accept.
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, ada.ID, nil); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("standing down in favour of an heir who has not accepted = %v, want ErrInvalid", err)
			}
			// And removing the heir is refused too, from the other side: they
			// are an administrator, and ada would be the one left.
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, heir.ID, nil); err != nil {
				t.Errorf("removing an heir while ada can still administer = %v", err)
			}
			// Once the heir accepts — a password makes them active — the
			// handover is allowed.
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, heir.ID, []string{Administering}); err != nil {
				t.Fatalf("re-appointing the heir: %v", err)
			}
			if err := f.Service.SetPassword(f.Ctx, f.Tx, heir.ID, good); err != nil {
				t.Fatalf("the heir accepting: %v", err)
			}
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, ada.ID, nil); err != nil {
				t.Errorf("standing down in favour of an heir who has accepted = %v", err)
			}
		},

		"somebody who never administered can always be removed": func(t *testing.T, f Fixture) {
			// The floor never demands that an administrator exist before any
			// write at all. This case is about the first early return and says
			// so in its name: it used to be called "a tenant nobody can
			// administer can still be written to", which claimed the state
			// below it and exercised this one.
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, u.ID, []string{"member"}); err != nil {
				t.Fatalf("SetRoles member: %v", err)
			}
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, u.ID, nil); err != nil {
				t.Errorf("emptying the roles of somebody who never administered = %v", err)
			}
			if _, err := f.Service.Deactivate(f.Ctx, f.Tx, u.ID); err != nil {
				t.Errorf("deactivating somebody who never administered = %v", err)
			}
		},

		"a tenant whose administrators have all not accepted is refused, and one grant repairs it": func(t *testing.T, f Fixture) {
			// The state the case above used to claim, and the one the two
			// predicates create: every holder of an administering role is
			// invited, so Administers says yes for each and CanAdminister says
			// no for all, and removing any of them is refused while an
			// identical second holder sits there.
			//
			// That is deliberate — unaccepted invitations are the tenant's only
			// thread — but the refusal has to be honest about which of the two
			// things it is saying, because the earlier message said the subject
			// was "the last person who can still sign in", about somebody who
			// could not sign in at all.
			first, err := f.Service.Invite(f.Ctx, f.Tx, "first@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if first, err = f.Service.SetRoles(f.Ctx, f.Tx, first.ID, []string{Administering}); err != nil {
				t.Fatalf("appointing the first: %v", err)
			}
			second, err := f.Service.Invite(f.Ctx, f.Tx, "second@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if _, err = f.Service.SetRoles(f.Ctx, f.Tx, second.ID, []string{Administering}); err != nil {
				t.Fatalf("appointing the second: %v", err)
			}
			err = f.Delete(first.ID)
			if !errors.Is(err, crud.ErrInvalid) {
				t.Fatalf("removing one of two unaccepted administrators = %v, want ErrInvalid", err)
			}
			// The message is about what the write would leave, not about the
			// subject being the last who can sign in — which is false here for
			// everybody, including the one still standing.
			if strings.Contains(err.Error(), "last person who can still sign in") {
				t.Errorf("the refusal calls somebody who cannot sign in the last who can: %v", err)
			}
			if !strings.Contains(err.Error(), "nobody who can sign in") {
				t.Errorf("the refusal does not say what the write would leave: %v", err)
			}
			// And it names the repair, because the repair is not obvious from
			// the state: a grant, not a removal.
			if !strings.Contains(err.Error(), "grant it to somebody already active") {
				t.Errorf("the refusal does not name the way out: %v", err)
			}
			// Which works. Appointing somebody active is never refused — the
			// subject holds no administering role, so the rule returns at its
			// first line — and the removal is then allowed.
			ada := administrator(t, f, "ada@acme.example.com")
			if !ada.CanAdminister([]string{Administering}) {
				t.Fatal("the repair did not produce somebody who can administer today")
			}
			if err := f.Delete(first.ID); err != nil {
				t.Errorf("removing an unaccepted administrator after the repair = %v", err)
			}
		},

		"a role that is not an identifier is refused": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if _, err := f.Service.SetRoles(f.Ctx, f.Tx, u.ID, []string{"Site Admin"}); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("SetRoles with a role that is not an identifier = %v, want ErrInvalid", err)
			}
		},

		"a handle is claimed, folded, and answers to its name": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "Ada Lovelace")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if u.Handle != "" {
				t.Errorf("handle is %q; an invited person has claimed nothing", u.Handle)
			}
			got, err := f.Service.SetHandle(f.Ctx, f.Tx, u.ID, "  Ada  ")
			if err != nil {
				t.Fatalf("SetHandle: %v", err)
			}
			if got.Handle != "ada" {
				t.Errorf("handle is %q, want it trimmed and folded like an address", got.Handle)
			}
			// The whole point of the field: a name typed by a human finds the row.
			found, err := f.Service.ByHandle(f.Ctx, f.Tx, "ADA")
			if err != nil {
				t.Fatalf("ByHandle: %v", err)
			}
			if found.ID != u.ID {
				t.Errorf("ByHandle found %s, want %s", found.ID, u.ID)
			}
			published(t, f, contracts.EventInvited, contracts.EventHandleSet)
		},

		"a rename leaves the person where they were": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if _, err = f.Service.SetHandle(f.Ctx, f.Tx, u.ID, "ada"); err != nil {
				t.Fatalf("SetHandle: %v", err)
			}
			again, err := f.Service.SetHandle(f.Ctx, f.Tx, u.ID, "ada.lovelace")
			if err != nil {
				t.Fatalf("rename: %v", err)
			}
			// The claim this design rests on: the handle is an alias and the uuid is
			// the key, so a rename rewrites nothing and every event, foreign key and
			// audit row naming the old subject still names the same person.
			if again.ID != u.ID {
				t.Errorf("a rename moved the person: %s became %s", u.ID, again.ID)
			}
			if _, err := f.Service.ByHandle(f.Ctx, f.Tx, "ada"); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("the released handle still resolves: %v", err)
			}
			// Two claims, two events: who held `ada` before is the question a rename
			// raises, and it has to have an answer in the trail.
			published(t, f, contracts.EventInvited, contracts.EventHandleSet, contracts.EventHandleSet)
		},

		"a handle already claimed conflicts without saying whose it is": func(t *testing.T, f Fixture) {
			ada, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "Ada Lovelace")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			grace, err := f.Service.Invite(f.Ctx, f.Tx, "grace@acme.example.com", "Grace Hopper")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if _, err = f.Service.SetHandle(f.Ctx, f.Tx, ada.ID, "grace"); err != nil {
				t.Fatalf("first claim: %v", err)
			}
			_, err = f.Service.SetHandle(f.Ctx, f.Tx, grace.ID, "grace")
			if !errors.Is(err, crud.ErrConflict) {
				t.Fatalf("second claim = %v, want ErrConflict", err)
			}
			// The answer must not confirm who is in this tenant to somebody probing
			// for it: the rule, never the holder.
			for _, leak := range []string{"ada@acme.example.com", "Ada Lovelace", ada.ID.String()} {
				if strings.Contains(err.Error(), leak) {
					t.Errorf("the conflict names %q; it may name the rule only: %v", leak, err)
				}
			}
		},

		"names that are not persons are refused, and so is letting one go": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			for _, refused := range []string{"admin", "root", "support", "me", "signin", "null", "platformkit"} {
				if _, err := f.Service.SetHandle(f.Ctx, f.Tx, u.ID, refused); !errors.Is(err, crud.ErrInvalid) {
					t.Errorf("SetHandle(%q) = %v, want ErrInvalid: it names the platform or a door, not a person", refused, err)
				}
			}
			for _, malformed := range []string{"ad", "a b", "ada!", "_ada", "ada.", strings.Repeat("a", 33)} {
				if _, err := f.Service.SetHandle(f.Ctx, f.Tx, u.ID, malformed); !errors.Is(err, crud.ErrInvalid) {
					t.Errorf("SetHandle(%q) = %v, want ErrInvalid", malformed, err)
				}
			}
			// Released is refused: a name somebody can drop while still being called
			// it is a name two people can be answered to.
			if _, err = f.Service.SetHandle(f.Ctx, f.Tx, u.ID, "ada"); err != nil {
				t.Fatalf("SetHandle: %v", err)
			}
			if _, err := f.Service.SetHandle(f.Ctx, f.Tx, u.ID, ""); !errors.Is(err, crud.ErrInvalid) {
				t.Errorf("releasing a handle = %v, want ErrInvalid", err)
			}
			// Every refusal above changed nothing, so exactly one event was published.
			published(t, f, contracts.EventInvited, contracts.EventHandleSet)
		},

		"deactivating stops a sign-in, and twice says nothing": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); err != nil {
				t.Fatalf("SetPassword: %v", err)
			}
			for range 2 {
				got, err := f.Service.Deactivate(f.Ctx, f.Tx, u.ID)
				if err != nil {
					t.Fatalf("Deactivate: %v", err)
				}
				if got.Status != contracts.StatusInactive || got.CanSignIn() {
					t.Errorf("a deactivated user is %q and CanSignIn is %v", got.Status, got.CanSignIn())
				}
			}
			published(t, f, contracts.EventInvited, contracts.EventPasswordSet, contracts.EventDeactivated)
		},

		"a deactivated user cannot be given a password": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			if _, err := f.Service.Deactivate(f.Ctx, f.Tx, u.ID); err != nil {
				t.Fatalf("Deactivate: %v", err)
			}
			if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); !errors.Is(err, crud.ErrConflict) {
				t.Errorf("SetPassword on a deactivated user = %v, want ErrConflict", err)
			}
		},

		"an address finds its person, whatever the case": func(t *testing.T, f Fixture) {
			u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
			if err != nil {
				t.Fatalf("Invite: %v", err)
			}
			got, err := f.Service.ByEmail(f.Ctx, f.Tx, "  ADA@Acme.Example.COM ")
			if err != nil || got.ID != u.ID {
				t.Errorf("ByEmail = %v, %v; want the invited user", got, err)
			}
			if _, err := f.Service.ByEmail(f.Ctx, f.Tx, "nobody@acme.example.com"); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("ByEmail of an address nobody has = %v, want ErrNotFound", err)
			}
		},

		"an unknown id is not found": func(t *testing.T, f Fixture) {
			id := uuid.New()
			if _, err := f.Service.Get(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Get of an unknown user = %v, want ErrNotFound", err)
			}
			if err := f.Service.SetPassword(f.Ctx, f.Tx, id, good); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("SetPassword on an unknown user = %v, want ErrNotFound", err)
			}
			if _, err := f.Service.Deactivate(f.Ctx, f.Tx, id); !errors.Is(err, crud.ErrNotFound) {
				t.Errorf("Deactivate of an unknown user = %v, want ErrNotFound", err)
			}
		},
	}
}

// administrator is somebody this tenant can be administered by today: active,
// with a password, holding Administering. Granting the role is never refused —
// the floor is about a grant leaving — and the password is what makes them
// count on the side of the floor that asks who is left. A case that wants
// somebody who has not accepted yet builds them itself.
func administrator(t *testing.T, f Fixture, email string) *contracts.User {
	t.Helper()
	u, err := f.Service.Invite(f.Ctx, f.Tx, email, "")
	if err != nil {
		t.Fatalf("Invite %s: %v", email, err)
	}
	if err := f.Service.SetPassword(f.Ctx, f.Tx, u.ID, good); err != nil {
		t.Fatalf("SetPassword %s: %v", email, err)
	}
	u, err = f.Service.SetRoles(f.Ctx, f.Tx, u.ID, []string{Administering})
	if err != nil {
		t.Fatalf("SetRoles %s: %v", email, err)
	}
	return u
}

// deleteDoor fails the case when a harness did not wire Fixture.Delete, rather
// than passing quietly with one of the three doors unexercised.
func deleteDoor(t *testing.T, f Fixture) {
	t.Helper()
	if f.Delete == nil {
		t.Fatal("this harness wired no Fixture.Delete, so the delete door is untested against this implementation")
	}
}

func published(t *testing.T, f Fixture, want ...string) {
	t.Helper()
	if f.Published == nil {
		return
	}
	if got := f.Published(); !slices.Equal(got, want) {
		t.Errorf("published %v, want %v", got, want)
	}
}
