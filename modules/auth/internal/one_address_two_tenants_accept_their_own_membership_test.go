package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// One address is unique inside a customer and not across them, so two customers of one
// installation can each invite their own person to it and hold rows keyed identically on
// the one column a person types. A set-password link names the invited row, never the
// address, so completing one customer's invitation has to leave the same address's pending
// person in the other customer pending. This asks that: each customer mails its own link for
// the one address, the second customer is handed the first customer's live link and refused,
// the first then sets its password through its own link, and the shared address's other person
// is read in its own customer's transaction — pending, no password set, and its own link still
// spendable there. Each claim names the tenant and the person it is about, so a read that
// reached no row refuses rather than passing on an empty result. The cross-customer offer is
// tried while that link is still live, and its own tenant spends it afterwards, so an offer that
// reached past the tenant it was handed in, or a refusal that consumed a credential it should
// not have touched, is refused here rather than passing on a link the acceptance had spent.
func TestOneAddressTwoTenantsAcceptTheirOwnMembership(t *testing.T) {
	const shared = "single.person@example.com"
	f := invitationSQL(t)
	globex := globexSession(t, f)
	abroad := invitedIn(t, f, globex, shared)
	atHome := invitedIn(t, f, f.ctx, shared)
	if abroad.ID == atHome.ID {
		t.Fatalf("both customers invited the one person %s for %s, want one invited person per customer",
			abroad.ID, shared)
	}
	abroadLink := offeredIn(t, f, globex, abroad)
	homeLink := offeredIn(t, f, f.ctx, atHome)
	if abroadLink == "" {
		t.Fatalf("no link was mailed to %s in the tenant that is to accept first, so nothing there could be completed", shared)
	}
	if homeLink == "" {
		t.Fatalf("no link was mailed to %s in the other tenant, so its pending person could not be compared", shared)
	}
	if session, err := setPasswordIn(t, f, f.ctx, abroadLink); err == nil {
		t.Errorf("the second tenant was handed the link mailed to %s by the first customer and was answered with %v, want that offer refused where the link was not mailed",
			shared, session)
	}
	if untouched := personSeenIn(t, f, f.ctx, atHome.ID); untouched.Invitation.Phase != user.InvitationPending ||
		untouched.PasswordHash != "" {
		t.Errorf("the shared address reads phase %q with its password set %t in the second tenant after a refused offer there, want its own person untouched",
			untouched.Invitation.Phase, untouched.PasswordHash != "")
	}
	if _, err := setPasswordIn(t, f, globex, abroadLink); err != nil {
		t.Fatalf("%s setting its password through the link mailed to it in its own tenant returned %v, want that link still live after another tenant was refused it",
			shared, err)
	}
	if got := personSeenIn(t, f, globex, abroad.ID); got.Status != user.StatusActive ||
		got.Invitation.Phase != user.InvitationAccepted || got.PasswordHash == "" {
		t.Errorf("%s reads %q in phase %q with its password set %t in the tenant that accepted, want an active accepted membership",
			shared, got.Status, got.Invitation.Phase, got.PasswordHash != "")
	}
	if kept := personSeenIn(t, f, f.ctx, atHome.ID); kept.Invitation.Phase != user.InvitationPending ||
		kept.PasswordHash != "" {
		t.Errorf("the same address %s reads phase %q with its password set %t in the other tenant after its acceptance next door, want it untouched and still pending",
			shared, kept.Invitation.Phase, kept.PasswordHash != "")
	}
	if _, err := setPasswordIn(t, f, f.ctx, homeLink); err != nil {
		t.Fatalf("%s setting its password through the link its own customer mailed returned %v, want that link still spendable after the same address was accepted in another tenant",
			shared, err)
	}
	if got := personSeenIn(t, f, f.ctx, atHome.ID); got.Status != user.StatusActive ||
		got.Invitation.Phase != user.InvitationAccepted {
		t.Errorf("%s reads %q in phase %q after spending its own customer's link, want its own active accepted membership",
			shared, got.Status, got.Invitation.Phase)
	}
}

// setPasswordIn takes one tenant's own set-password handoff through the door of that
// tenant and answers what the service returned to that tenant.
func setPasswordIn(t *testing.T, f invitationSQLFixture, ctx context.Context, link string) (*contracts.Session, error) {
	t.Helper()
	var session *contracts.Session
	err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		session, err = f.Auth.Reset(ctx, tx, link, "a sufficiently strong password", contracts.Client{})
		return err
	})
	return session, err
}

// personSeenIn reads one invited person as the tenant named can see it: a person of
// another customer is no row at all here, which is the answer this asks against.
func personSeenIn(t *testing.T, f invitationSQLFixture, ctx context.Context, id uuid.UUID) *user.User {
	t.Helper()
	var got *user.User
	if err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		got, err = f.Users.Get(ctx, tx, id)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return got
}
