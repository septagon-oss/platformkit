package contracts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
)

// The three failures a caller of this capability can act on.
var (
	// ErrFactorRequired is Login's and Open's answer when the first proof arrived
	// and the second did not: this person holds a factor and this request did not
	// carry its answer. It is not ErrCredentials, and which proof came first is
	// not fixed — a password that checked out and a provider that confirmed the
	// address are the same half — and a person told otherwise reaches for the reset
	// link, which is how a second factor becomes a password reset queue. It is an
	// outcome rather than a fault: both doors return it with no session and no
	// identity, because the caller holds one right thing, which is exactly what
	// answering the factor is for. Both doors mark that right thing as proved,
	// for FirstFactorProofWindow, and that mark is the only thing that makes the
	// code below answerable: an answer with no refused door behind it is refused
	// itself, so a code — which is a thing a person may have written down
	// somewhere — is never on its own the account.
	ErrFactorRequired = errors.New("auth: this account answers with a second factor")

	// ErrLastFactor is WithdrawFactor's refusal to take the last one away.
	//
	// Rule 8: refuse the write that takes the last one away, never the write
	// that finds none. It is correctable in one step — enrol another factor, then
	// withdraw this one — and the refusal *is* the guarantee, because a person who
	// withdrew their only factor has, without meaning to, turned the account back
	// into a password.
	ErrLastFactor = errors.New("auth: that is the last factor on this account")

	// ErrNoFactor is RotateRecoveryCodes' refusal to hand out recovery codes to a
	// person with no factor. Codes are the substitute for a second factor; on
	// their own they would be a second password, and a set issued to an account
	// with only a password would read as hardening while it was the opposite.
	ErrNoFactor = errors.New("auth: this account has no factor to recover")

	// ErrNoFactorKey says this deployment set no auth.factor_key, so a secret
	// could be written only as plaintext. The enrolment routes answer 503 and
	// write nothing; verification of an already-enrolled factor is unaffected.
	//
	// It covers the enrolment of a shared secret and nothing else. A passkey
	// writes no secret — its credential material is a public key — so a
	// deployment with no factor key enrols passkeys and answers both passkey
	// sign-in doors normally, and a person holding only passkeys is still held
	// at the door by ErrFactorRequired.
	ErrNoFactorKey = errors.New("auth: no factor key is configured")
)

// Factor parameters, in the module that enforces them rather than in
// configuration: a deployment that lengthens the window or thins the codes has
// weakened itself, and the rule of this file is that weakness is not a knob.
const (
	// TOTPDigits is RFC 6238's own code length. Six digits is a 1-in-10^6 guess
	// per step, which is why the challenge needs no separate attempt counter —
	// see the header of 000031_auth_factors.up.sql for what that buys.
	TOTPDigits = 6
	// TOTPPeriod is RFC 6238's step: 30 seconds.
	TOTPPeriod = 30 * time.Second
	// TOTPSkew is how far either side of "now" a code is accepted: one step, so
	// a phone whose clock is half a minute off is a person who signs in rather
	// than a support ticket. It is the whole of the clock tolerance, and two
	// would be a minute of replay window bought with a wrong clock.
	TOTPSkew = 1
	// RecoveryCodes is how many codes a person is handed, once, at enrolment.
	// Ten is the number that survives a week of travelling without letting a
	// pocket full of spares become the ordinary way in.
	RecoveryCodes = 10

	// FirstFactorProofWindow is how long the second half stays answerable after
	// a door refused to finish the sign-in without it. It is a window and not a
	// standing door: the only way to get another is to offer the first factor
	// again, and the challenge spends the one it answers, so a person who walks
	// away mid-sign-in leaves nothing behind that outlives five minutes.
	//
	// It lives here rather than in configuration for the reason the window and
	// the skew above do — a deployment that lengthened it had weakened the thing
	// it turned on — and five minutes is the shortest span that covers a person
	// on somebody else's keyboard unlocking a phone, opening an authenticator
	// and typing six digits, which is the errand the wait is for.
	FirstFactorProofWindow = 5 * time.Minute
)

// Factor is one second factor a person holds. It names no secret and carries no
// material a client could use: the row is a fact about the account ("this person
// proves something beside the password"), and the proof itself never leaves the
// database sealed.
type Factor struct {
	ID   uuid.UUID `json:"id"`
	Kind string    `json:"kind" enums:"totp,passkey" example:"passkey"`
	// Name is what this person called it. Only a passkey has one to give: an
	// authenticator app holds one secret per account and needs no label to tell
	// two of them apart, so the field is empty for a TOTP row rather than
	// invented for it.
	Name       string    `json:"name,omitempty" maxLength:"40" doc:"What this person called this passkey; empty for a TOTP, which has no name to give"`
	EnrolledAt time.Time `json:"enrolledAt"`
}

