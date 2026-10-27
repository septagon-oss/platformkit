package internal_test

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// One address two customers each invited their own person to is one mailbox two
// doors answer for, and the sign-in counters decide whether one customer can be
// used to lock the other out. Each counter is keyed by the customer of the
// transaction that asked for it, so failures spent at one door are nothing at
// the other. This spends the whole allowance at the first customer's door, asks
// that door to refuse its own person afterwards, and asks the second door to
// open its own person with the password the two share. One counter behind both
// doors would answer the second door with ErrTooManyAttempts, which is one
// customer locking another customer's member out by typing the wrong password
// at its own login page.
func TestOneAddressTwoCustomersDoNotShareASignInLockout(t *testing.T) {
	const (
		shared = "single.person@example.com"
		chosen = "a sufficiently strong password"
		wrong  = "not the password anybody was invited with"
	)
	f := invitationSQL(t)
	globex := globexSession(t, f)
	abroad := invitedIn(t, f, globex, shared)
	atHome := invitedIn(t, f, f.ctx, shared)
	abroadLink := offeredIn(t, f, globex, abroad)
	homeLink := offeredIn(t, f, f.ctx, atHome)
	if abroadLink == "" || homeLink == "" {
		t.Fatalf("the links mailed for %s were %q and %q, want one live link per customer to complete",
			shared, abroadLink, homeLink)
	}
	if _, err := setPasswordIn(t, f, globex, abroadLink); err != nil {
		t.Fatalf("%s completing its invitation in the first customer returned %v, want two active people at %s",
			shared, err, shared)
	}
	if _, err := setPasswordIn(t, f, f.ctx, homeLink); err != nil {
		t.Fatalf("%s completing its invitation in the second customer returned %v, want two active people at %s",
			shared, err, shared)
	}
	for n := range contracts.MaxAttempts {
		if _, _, err := signedInAt(t, f, globex, shared, wrong); !errors.Is(err, contracts.ErrCredentials) {
			t.Fatalf("failure %d at the first customer's door returned %v, want the wrong-answer refusal it is counting",
				n+1, err)
		}
	}
	// The first door has counted itself shut. Asking it is what makes the answer
	// below mean something: a counter that never filled would leave the second
	// door's success unexplained rather than separate.
	if _, _, err := signedInAt(t, f, globex, shared, chosen); !errors.Is(err, contracts.ErrTooManyAttempts) {
		t.Fatalf("the first customer's door answered its own person %v after the %d failures it counted, want its lockout",
			err, contracts.MaxAttempts)
	}
	session, identity, err := signedInAt(t, f, f.ctx, shared, chosen)
	if err != nil {
		t.Fatalf("the second customer's door answered its own person %v after the first customer's %d failures, want a session: that customer counted them, not this one",
			err, contracts.MaxAttempts)
	}
	if session.UserID != atHome.ID || identity.UserID != atHome.ID {
		t.Errorf("the second customer's door opened %s (identity %s), want its own person %s",
			session.UserID, identity.UserID, atHome.ID)
	}
}
