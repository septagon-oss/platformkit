package contracts

import (
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/events"
)

// The three events this module emits. There are no CRUD events, because this
// module mounts no rest.Spec: a session is not a resource somebody edits.
const (
	EventLoggedIn    = "auth.logged_in"
	EventLoggedOut   = "auth.logged_out"
	EventLoginFailed = "auth.login_failed"
	// EventResetRequested is somebody asking for a reset link, and it is the
	// whole of what the public route does.
	//
	// The lookup happens in the subscription rather than in the request,
	// because the request must cost the same whether or not anybody has the
	// address. Doing it inline answered a known address in 2.1 ms and an
	// unknown one in 0.9 ms with non-overlapping distributions, which is an
	// account enumeration oracle with a stopwatch — exactly the thing the
	// route's own description says it is not.
	EventResetRequested = "auth.reset_requested"
	// EventPasswordReset is the end of the forgotten-password flow: somebody
	// who could not sign in proved they read a mailbox and chose a new
	// password. It is distinct from user.password_set, which the user module
	// publishes for every password change however it was authorised, because
	// "this account's password was changed by somebody holding an emailed link"
	// is the line in an audit trail a person looks for after an incident.
	EventPasswordReset = "auth.password_reset"
	// EventRoleSet is a change to what a role grants, which is a change to what
	// everybody holding it may do. It carries both lists for the reason
	// user.roles_set does: the interesting question about a grant is what it
	// added.
	EventRoleSet = "auth.role_set"
	// EventSessionRevoked is a person ending one of their own sessions or all
	// of them. It is distinct from auth.logged_out, which is somebody signing
	// themselves out of the machine in front of them: this is the entry in the
	// trail that answers "when did I kick that one out, and was it before the
	// laptop I forgot about".
	EventSessionRevoked = "auth.session_revoked"
)

// Events is every event this module emits, for the manifest.
var Events = []events.Declared{
	events.Declare[LoggedIn](EventLoggedIn),
	events.Declare[LoggedOut](EventLoggedOut),
	events.Declare[LoginFailed](EventLoginFailed),
	events.Declare[ResetRequested](EventResetRequested),
	events.Declare[PasswordReset](EventPasswordReset),
	events.Declare[RoleSet](EventRoleSet),
	events.Declare[SessionRevoked](EventSessionRevoked),
	events.Declare[FactorEnrolled](EventFactorEnrolled),
	events.Declare[FactorWithdrawn](EventFactorWithdrawn),
	events.Declare[RecoveryCodesIssued](EventRecoveryCodesIssued),
	events.Declare[RecoveryCodeUsed](EventRecoveryCodeUsed),
	events.Declare[RegistrationRequested](EventRegistrationRequested),
	events.Declare[VerificationRequested](EventVerificationRequested),
}

// ResetRequested is the payload of EventResetRequested: this address asked for
// a reset link. Whether anybody has it is not decided here.
//
// It carries the address, which means an outbox row holds one for a week and
// modules/audit copies it into the trail. That is the same trade LoginFailed
// already makes, and it is the reason this event carries nothing else: an
// address that asked for a reset is what an account under attack looks like,
// and it is not a credential. The token is not here and is in no row anywhere —
// see Service.Reissue.
type ResetRequested struct {
	Email string    `json:"email"`
	At    time.Time `json:"at"`
}

// PasswordReset is the payload of EventPasswordReset. It carries no password,
// no token and no hash: what a subscriber may act on is that it happened and to
// whom.
type PasswordReset struct {
	UserID uuid.UUID `json:"userId"`
	At     time.Time `json:"at"`
}

// RoleSet is the payload of EventRoleSet.
type RoleSet struct {
	Role string    `json:"role"`
	Was  []string  `json:"was"`
	Now  []string  `json:"now"`
	At   time.Time `json:"at"`
}

// LoggedIn is the payload of EventLoggedIn.
type LoggedIn struct {
	UserID uuid.UUID `json:"userId"`
	// SessionRef(id), never the id: the id is the cookie credential (review 2026-09-29).
	SessionRef string `json:"sessionRef"`
	// Method is "password" or "oidc", because "somebody signed in with a
	// password after we turned single sign-on on" is a question with an answer.
	Method string    `json:"method"`
	IP     string    `json:"ip,omitempty"`
	At     time.Time `json:"at"`
}

