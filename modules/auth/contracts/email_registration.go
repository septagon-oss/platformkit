package contracts

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// EmailRegistrar owns password hashing and activation of the exact unverified
// tenant/user/email state. Auth consumes the verification proof in the same
// transaction; the caller must roll back both operations on any failure.
type EmailRegistrar interface {
	RegisterUnverified(context.Context, db.Tx[db.Tenant], user.PasswordRegistration) (*user.User, error)
	VerifyEmail(context.Context, db.Tx[db.Tenant], uuid.UUID, string) (*user.User, error)
}

// EmailRegistration enables password-first signup awaiting mailbox confirmation.
// Roles are trusted application defaults. Invitation and approval registration
// remain separate, mutually exclusive composition choices.
type EmailRegistration struct {
	Users EmailRegistrar
	Roles []string
}

func (r EmailRegistration) Checked() (EmailRegistration, error) {
	if r.Users == nil || len(r.Roles) == 0 {
		return r, fmt.Errorf("auth: email registration requires a registrar and initial roles")
	}
	var err error
	r.Roles, err = checkedRegistrationRoles(r.Roles)
	return r, err
}

const (
	VerificationLifetime       = 24 * time.Hour
	VerificationResendInterval = time.Minute
	EventVerificationRequested = "auth.verification_requested"

	// VerifyEmailPath is the address a sign-up mail carries and the page that
	// answers it: the confirmation screen, on the tenant's own public face.
	//
	// It is a contract and not an internal fact because two packages have to
	// agree on it: the delivery that writes the link and the page that mounts
	// under the address it mounted itself at, whose guard reads this name and
	// panics at boot if the two ever part ways. internal.VerifyEmailPath is this
	// constant, so the mail and the page cannot be moved apart by an edit that
	// reaches one of them.
	//
	// It is on the public face, and not under the workspace prefix like
	// ResetPath is, because of who opens it: a person who has just chosen a
	// password has no session, and the workspace turns a caller with no session
	// towards the sign-in form. A confirmation link that answers with a request
	// to sign in is a door that closes on the person it was mailed to.
	VerifyEmailPath = "/auth/verify-email"
)

// VerificationRequested queues a lookup without putting an account identity or
// credential in the public response. The worker operates in the event's tenant.
type VerificationRequested struct {
	Email string    `json:"email"`
	At    time.Time `json:"at"`
	// Served is the address the request that asked was answered at, port and all,
	// and empty when it named no port. The confirmation link is rendered in the
	// worker, and the person has to come back to the address they signed up at.
	// See ResetRequested.Served and httpx.ServedAuthority.
	Served string `json:"served,omitempty"`
}
