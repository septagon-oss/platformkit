package authtest

import (
	"errors"
	"testing"

	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func readPasskeyDoor(t *testing.T, f PasskeyFixture) {
	door, ok := f.Passkeys.(contracts.PasskeyDoor)
	if !ok {
		t.Fatal("this implementation of Passkeys does not answer PasskeySignInEnabled, so a sign-in screen cannot ask what it will refuse")
	}
	want := []bool{false, true, false}
	for _, enabled := range want {
		f.EnablePasskeySignIn(enabled)
		got, err := door.PasskeySignInEnabled(f.Ctx, f.Tx)
		if err != nil {
			t.Fatalf("PasskeySignInEnabled: %v", err)
		}
		if got != enabled {
			t.Errorf("the door says %v after it was set to %v", got, enabled)
		}
		// The half that keeps the read from disagreeing with the door:
		// begun, or refused with the reason, exactly as the answer says.
		_, err = f.Passkeys.BeginPasskeySignIn(f.Ctx, f.Tx)
		if enabled && err != nil {
			t.Errorf("BeginPasskeySignIn with the door on: %v", err)
		}
		if !enabled && !errors.Is(err, contracts.ErrPasskeySignInOff) {
			t.Errorf("BeginPasskeySignIn with the door off = %v, want ErrPasskeySignInOff", err)
		}
	}
}

func usePasskeyWithoutFactorKey(t *testing.T, f PasskeyFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	// No factor yet: the password signs her in alone, which is the state
	// the platform stays in until a factor is chosen rather than imposed.
	session, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody)
	if err != nil || session == nil {
		t.Fatalf("Login with no factor = %v, want a session", err)
	}
	enrol(t, f, ada, theCred, "Office laptop")
	// Now the password is one half, and the door says so. A person told
	// otherwise reaches for the reset link, which is how a second factor
	// becomes a password reset queue.
	if _, _, err := f.Service.Login(f.Ctx, f.Tx, "ada@acme.example.com", Password, nobody); !errors.Is(err, contracts.ErrFactorRequired) {
		t.Fatalf("Login for a passkey-only account = %v, want ErrFactorRequired", err)
	}
	// The proof that refusal minted is what makes the passkey answerable.
	begin, err := f.Passkeys.BeginPasskeyAssertion(f.Ctx, f.Tx, "ada@acme.example.com")
	if err != nil {
		t.Fatalf("BeginPasskeyAssertion: %v", err)
	}
	session, _, err = f.Passkeys.FinishPasskeyAssertion(f.Ctx, f.Tx, begin.Ceremony,
		f.Answer(*begin, theCred), nobody)
	if err != nil || session == nil || session.UserID != ada {
		t.Fatalf("the second factor = %v with session %+v, want one for its owner", err, session)
	}
	// And a code is not a first factor: the same door, with no refused
	// sign-in behind it, is the answer a wrong code gets.
	again, err := f.Passkeys.BeginPasskeyAssertion(f.Ctx, f.Tx, "ada@acme.example.com")
	if err != nil {
		t.Fatalf("BeginPasskeyAssertion again: %v", err)
	}
	if _, _, err := f.Passkeys.FinishPasskeyAssertion(f.Ctx, f.Tx, again.Ceremony,
		f.Answer(*again, theCred), nobody); !errors.Is(err, contracts.ErrCredentials) {
		t.Errorf("a second factor with no refused sign-in behind it = %v, want ErrCredentials", err)
	}
	published(t, f.Fixture, contracts.EventLoggedIn, contracts.EventFactorEnrolled,
		contracts.EventFactorUsed, contracts.EventLoggedIn)
}

func refuseDisabledPasskeyDoor(t *testing.T, f PasskeyFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	f.EnablePasskeySignIn(false)
	if _, err := f.Passkeys.BeginPasskeySignIn(f.Ctx, f.Tx); !errors.Is(err, contracts.ErrPasskeySignInOff) {
		t.Fatalf("the usernameless door at a tenant that never opened it = %v, want ErrPasskeySignInOff", err)
	}
	if _, err := f.Passkeys.BeginPasskeyRegistration(f.Ctx, f.Tx, ada); err != nil {
		t.Errorf("the door being shut stopped enrolment: %v", err)
	}
	if _, err := f.Passkeys.BeginPasskeyAssertion(f.Ctx, f.Tx, "ada@acme.example.com"); err != nil {
		t.Errorf("the door being shut stopped the second-factor leg: %v", err)
	}
	published(t, f.Fixture)
}
