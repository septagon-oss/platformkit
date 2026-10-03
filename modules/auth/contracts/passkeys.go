package contracts

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// The two failures the passkey half can answer that the four in factors.go
// cannot say. They are beside those four rather than inside them because they
// belong to a ceremony rather than to a code, and the caller that can act on one
// is not the caller that can act on the other.
var (
	// ErrPasskeyExists is FinishPasskeyRegistration's refusal to enrol one
	// authenticator as two people's factor. It is correctable, but not in this
	// account: the passkey has to be removed from whichever account holds it
	// first, and a refusal that said only "invalid attestation" would send the
	// person to a help desk that cannot see the other account either.
	ErrPasskeyExists = errors.New("auth: that passkey is already enrolled for another account")

	// ErrPasskeySignInOff is the usernameless door's answer when the tenant this
	// request resolved to has not enabled it. It is not ErrCredentials: nothing
	// was wrong with what the person offered, and a person told their passkey is
	// wrong reaches for a password they may not have — while the sentence this
	// one turns into names the thing that is actually true, that this
	// organisation signs in with a passkey only as a second factor.
	ErrPasskeySignInOff = errors.New("auth: this tenant does not sign in with a passkey alone")

	// ErrPasskeyExpired is the enrolment leg's answer to a ceremony that is
	// gone: spent, expired, or never begun here. It is correctable in one step —
	// begin again — and it is not ErrCredentials because nothing was answered
	// wrongly; the prompt simply took longer than two minutes.
	ErrPasskeyExpired = errors.New("auth: that passkey prompt has expired")
)

// PasskeyChallengeWindow is how long a begun ceremony stays answerable. It is a
// constant and not a knob for the reason FirstFactorProofWindow is one: a
// deployment that lengthened it weakened itself, and the only way to get another
// window is to begin again, which costs one tap.
//
// Two minutes is the shortest span that covers a person who has to unlock a
// phone, be prompted by the platform and accept. The same value is what the
// browser is told the ceremony lives — the library's own default is five minutes
// (300 s), and a platform that is still holding a prompt the server has already
// swept is the one refusal no test catches and every slow person hits. A case
// reads the timeout out of the response and pins it to this constant.
const PasskeyChallengeWindow = 2 * time.Minute

// The three doors a ceremony is begun at. Which one a challenge belongs to is
// written at begin and read at finish: the door is the row's fact and never the
// caller's, which is what stops a ceremony begun where a password was expected
// from being answered where none was.
const (
	// PasskeyCeremonyRegister enrols a passkey for the signed-in caller.
	PasskeyCeremonyRegister = "register"
	// PasskeyCeremonySecondFactor is the half after a password: answering it
	// spends the first-factor proof before it opens anything.
	PasskeyCeremonySecondFactor = "second-factor"
	// PasskeyCeremonySignIn is the whole sign-in, usernameless, for a tenant
	// that enabled it.
	PasskeyCeremonySignIn = "sign-in"
)

// PasskeyChallenge is a begun ceremony: what to hand the browser, and the id the
// browser must hand back. Options is the credential-creation or
// credential-request JSON of the standard, verbatim — it carries the challenge
// the browser is being asked to sign, which is public by design, and no
// credential material. The row behind Ceremony holds what the answer must match.
type PasskeyChallenge struct {
	Ceremony uuid.UUID       `json:"ceremony" format:"uuid"`
	Options  json.RawMessage `json:"options"`
}