// LoggedOut is the payload of EventLoggedOut.
type LoggedOut struct {
	UserID     uuid.UUID `json:"userId"`
	SessionRef string    `json:"sessionRef"`
	At         time.Time `json:"at"`
}

// SessionRevoked is the payload of EventSessionRevoked: one session this person
// ended, named by its ref. It carries the agent and the address the session was
// opened with — the two facts the list is rendered from — so the trail says what
// went away and not merely that something did. All is set when the revocation
// was "everywhere": the flag is the difference between one machine leaving and a
// person who was frightened of one machine and cleaned house.
//
// It carries no session id and no hash: a ref is this module's public name for a
// session and the id is a credential.
type SessionRevoked struct {
	UserID     uuid.UUID `json:"userId"`
	SessionRef string    `json:"sessionRef"`
	UserAgent  string    `json:"userAgent,omitempty"`
	IP         string    `json:"ip,omitempty"`
	ExpiresAt  time.Time `json:"expiresAt"`
	All        bool      `json:"all,omitempty"`
	At         time.Time `json:"at"`
}

// LoginFailed is the payload of EventLoginFailed: somebody tried and did not
// get in. It carries the address that was tried, because that is the whole
// value of the event — an audit of one account under attack — and it carries no
// password, not even its length.
//
// Locked says the attempt was refused before it was checked, which is how a
// subscriber tells a person mistyping their password from an attack that has
// already tripped the limit.
type LoginFailed struct {
	Email  string    `json:"email"`
	IP     string    `json:"ip,omitempty"`
	Locked bool      `json:"locked"`
	At     time.Time `json:"at"`
}

// The second factor's three events, and the one that closes a set.
//
// None of them carries a secret, a code or a step. An event is copied into the
// audit trail and delivered to anything subscribed, so a payload naming a
// credential would turn the trail into a bag of spare factors — which is the
// reason these four say only that a factor was made, stopped, spent or replaced,
// and by whom. The one number they carry is a count, because "ten codes were
// issued" is a fact about an account and "here is one" is a key.
const (
	// EventFactorEnrolled: this person proved a second thing and the platform
	// wrote it down. Kind is which thing, so the trail distinguishes a phone
	// from a security key without this module naming either.
	EventFactorEnrolled = "auth.factor_enrolled"
	// EventFactorWithdrawn: one factor stopped working, and how many are left,
	// which is the difference between "swapped phones" and "turned the account
	// back into a password". The latter cannot happen here, and the field is how
	// a reader of the trail can see that it did not.
	EventFactorWithdrawn = "auth.factor_withdrawn"
	// EventRecoveryCodesIssued: a set was handed out. It says how many, never
	// which.
	EventRecoveryCodesIssued = "auth.recovery_codes_issued"
	// EventRecoveryCodeUsed: one was spent to sign in. It is the entry an owner
	// reads afterwards and the only hint that somebody is in with the codes
	// rather than with the device — which is why the use is an event even though
	// the sign-in itself is a session row and a logged_in event.
	EventRecoveryCodeUsed = "auth.recovery_code_used"
)

// FactorEnrolled is the payload of EventFactorEnrolled.
type FactorEnrolled struct {
	UserID   uuid.UUID `json:"userId"`
	FactorID uuid.UUID `json:"factorId"`
	Kind     string    `json:"kind" enums:"totp" example:"totp"`
	At       time.Time `json:"at"`
}

// FactorWithdrawn is the payload of EventFactorWithdrawn.
type FactorWithdrawn struct {
	UserID    uuid.UUID `json:"userId"`
	FactorID  uuid.UUID `json:"factorId"`
	Kind      string    `json:"kind" enums:"totp" example:"totp"`
	Remaining int       `json:"remaining" example:"1"`
	At        time.Time `json:"at"`
}

// RecoveryCodesIssued is the payload of EventRecoveryCodesIssued.
type RecoveryCodesIssued struct {
	UserID uuid.UUID `json:"userId"`
	Count  int       `json:"count" example:"10"`
	At     time.Time `json:"at"`
}

// RecoveryCodeUsed is the payload of EventRecoveryCodeUsed. It carries no
// identifying byte of the code that was spent: which of the ten is a fact only
// the row knows, and the row is deleted by the sweep.
type RecoveryCodeUsed struct {
	UserID uuid.UUID `json:"userId"`
	At     time.Time `json:"at"`
}
