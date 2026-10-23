package authtest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"

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
// Twelve cases, all of them §9's. The one this suite reads differently from the
// SQL service is `no factor key is nobody's business`: there is no factor key in
// the fake at all, so the passkey doors answering normally is the ground this
// case runs on, and TOTP enrolment's ErrNoFactorKey is not asked — the fake has
// no secret half to refuse (see FactorList). What the case does hold the
// implementation to is the part that outlives risk 3: a person holding only
// passkeys is held at the first door, and their passkey answers the second.
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
		// The tenant's passkey door, read rather than refused. It is the read a
		// sign-in screen makes before it decides what to offer, so the case that
		// matters is not the boolean but its agreement with the door: the answer
		// must be the one BeginPasskeySignIn is about to act on.
		//
		// There is no second field on this fixture for it, on purpose: the read
		// is taken from the same object the ceremony runs on, which is the only
		// way this case can refuse an implementation that answers the door from
		// somewhere other than the row the door reads.
		"the tenant's passkey door answers what the door itself acts on":             readPasskeyDoor,
		"a passkey is listed beside a totp, with its name and no material":           listPasskeyWithTOTP,
		"the last factor of any kind refuses withdrawal":                             preserveLastFactor,
		"a passkey and a totp let either be withdrawn, until one is left":            withdrawEitherFactor,
		"an unknown factor id is not found, in either table":                         refuseUnknownFactor,
		"a ceremony begun for another person enrols nothing":                         bindCeremonyToOwner,
		"a ceremony begun at one door answers none other":                            bindCeremonyToDoor,
		"a spent ceremony answers the same as an unknown one":                        refuseSpentCeremony,
		"an expired ceremony enrols nothing":                                         refuseExpiredCeremony,
		"one authenticator is one person's factor":                                   preserveCredentialOwner,
		"a person with no factor is refused recovery codes; a passkey ends that":     requireFactorForRecovery,
		"no factor key is nobody's business":                                         usePasskeyWithoutFactorKey,
		"a tenant that has not enabled passkey sign-in is refused at that door only": refuseDisabledPasskeyDoor,
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