// TOTPEnrolment is what beginning an enrolment hands back, and it is shown
// once. Secret is the RFC 4648 base32 seed and URI is the same seed as an
// otpauth:// URI — an authenticator app scans the URI and a person typing it
// has the secret either way, which is why this response is a 200 for its caller
// and a photograph somebody should not take.
//
// Nothing is written by beginning. The factor does not exist until a code
// proves the secret reached the phone, so an abandoned enrolment leaves no row,
// publishes no event and changes nothing about how this person signs in.
type TOTPEnrolment struct {
	Secret      string    `json:"secret" example:"JBSWY3DPEHPK3PXPJBSWY3DPEHPK3PXP"`
	URI         string    `json:"uri,omitempty" example:"otpauth://totp/Example:ada@example.com?secret=…&issuer=Example"`
	Account     string    `json:"account" example:"ada@example.com"`
	GeneratedAt time.Time `json:"generatedAt"`
}

// Factors is the second factor of a sign-in: what a person proves beside the
// password, and the codes they are given for the day the thing they carry is
// not with them.
//
// It is its own interface rather than more methods on Service for the reason
// the mailer and the limiter are their own ports: a composition that wants
// passwords only composes none of this, and every route below is mounted only
// when it is. Nothing here is reachable unless the deployment wires it.
type Factors interface {
	// BeginTOTP mints a secret for this person and reports it. It writes
	// nothing: the enrolment is real only when FinishTOTP has seen a code that
	// the secret produces. Without a factor key it answers ErrNoFactorKey and
	// writes nothing, because a plaintext secret at rest is the thing this
	// capability exists not to do.
	BeginTOTP(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (*TOTPEnrolment, error)

	// FinishTOTP enrols the secret begun above, having checked a code against
	// it, and hands back the recovery codes — the only time any of this is ever
	// written down. A wrong code is ErrCredentials (the one answer the login
	// path gives too) and enrols nothing, which is what stops a guessed code
	// from becoming a factor.
	//
	// The secret is handed back to this request rather than remembered by the
	// server: an enrolment parked in a pending row would be state the pool could
	// not see, and a person who closes the tab begins again.
	//
	// The issuer named in the otpauth URI is the host the request arrived at,
	// composed by the route rather than known here: an authenticator shows that
	// string to a person deciding whether to trust a prompt, and a module that
	// hardcoded it would be naming a customer (rule 4).
	FinishTOTP(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID, secret, code string) (*Factor, []string, error)

	// ListFactors is what this person holds. An empty list means the password
	// still signs them in alone, which is a state the platform stays in by
	// default: a factor is chosen, never imposed.
	ListFactors(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) ([]*Factor, error)

	// WithdrawFactor ends one factor. The last one is ErrLastFactor.
	WithdrawFactor(ctx context.Context, tx db.Tx[db.Tenant], userID, factor uuid.UUID) error

	// RotateRecoveryCodes spends every unused code this person has and issues a
	// fresh set: the command for "I think my codes leaked". It refuses a person
	// with no factor, because codes are the substitute for a factor and alone
	// they would be a second password.
	RotateRecoveryCodes(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) ([]string, error)

	// RequireFirstFactorProof spends the proof that this address's first factor
	// checked out a moment ago, so that the code below is answering a sign-in
	// rather than being one.
	//
	// It exists because a code is not a first factor. Login and Open refuse with
	// ErrFactorRequired and mint the window (000033_first_factor_proofs.up.sql);
	// the challenge leg calls this before it looks at a code, and the refusal
	// that comes back here spends no code, opens no session and publishes no
	// sign-in — which is the difference between a recovery code that stands in
	// for a second factor and one that has quietly become a password. A code is
	// text a person keeps: the module that lets one be spent by whoever presents
	// it has handed out ten sign-ins per enrolment and asked nobody whether they
	// were also holding the address.
	//
	// Absent, spent or expired is ErrCredentials — the answer a wrong code gets,
	// at its cost, because "sign in with your password first" and "that code is
	// wrong" would tell a stranger which half they were missing.
	RequireFirstFactorProof(ctx context.Context, tx db.Tx[db.Tenant], email string, from Client) error

	// VerifySecondFactor spends a second factor and opens the session its
	// account earned: given the address, the code (a TOTP or a recovery code)
	// and where the answer came from, it checks the factor is still there, that
	// the code has not been spent, and that the step has not been used, then
	// opens a session exactly as Login would.
	//
	// It is the second half and not a sign-in. The half that came before it is
	// the caller's own, and this method cannot see it, which is why nothing
	// mounts it on its own: the challenge leg spends RequireFirstFactorProof
	// first, in the same transaction, so a code is only ever spent by a caller
	// who was refused for the other half a moment earlier. A composition that
	// called this one alone would be handing out sessions to whoever held a code.
	//
	// A wrong code, a replayed step, a spent recovery code, an address with no
	// factor and an address nobody has are one answer at one cost — ErrCredentials
	// — for the reason Login gives for its three.
	VerifySecondFactor(ctx context.Context, tx db.Tx[db.Tenant], email, code string, from Client) (*Session, *Identity, error)
}
