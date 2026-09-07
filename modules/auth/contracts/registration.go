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
}
