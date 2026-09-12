package contracts

import (
	"context"
	"fmt"
	"slices"

	"github.com/septagon-oss/platformkit/kit/db"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// PendingRegistrar is the user-owned write needed by approval-required signup.
// It cannot look up an existing identity. user.ErrRegistrationExists leaves the
// caller's transaction usable; every other error must abort the request.
type PendingRegistrar interface {
	RegisterPending(context.Context, db.Tx[db.Tenant], user.PendingRegistration) (*user.User, error)
}

// ApprovalRegistration opts one auth composition into password-based signup
// awaiting operator approval. Roles come from trusted application composition,
// never the visitor. The application must provision these roles for its tenants.
// This policy is mutually exclusive with emailed password-setup registration.
type ApprovalRegistration struct {
	Users PendingRegistrar
	Roles []string
}

// Checked freezes normalized role defaults before the module mounts its routes.
func (r ApprovalRegistration) Checked() (ApprovalRegistration, error) {
	if r.Users == nil || len(r.Roles) == 0 {
		return r, fmt.Errorf("auth: approval registration requires a registrar and initial roles")
	}
	var err error
	r.Roles, err = checkedRegistrationRoles(r.Roles)
	return r, err
}

func checkedRegistrationRoles(roles []string) ([]string, error) {
	roles = slices.Clone(roles)
	for i, role := range roles {
		name, err := ValidRoleName(role)
		if err != nil {
			return nil, err
		}
		roles[i] = name
	}
	slices.Sort(roles)
	return slices.Compact(roles), nil
}
