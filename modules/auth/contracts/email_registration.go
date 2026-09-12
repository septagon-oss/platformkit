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
	VerifyEmailPath            = "/auth/verify-email"
	EventVerificationRequested = "auth.verification_requested"
)

// VerificationRequested queues a lookup without putting an account identity or
// credential in the public response. The worker operates in the event's tenant.
type VerificationRequested struct {
	Email string    `json:"email"`
	At    time.Time `json:"at"`
}