// Passkeys is the ceremony half of the second factor: the exchange where the
// server mints a nonce and receives a signature over it, rather than the one
// where it mints a secret and receives a code derived from it.
//
// It is its own interface beside Factors for the reason the mailer and the
// limiter are their own ports: the only enrolment pair Factors offers,
// BeginTOTP/FinishTOTP, is shaped by a secret the server hands out, and nothing
// in that shape is a parameter of a ceremony. What the two halves share is not a
// method but a rule, and the rule stays in one place: ListFactors and
// WithdrawFactor span both kinds, and the refusal to take the last one away
// counts both.
type Passkeys interface {
	// BeginPasskeyRegistration asks this person's device for a new passkey.
	// Beyond the nonce row it writes nothing: the credential exists only when
	// FinishPasskeyRegistration has seen a signature nobody else could have
	// made, so a tab closed mid-enrolment leaves no factor, publishes no event
	// and changes nothing about how this person signs in. No factor key is
	// involved — a public key needs no envelope — so this method never answers
	// ErrNoFactorKey.
	BeginPasskeyRegistration(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (*PasskeyChallenge, error)

	// FinishPasskeyRegistration enrols the credential the ceremony answered,
	// bound to the person the ceremony was begun for. name is that person's own
	// label for the device, up to 40 characters; empty is allowed and the list
	// then shows the kind and the date.
	//
	// The ceremony must be this caller's, of kind register, unspent and
	// unexpired. Somebody else's ceremony, or one begun at a sign-in door, is
	// refused and enrols nothing; a credential id already enrolled here for
	// another account is ErrPasskeyExists and enrols nothing.
	FinishPasskeyRegistration(ctx context.Context, tx db.Tx[db.Tenant], userID, ceremony uuid.UUID, response json.RawMessage, name string) (*Factor, error)

	// BeginPasskeyAssertion begins the answer for a person the caller named, at
	// the second-factor door. The allow list is deliberately empty: the request
	// costs the same and the browser prompt looks the same whether or not this
	// address holds a passkey, so a begin leg that differed for the two cases
	// would be an account-enumeration oracle handed out for free.
	BeginPasskeyAssertion(ctx context.Context, tx db.Tx[db.Tenant], email string) (*PasskeyChallenge, error)

	// BeginPasskeySignIn begins the usernameless ceremony: no address is
	// offered, none is learned, and nothing is written beyond the nonce. It
	// refuses ErrPasskeySignInOff for a tenant that has not enabled the door,
	// which is the only thing it refuses on its own.
	BeginPasskeySignIn(ctx context.Context, tx db.Tx[db.Tenant]) (*PasskeyChallenge, error)

	// SetPasskeySignIn is the tenant's own answer to whether a passkey may be the
	// whole sign-in. A passkey is a second factor the moment one is enrolled;
	// what this switches is the wider promise — that no password is offered first
	// — and the person who decides it is this tenant's administrator, which is
	// why the route that reaches it is guarded by PermissionPasskeySignIn rather
	// than left to whoever is standing at the door.
	//
	// It writes one row and publishes one event in the caller's own transaction,
	// and refuses without either: setting the value that is already set changes
	// nothing and publishes nothing, so the trail says that the door was turned,
	// and not that somebody pressed a button.
	//
	// It returns the state the tenant is in afterwards, which is the same answer
	// for a call that changed it and for a call that found it already so — the
	// route renders what the row says rather than what the caller asked for.
	SetPasskeySignIn(ctx context.Context, tx db.Tx[db.Tenant], enabled bool) (bool, error)

	// FinishPasskeyAssertion is the answer to either sign-in door. It spends the
	// ceremony nonce before it reads anything about a factor, so a refusal
	// consumes the attempt rather than leaving a captured signature spendable;
	// it then requires the first-factor proof when and only when the row says
	// the ceremony was begun at the second-factor door, and it opens the session
	// exactly as Login would. Which door it was is the row's fact, not the
	// caller's argument.
	//
	// An unknown credential, a credential of another tenant, a person who cannot
	// sign in, a wrong signature and a spent or expired ceremony are one answer
	// at one cost — ErrCredentials — for the reason Login gives for its three.
	FinishPasskeyAssertion(ctx context.Context, tx db.Tx[db.Tenant], ceremony uuid.UUID, response json.RawMessage, from Client) (*Session, *Identity, error)
}
