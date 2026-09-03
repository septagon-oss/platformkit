package contracts

import (
	"time"

	"github.com/google/uuid"
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
)

// Events is every event this module emits, for the manifest.
var Events = []string{
	EventLoggedIn, EventLoggedOut, EventLoginFailed,
	EventResetRequested, EventPasswordReset, EventRoleSet,
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
	UserID    uuid.UUID `json:"userId"`
	SessionID uuid.UUID `json:"sessionId"`
	// Method is "password" or "oidc", because "somebody signed in with a
	// password after we turned single sign-on on" is a question with an answer.
	Method string    `json:"method"`
	IP     string    `json:"ip,omitempty"`
	At     time.Time `json:"at"`
}

// LoggedOut is the payload of EventLoggedOut.
type LoggedOut struct {
	UserID    uuid.UUID `json:"userId"`
	SessionID uuid.UUID `json:"sessionId"`
	At        time.Time `json:"at"`
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
