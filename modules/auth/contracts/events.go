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
)

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
