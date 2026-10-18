package contracts

import (
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// SessionListing is one row of the list a person reads in order to answer one
// question: "was that me?".
//
// It names a session by its ref — hex(sha256(id)) — and never by an id or a
// hash field a client could put straight back into a cookie. The sessions table
// stores a hash for the same reason (see Session): a list somebody can read off
// their own screen, or a backup somebody can read off a disk, must be a list of
// references rather than a set of live credentials. The ref is what
// RevokeSession takes, so the page needs no second identifier and gains none.
type SessionListing struct {
	Ref        string    `json:"ref" example:"3f2a1c…" doc:"hex(sha256(session id)); what revoking takes"`
	UserAgent  string    `json:"userAgent,omitempty"`
	IP         string    `json:"ip,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
	// Current marks the session answering this request, so the one entry a
	// person should not revoke by reflex is the one that says so. It is
	// computed by comparing refs, not read from a column: the table holds no
	// is_current, because a column that is true for whoever happens to be
	// asking is a column that lies the moment two tabs are open.
	Current bool `json:"current"`
}

// SessionRefOf is the ref a person sees and a revoke call carries, validated
// back into the bytea the sessions table is keyed by.
//
// It refuses rather than querying with what it was handed: a ref of the wrong
// length or with a non-hex byte is not one of this module's sessions, and an
// unknown ref is the same 404 as a session that was never there.
func SessionRefOf(ref string) (Digest, error) {
	raw, err := hex.DecodeString(ref)
	if err != nil || len(raw) != len(Hash("")) {
		return nil, errors.New("auth: that is not a session reference")
	}
	return Digest(raw), nil
}

// SessionRefMatches is the one comparison between a session and the caller's
// own cookie: two refs, not two ids, so the same function serves the list and
// the page and neither has to hold a credential to use it.
func SessionRefMatches(id uuid.UUID, ref string) bool {
	return SessionRef(id) == ref
}

// RevokedSession is the payload a revocation reports. It is a struct rather than
// five arguments because the three commands that end a session report the same
// six facts and a reviewer should be able to see that they do.
type RevokedSession struct {
	Ref       string
	UserAgent string
	IP        string
	ExpiresAt time.Time
}

// String is the audit-facing form: a ref, an agent, an address. Never an id.
func (r RevokedSession) String() string {
	return fmt.Sprintf("session %s (%q from %s)", r.Ref, r.UserAgent, r.IP)
}
