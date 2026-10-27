package internal_test

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// The lockout refuses one door and revokes nothing. Ten wrong answers at the sign-in
// page stop that page for a window, and the link a customer already mailed stays
// spendable, because what makes a mailed invitation unusable is its own lifecycle —
// spent by an acceptance, retired by a revoke, outlived by its expiry, or replaced by a
// resend — and never what happened at a password form. The distinction is worth its own
// case: a lockout is something a stranger can bring on one address by typing at it, so a
// build that let the counter reach the handoff would hand that stranger the revocation
// this module reserves to an administrator's command.
//
// This invites one person, spends the whole allowance of wrong answers against that
// address, and asks the page to refuse the right password, which is the probe that says
// the lockout is full rather than merely assumed. It then completes the invitation
// through the link the same customer mailed, and reads the person back as accepted and
// active, with no set-password row left to spend. The lockout is still in force across
// those two steps: nothing here signs the address in, so a handoff that consulted the
// account counter, retired the token, or refused to accept while the page was shut is
// refused here, and a handoff that ignores the counter passes.
func TestASignInLockoutLeavesTheMailedInvitationLinkSpendable(t *testing.T) {
	const (
		asked  = "locked.out.person@example.com"
		chosen = "a sufficiently strong password"
		wrong  = "not the password anybody was invited with"
	)
	f := invitationSQL(t)
	invited := invitedIn(t, f, f.ctx, asked)
	link := offeredIn(t, f, f.ctx, invited)
	if link == "" {
		t.Fatalf("no link was mailed to %s, so no invitation was waiting for a lockout to reach", asked)
	}
	for n := range contracts.MaxAttempts {
		if _, _, err := signedInAt(t, f, f.ctx, asked, wrong); !errors.Is(err, contracts.ErrCredentials) {
			t.Fatalf("failure %d at the sign-in page returned %v, want the wrong-answer refusal it is counting",
				n+1, err)
		}
	}
	if _, _, err := signedInAt(t, f, f.ctx, asked, chosen); !errors.Is(err, contracts.ErrTooManyAttempts) {
		t.Fatalf("the sign-in page answered its own address %v after the %d failures it counted, want its lockout: "+
			"without a door known to be shut, the link below would say nothing about the counter",
			err, contracts.MaxAttempts)
	}
	session, err := setPasswordIn(t, f, f.ctx, link)
	if err != nil {
		t.Fatalf("%s completing its invitation through the link it was mailed returned %v while the sign-in page was "+
			"locked, want that link spendable: a lockout refuses a password form, it does not revoke an invitation",
			asked, err)
	}
	if session.UserID != invited.ID {
		t.Errorf("the mailed link opened %s, want the invited person %s it was mailed for",
			session.UserID, invited.ID)
	}
	if left := liveTokens(t, f, f.ctx, invited.ID); left != 0 {
		t.Errorf("%d set-password rows of %s are still live after the link was spent, want the acceptance to have taken it",
			left, asked)
	}
	if got := personSeenIn(t, f, f.ctx, invited.ID); got.Status != user.StatusActive ||
		got.Invitation.Phase != user.InvitationAccepted || got.PasswordHash == "" {
		t.Errorf("%s reads %q in phase %q with its password set %t after completing its invitation under a lockout, "+
			"want an active accepted membership",
			asked, got.Status, got.Invitation.Phase, got.PasswordHash != "")
	}
}
