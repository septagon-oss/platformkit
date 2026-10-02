package authtest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// FactorList is the half of contracts.Factors the ceremony cases ask about: the
// list, the withdrawal and the recovery codes. The secret half — BeginTOTP,
// FinishTOTP, VerifySecondFactor, RequireFirstFactorProof — is deliberately not
// asked here: it turns on an HMAC secret the fake would have to mint and check,
// which is the same rebuilding the ceremony half refuses. A case adds a factor of
// the other kind through AddFactor and asks the questions that are about rows.
type FactorList interface {
	ListFactors(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) ([]*contracts.Factor, error)
	WithdrawFactor(ctx context.Context, tx db.Tx[db.Tenant], userID, factor uuid.UUID) error
	RotateRecoveryCodes(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) ([]string, error)
}

// PasskeyFixture is one ceremony case's world: the ordinary service fixture, plus
// the two halves a ceremony needs and the three things a case cannot do for
// itself — answer as an authenticator, move the clock, and set the tenant's door.
type PasskeyFixture struct {
	Fixture

	// Passkeys and Factors are the implementation under test. Factors is the
	// narrow interface above, not all of contracts.Factors.
	Passkeys contracts.Passkeys
	Factors  FactorList

	// Answer is the authenticator's half of the case. It turns a begun
	// ceremony and a credential id into the body an authenticator holding that
	// credential would send. The fake's answer carries the id and nothing else;
	// a signature-checking implementation answers here with a real assertion
	// over the challenge in challenge.Options, and every case below then reads
	// the same way against both implementations.
	Answer func(ceremony contracts.PasskeyChallenge, credential string) json.RawMessage

	// AddFactor stands in for a factor this person holds that the case did not
	// enrol through a ceremony — the TOTP row the shared rules are counted
	// against. It returns the factor's id.
	AddFactor func(userID uuid.UUID, kind, name, credential string) uuid.UUID

	// ExpireCeremony stands the clock past PasskeyChallengeWindow for one
	// begun ceremony. A case never waits two minutes for a row the contract
	// says lives exactly that long, and the real service's harness answers this
	// with an UPDATE of expires_at.
	ExpireCeremony func(ceremony uuid.UUID)

	// EnablePasskeySignIn is the tenant's own row at the usernameless door.
	EnablePasskeySignIn func(enabled bool)
}

// PasskeyHarness builds one PasskeyFixture and calls run with it.
type PasskeyHarness func(t *testing.T, run func(PasskeyFixture))

// RunPasskeys is the conformance suite for the ceremony half: the rules about
// rows, doors, owners and counts, written once so a second implementation has to
// read them the same way.
//
// One case is not here, and saying so is part of the suite. SPECIFY §9's
// `no factor key is nobody's business` asks the first door — Login — to hold a
// passkey-only person at ErrFactorRequired while both passkey doors answer
// normally. The fake's Login does not do that yet: it opens the session a factor
// should hold at, so the case would assert an affordance rather than a rule. It
// is the SQL service's case today (modules/auth/internal
// TestTheUsernamelessDoorIsTheTenantsOwnRow and the second-factor pins there), and
// it comes back here when the fake's Login mints the first-factor proof.
func RunPasskeys(t *testing.T, h PasskeyHarness) {
	t.Helper()
	for name, run := range passkeyCases() {
		t.Run(name, func(t *testing.T) {
			h(t, func(f PasskeyFixture) { run(t, f) })
		})
	}
}

// theCred is the credential id a case enrols, and the one a second case reuses to
// ask whether one authenticator can be two people's factor.
const theCred = "aW1hZ2luYXJ5LWNyZWRlbnRpYWw"

