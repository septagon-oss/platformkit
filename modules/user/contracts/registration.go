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

// PasswordRegistration is a trusted application command, not a public request
// schema. The composing registrar validates consent and confirmation, chooses
// roles itself and never accepts grants from the visitor. Password is hashed
// in the user owner and is never serialized or published in an event.
type PasswordRegistration struct {
	Email, DisplayName string
	Password           string `json:"-"`
	Roles              []string
}

// PendingRegistration retains the approval registrar's existing input name.
type PendingRegistration = PasswordRegistration

type RegistrationPage struct {
	Items []*User `json:"items"`
	Total int64   `json:"total"`
}

// Registrations owns distinct approval and mailbox-verification lifecycles over
// tenant-local user rows. The application opts in and auth owns mailbox tokens.
type Registrations interface {
	// RegisterPending creates a pending account with a password and trusted
	// roles. An existing email returns ErrRegistrationExists without changing
	// that account or aborting the transaction. Validation and hashing precede the insert on
	// every attempt; it never reads an existing account to decide its response.
	RegisterPending(context.Context, db.Tx[db.Tenant], PendingRegistration) (*User, error)
	// RegisterUnverified has the same insertion and conflict guarantees, but
	// only VerifyEmail can activate the resulting password-bearing account.
	RegisterUnverified(context.Context, db.Tx[db.Tenant], PasswordRegistration) (*User, error)
	// VerifyEmail locks the user, compares the normalized expected email with
	// the current canonical email, and activates only an unverified account
	// with a password. Wrong email or lifecycle state, including replay after
	// activation, conflicts. Credentials and roles remain unchanged.
	VerifyEmail(context.Context, db.Tx[db.Tenant], uuid.UUID, string) (*User, error)
	// PendingRegistrations lists oldest first, then by ID, with bounded offset
	// pagination. Zero limit uses the standard default; invalid bounds fail.
	PendingRegistrations(context.Context, db.Tx[db.Tenant], int, int) (RegistrationPage, error)
	// ApproveRegistration activates a pending account without changing its
	// password or roles. Already-active accounts are unchanged; invited and
	// inactive and unverified accounts conflict. Actor identifies the approver.
	ApproveRegistration(context.Context, db.Tx[db.Tenant], uuid.UUID, uuid.UUID) (*User, error)
}

const (
	EventRegistrationPending    = "user.registration_pending"
	EventRegistrationApproved   = "user.registration_approved"
	EventRegistrationUnverified = "user.registration_unverified"
	EventEmailVerified          = "user.email_verified"
)

type RegistrationUnverified struct {
	UserID uuid.UUID `json:"userId"`
	Email  string    `json:"email"`
	At     time.Time `json:"at"`
}

type EmailVerified struct {
	UserID uuid.UUID `json:"userId"`
	Email  string    `json:"email"`
	At     time.Time `json:"at"`
}

type RegistrationPending struct {
	UserID uuid.UUID `json:"userId"`
	At     time.Time `json:"at"`
}

type RegistrationApproved struct {
	UserID uuid.UUID `json:"userId"`
	Actor  uuid.UUID `json:"actor"`
	At     time.Time `json:"at"`
}
