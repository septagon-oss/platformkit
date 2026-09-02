package internal

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// RegisterRoutes mounts the three lifecycle commands on the same resource the
// Spec mounts the five CRUD routes on.
//
// They are routes rather than fields of a PATCH because each is a rule about
// the state the user is in and each publishes an event. Setting a password
// through a generic update would put a password in a request body beside a
// display name, and in the update event beside it too; granting a role through
// one would make "who was made an administrator, and when" a question with no
// answer. spec.Immutable is the other half of that argument, and rest.Command
// is the part all three share.
func RegisterRoutes(api *httpx.API, spec rest.Spec[*contracts.User], svc contracts.Service) {
	rest.Command(api, spec, "set-password",
		"Set a user's password",
		"Stores an argon2id hash and makes an invited user active. It is not idempotent: setting a password to what it already was is still a password change, and somebody who did it deliberately has to see it in their own audit trail.",
		[]string{contracts.EventPasswordSet},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in passwordBody) (*contracts.User, error) {
			if err := svc.SetPassword(ctx, tx, id, in.Password); err != nil {
				return nil, err
			}
			return svc.Get(ctx, tx, id)
		})

	rest.Command(api, spec, "roles",
		"Set a user's roles",
		"Replaces the roles this person holds. The same set again, in any order, changes nothing and publishes nothing.",
		[]string{contracts.EventRolesSet},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in rolesBody) (*contracts.User, error) {
			return svc.SetRoles(ctx, tx, id, in.Roles)
		})

	rest.Command(api, spec, "deactivate",
		"Deactivate a user",
		"Stops the person signing in. Their sessions stop working with them, because a session is only honoured for an active user, so there is no list of sessions to walk. Deactivating them again changes nothing.",
		[]string{contracts.EventDeactivated},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.User, error) {
			return svc.Deactivate(ctx, tx, id)
		})
}

// passwordBody and rolesBody are the arguments of the two commands that take
// one. deactivate takes none, so its body is the empty struct and a caller
// sends no body at all.
type passwordBody struct {
	Password string `json:"password" minLength:"12" maxLength:"256" doc:"The new password; at least twelve characters"`
}

type rolesBody struct {
	Roles []string `json:"roles" doc:"The roles this person holds from now on" example:"admin"`
}
