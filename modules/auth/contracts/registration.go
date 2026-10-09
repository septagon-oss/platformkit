package contracts

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// RegistrationUsers is the optional capability an application supplies to
// enable self-registration. New accounts receive only the member role and
// remain invited until their emailed password token is redeemed. Existing
// accounts, including their roles and inactive status, are preserved.
type RegistrationUsers interface {
	ByEmail(context.Context, db.Tx[db.Tenant], string) (*user.User, error)
	Invite(context.Context, db.Tx[db.Tenant], string, string) (*user.User, error)
	SetRoles(context.Context, db.Tx[db.Tenant], uuid.UUID, []string) (*user.User, error)
}

const EventRegistrationRequested = "auth.registration_requested"

// RegistrationRequested records a request, without deciding whether the
// address already exists. Creation runs in the worker in the event's tenant;
// the public response contains neither an account identity nor a credential.
type RegistrationRequested struct {
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	At          time.Time `json:"at"`
	// Served is the address the request that asked was answered at, port and all,
	// and empty when it named no port: the address every set-password link this
	// request causes has to be built on, whether the account already existed and is
	// mailed from here, or is created here and mailed from user.invited's own
	// subscriber. See ResetRequested.Served.
	Served string `json:"served,omitempty"`
}

// RegistrationMode is one of the doors a stranger may use to become a person
// here: which public signup lifecycle this installation mounted.
//
// It is a contract because which way in a product offers is the product's, and
// `Deps` once carried three mutually exclusive fields for that one decision. The
// three named constructors in modules/auth (Registration, EmailRegistration,
// ApprovalRegistration) each provide exactly one mode, so composing two of them
// is the ambiguity `Choose` answers, and a composition that names none has no
// public signup door at all — which is what leaving the field empty means today.
//
// Each mode carries the policy its own lifecycle needs (the people it writes
// through and the roles the account arrives with), because a half-filled one is
// a form that posts to a route that cannot answer it: the named constructor
// fills every field before it puts the value, and Checked runs in auth's build,
// so an incomplete policy is a refusal at boot rather than at the first visitor.
type RegistrationMode interface {
	// Mode is the lifecycle's own name for itself, as a reader of a composition
	// file sees it: "password", "email" or "approval".
	Mode() string
}

// PasswordRegistration is member signup with emailed password setup: the
// account waits for the mailbox link before it can sign in.
type PasswordRegistration struct {
	Users RegistrationUsers
	Roles []string
}

func (PasswordRegistration) Mode() string { return "password" }

// EmailRegistrationMode is the password door that also requires independent
// mailbox confirmation. It needs mail delivery.
type EmailRegistrationMode struct{ Policy *EmailRegistration }

func (EmailRegistrationMode) Mode() string { return "email" }

// ApprovalRegistrationMode accepts a password, a confirmation and a terms
// consent and keeps the account pending for review. It needs no mail delivery.
type ApprovalRegistrationMode struct{ Policy *ApprovalRegistration }

func (ApprovalRegistrationMode) Mode() string { return "approval" }
