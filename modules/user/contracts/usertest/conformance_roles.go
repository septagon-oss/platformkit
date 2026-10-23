package usertest

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

func normaliseRoles(t *testing.T, f Fixture) {
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
}

func preserveLastAdministrator(t *testing.T, f Fixture) {
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
	// Both refusals left the row alone. What they did not leave unsaid is
	// the attempt: each refused door is one record on the trail, and the
	// write behind it is not there, because a write that did not happen is
	// not an event anybody has to explain — the refusal is.
	got, err := f.Service.Get(f.Ctx, f.Tx, ada.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Administers([]string{Administering}) {
		t.Errorf("after two refused writes ada is %q holding %v", got.Status, got.Roles)
	}
	published(t, f, contracts.EventInvited, contracts.EventPasswordSet, contracts.EventRolesSet,
		contracts.EventAdministrationRefused, contracts.EventAdministrationRefused,
		contracts.EventAdministrationRefused)
}

func replaceAdministrator(t *testing.T, f Fixture) {
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
}

func disregardInvitedAdministrator(t *testing.T, f Fixture) {
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
}

func removeOrdinaryUser(t *testing.T, f Fixture) {
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
}

func repairAdministrationFloor(t *testing.T, f Fixture) {
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
}

func refuseInvalidRole(t *testing.T, f Fixture) {
	u, err := f.Service.Invite(f.Ctx, f.Tx, "ada@acme.example.com", "")
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	if _, err := f.Service.SetRoles(f.Ctx, f.Tx, u.ID, []string{"Site Admin"}); !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("SetRoles with a role that is not an identifier = %v, want ErrInvalid", err)
	}
}
