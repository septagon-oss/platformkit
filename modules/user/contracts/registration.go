package contracts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
)

// ErrRegistrationExists is the only registration conflict that leaves the
// transaction usable for a neutral public acknowledgment. The existing account
// is unchanged. Other failures must abort the caller's transaction.
var ErrRegistrationExists = fmt.Errorf("%w: this email address is already registered", crud.ErrConflict)

// PendingRegistration is a trusted application command, not a public request
// schema. The composing registrar validates consent and confirmation, chooses
// roles itself and never accepts grants from the visitor. Password is hashed
// in the user owner and is never serialized or published in an event.
type PendingRegistration struct {
	Email, DisplayName string
	Password           string `json:"-"`
	Roles              []string
}

type RegistrationPage struct {
	Items []*User `json:"items"`
	Total int64   `json:"total"`
}

// Registrations is the approval workflow over the same tenant-local
// user rows. It neither enables public signup nor claims mailbox verification.
type Registrations interface {
	// RegisterPending creates a pending account with a password and trusted
	// roles. An existing email returns ErrRegistrationExists without changing
	// that account or aborting the transaction. Validation and hashing precede the insert on
	// every attempt; it never reads an existing account to decide its response.
	RegisterPending(context.Context, db.Tx[db.Tenant], PendingRegistration) (*User, error)
	// PendingRegistrations lists oldest first, then by ID, with bounded offset
	// pagination. Zero limit uses the standard default; invalid bounds fail.
	PendingRegistrations(context.Context, db.Tx[db.Tenant], int, int) (RegistrationPage, error)
	// ApproveRegistration activates a pending account without changing its
	// password or roles. Already-active accounts are unchanged; invited and
	// inactive accounts conflict. Actor must identify the approving principal.
	ApproveRegistration(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID) (*User, error)
}

const (
	EventRegistrationPending  = "user.registration_pending"
	EventRegistrationApproved = "user.registration_approved"
)

type RegistrationPending struct {
	UserID uuid.UUID `json:"userId"`
	At     time.Time `json:"at"`
}

type RegistrationApproved struct {
	UserID uuid.UUID `json:"userId"`
	Actor  uuid.UUID `json:"actor"`
	At     time.Time `json:"at"`
}