func passkeyCases() map[string]func(*testing.T, PasskeyFixture) {
	return map[string]func(*testing.T, PasskeyFixture){
		"a passkey is listed beside a totp, with its name and no material": func(t *testing.T, f PasskeyFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			f.AddFactor(ada, FactorTOTP, "", "totp-secret")
			key := enrol(t, f, ada, theCred, "Office laptop")
			list, err := f.Factors.ListFactors(f.Ctx, f.Tx, ada)
			if err != nil {
				t.Fatalf("ListFactors: %v", err)
			}
			if len(list) != 2 {
				t.Fatalf("this person holds %d factors, want a passkey and a totp", len(list))
			}
			kinds := []string{list[0].Kind, list[1].Kind}
			if !slices.Contains(kinds, FactorPasskey) || !slices.Contains(kinds, FactorTOTP) {
				t.Errorf("the list holds kinds %v, want both passkey and totp", kinds)
			}
			for _, factor := range list {
				if factor.Kind == FactorTOTP && factor.Name != "" {
					t.Errorf("a totp is named %q, where an authenticator app has no name to give", factor.Name)
				}
				if factor.Kind == FactorPasskey && factor.Name != "Office laptop" {
					t.Errorf("the passkey is named %q, want the label its owner typed", factor.Name)
				}
				if factor.ID != key.ID && factor.Kind != FactorTOTP {
					t.Errorf("the list holds a second passkey %+v", factor)
				}
				if strings.Contains(strings.ToLower(factor.Name), "credential") {
					t.Errorf("the name carries ceremony material: %q", factor.Name)
				}
			}
			published(t, f.Fixture, contracts.EventFactorEnrolled)
		},

		"the last factor of any kind refuses withdrawal": func(t *testing.T, f PasskeyFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			key := enrol(t, f, ada, theCred, "Only way in")
			if err := f.Factors.WithdrawFactor(f.Ctx, f.Tx, ada, key.ID); !errors.Is(err, contracts.ErrLastFactor) {
				t.Errorf("withdrawing the only passkey = %v, want ErrLastFactor", err)
			}
			list, _ := f.Factors.ListFactors(f.Ctx, f.Tx, ada)
			if len(list) != 1 {
				t.Errorf("the refusal left %d factors, want the one it refused to take", len(list))
			}
			published(t, f.Fixture, contracts.EventFactorEnrolled)
		},

		"a passkey and a totp let either be withdrawn, until one is left": func(t *testing.T, f PasskeyFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			totp := f.AddFactor(ada, FactorTOTP, "", "totp-secret")
			key := enrol(t, f, ada, theCred, "Office laptop")
			if err := f.Factors.WithdrawFactor(f.Ctx, f.Tx, ada, key.ID); err != nil {
				t.Fatalf("withdraw the passkey while a totp is still held: %v", err)
			}
			list, _ := f.Factors.ListFactors(f.Ctx, f.Tx, ada)
			if len(list) != 1 || list[0].Kind != FactorTOTP {
				t.Fatalf("withdrawing the passkey left %+v, want the totp", list)
			}
			// And the rule holds at the end of the road too: two factors became
			// one, and one is the last.
			if err := f.Factors.WithdrawFactor(f.Ctx, f.Tx, ada, totp); !errors.Is(err, contracts.ErrLastFactor) {
				t.Errorf("withdrawing what the first withdrawal left = %v, want ErrLastFactor", err)
			}
			published(t, f.Fixture, contracts.EventFactorEnrolled, contracts.EventFactorWithdrawn)
		},

		"an unknown factor id is not found, in either table": func(t *testing.T, f PasskeyFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			f.AddFactor(ada, FactorTOTP, "", "totp-secret")
			key := enrol(t, f, ada, theCred, "Office laptop")
			other := f.User("bob@acme.example.com", Password, contracts.RoleMember)
			for _, id := range []uuid.UUID{uuid.New(), key.ID} {
				if err := f.Factors.WithdrawFactor(f.Ctx, f.Tx, other, id); !errors.Is(err, crud.ErrNotFound) {
					t.Errorf("withdrawing %s from somebody who does not hold it = %v, want ErrNotFound", id, err)
				}
			}
			published(t, f.Fixture, contracts.EventFactorEnrolled)
		},

		"a ceremony begun for another person enrols nothing": func(t *testing.T, f PasskeyFixture) {
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
		},

		"a ceremony begun at one door answers none other": func(t *testing.T, f PasskeyFixture) {
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
		},

		"a spent ceremony answers the same as an unknown one": func(t *testing.T, f PasskeyFixture) {
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
		},

		"an expired ceremony enrols nothing": func(t *testing.T, f PasskeyFixture) {
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
		},

		"one authenticator is one person's factor": func(t *testing.T, f PasskeyFixture) {
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
		},

		"a person with no factor is refused recovery codes; a passkey ends that": func(t *testing.T, f PasskeyFixture) {
			ada := f.User("ada@acme.example.com", Password, contracts.RoleAdmin)
			if _, err := f.Factors.RotateRecoveryCodes(f.Ctx, f.Tx, ada); !errors.Is(err, contracts.ErrNoFactor) {
				t.Fatalf("codes for a person with no factor = %v, want ErrNoFactor", err)
			}
			enrol(t, f, ada, theCred, "Office laptop")
			codes, err := f.Factors.RotateRecoveryCodes(f.Ctx, f.Tx, ada)
			if err != nil {
				t.Fatalf("codes after a passkey: %v", err)
			}
			if len(codes) != contracts.RecoveryCodes {
				t.Errorf("the set holds %d codes, want %d", len(codes), contracts.RecoveryCodes)
			}
			published(t, f.Fixture, contracts.EventFactorEnrolled, contracts.EventRecoveryCodesIssued)
		},

		"a tenant that has not enabled passkey sign-in is refused at that door only": func(t *testing.T, f PasskeyFixture) {
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
		},
	}
}

// enrol runs both legs for a person and returns the factor, failing the case if
// either leg refuses: most of these cases start from somebody who holds a
// passkey, and a case that began from a refusal it did not mean to make reports
// the wrong failure later.
func enrol(t *testing.T, f PasskeyFixture, userID uuid.UUID, credential, name string) *contracts.Factor {
	t.Helper()
	begin, err := f.Passkeys.BeginPasskeyRegistration(f.Ctx, f.Tx, userID)
	if err != nil {
		t.Fatalf("BeginPasskeyRegistration: %v", err)
	}
	factor, err := f.Passkeys.FinishPasskeyRegistration(f.Ctx, f.Tx, userID, begin.Ceremony,
		f.Answer(*begin, credential), name)
	if err != nil {
		t.Fatalf("FinishPasskeyRegistration: %v", err)
	}
	return factor
}
