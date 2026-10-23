package authtest

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

func bindCeremonyToOwner(t *testing.T, f PasskeyFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	bob := f.User("bob@acme.example.com", Password, contracts.RoleMember)
	ceremony, err := f.Passkeys.BeginPasskeyRegistration(f.Ctx, f.Tx, ada)
	if err != nil {
		t.Fatalf("BeginPasskeyRegistration: %v", err)
	}
	_, err = f.Passkeys.FinishPasskeyRegistration(f.Ctx, f.Tx, bob, ceremony.Ceremony,
		f.Answer(*ceremony, theCred), "Somebody else's laptop")
	if !errors.Is(err, contracts.ErrCredentials) {
		t.Errorf("answering another person's ceremony = %v, want ErrCredentials", err)
	}
	noneYet, _ := f.Factors.ListFactors(f.Ctx, f.Tx, bob)
	if len(noneYet) != 0 {
		t.Errorf("the refused enrolment left %d factors on the caller", len(noneYet))
	}
	published(t, f.Fixture)
}

func bindCeremonyToDoor(t *testing.T, f PasskeyFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	f.EnablePasskeySignIn(true)
	registrar, err := f.Passkeys.BeginPasskeyRegistration(f.Ctx, f.Tx, ada)
	if err != nil {
		t.Fatalf("BeginPasskeyRegistration: %v", err)
	}
	if _, _, err := f.Passkeys.FinishPasskeyAssertion(f.Ctx, f.Tx, registrar.Ceremony,
		f.Answer(*registrar, theCred), nobody); !errors.Is(err, contracts.ErrCredentials) {
		t.Errorf("an enrolment prompt answered at a sign-in door = %v, want ErrCredentials", err)
	}
	fixture, err := f.Passkeys.BeginPasskeySignIn(f.Ctx, f.Tx)
	if err != nil {
		t.Fatalf("BeginPasskeySignIn: %v", err)
	}
	if _, err := f.Passkeys.FinishPasskeyRegistration(f.Ctx, f.Tx, ada, fixture.Ceremony,
		f.Answer(*fixture, theCred), "A sign-in prompt"); !errors.Is(err, contracts.ErrCredentials) {
		t.Errorf("a sign-in prompt answered at the enrolment door = %v, want ErrCredentials", err)
	}
	noneYet, _ := f.Factors.ListFactors(f.Ctx, f.Tx, ada)
	if len(noneYet) != 0 {
		t.Errorf("two door refusals left %d factors, want none", len(noneYet))
	}
	published(t, f.Fixture)
}

func refuseSpentCeremony(t *testing.T, f PasskeyFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	enrol(t, f, ada, theCred, "Office laptop")
	f.EnablePasskeySignIn(true)
	begin, err := f.Passkeys.BeginPasskeySignIn(f.Ctx, f.Tx)
	if err != nil {
		t.Fatalf("BeginPasskeySignIn: %v", err)
	}
	answer := f.Answer(*begin, theCred)
	session, _, err := f.Passkeys.FinishPasskeyAssertion(f.Ctx, f.Tx, begin.Ceremony, answer, nobody)
	if err != nil || session == nil || session.UserID != ada {
		t.Fatalf("the first answer = %v with session %+v, want one for the passkey's owner", err, session)
	}
	_, _, replay := f.Passkeys.FinishPasskeyAssertion(f.Ctx, f.Tx, begin.Ceremony, answer, nobody)
	_, _, unknown := f.Passkeys.FinishPasskeyAssertion(f.Ctx, f.Tx, uuid.New(), answer, nobody)
	if !errors.Is(replay, contracts.ErrCredentials) || !errors.Is(unknown, contracts.ErrCredentials) {
		t.Fatalf("a replay = %v and an unknown ceremony = %v; both are ErrCredentials", replay, unknown)
	}
	if replay.Error() != unknown.Error() {
		t.Errorf("the two refusals read differently: %q and %q", replay, unknown)
	}
}

func refuseExpiredCeremony(t *testing.T, f PasskeyFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	begin, err := f.Passkeys.BeginPasskeyRegistration(f.Ctx, f.Tx, ada)
	if err != nil {
		t.Fatalf("BeginPasskeyRegistration: %v", err)
	}
	f.ExpireCeremony(begin.Ceremony)
	_, err = f.Passkeys.FinishPasskeyRegistration(f.Ctx, f.Tx, ada, begin.Ceremony,
		f.Answer(*begin, theCred), "Too late")
	if err == nil {
		t.Fatal("an expired prompt enrolled a factor")
	}
	noneYet, _ := f.Factors.ListFactors(f.Ctx, f.Tx, ada)
	if len(noneYet) != 0 {
		t.Errorf("the expired prompt enrolled %d factors, want none", len(noneYet))
	}
	published(t, f.Fixture)
}

func preserveCredentialOwner(t *testing.T, f PasskeyFixture) {
	ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
	bob := f.User("bob@acme.example.com", Password, contracts.RoleMember)
	enrol(t, f, ada, theCred, "Office laptop")
	begin, err := f.Passkeys.BeginPasskeyRegistration(f.Ctx, f.Tx, bob)
	if err != nil {
		t.Fatalf("BeginPasskeyRegistration: %v", err)
	}
	_, err = f.Passkeys.FinishPasskeyRegistration(f.Ctx, f.Tx, bob, begin.Ceremony,
		f.Answer(*begin, theCred), "Somebody else's laptop")
	if !errors.Is(err, contracts.ErrPasskeyExists) {
		t.Errorf("the same authenticator for a second account = %v, want ErrPasskeyExists", err)
	}
	held, _ := f.Factors.ListFactors(f.Ctx, f.Tx, bob)
	if len(held) != 0 {
		t.Errorf("the refused enrolment gave the second account %d factors", len(held))
	}
	published(t, f.Fixture, contracts.EventFactorEnrolled)
}
