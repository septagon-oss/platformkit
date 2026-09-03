package internal

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
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
	// The invitation, which is a route of its own and not the create route with
	// a flag.
	//
	// POST to the collection writes a row and publishes user.user.created, and
	// nothing subscribes to that: an administrator who used it made somebody
	// who exists, cannot sign in, and was never told. Inviting is a different
	// intention — somebody should be able to get in — and user.invited is the
	// event the auth module listens for in order to mail them a link. It is a
	// collection of its own rather than a command on a user, because there is
	// no user yet to command.
	httpx.Register(api, huma.Operation{
		OperationID:   "user-invitation-create",
		Method:        http.MethodPost,
		Path:          invitations,
		Summary:       "Invite somebody into this tenant",
		Description:   "Creates a person who cannot sign in yet and publishes user.invited, which is what mails them a link to choose a password. Inviting an address that is already here is a conflict. Roles are granted in the same transaction, so an invitation that names them is one event about who was made what.",
		Tags:          []string{"user"},
		DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusConflict, http.StatusUnprocessableEntity,
			http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{
			contracts.EventInvited, contracts.EventRolesSet,
		}},
	}, httpx.Permission(contracts.PermissionUserManage),
		func(ctx context.Context, in *inviteInput) (*rest.Item[*contracts.User], error) {
			tx, ok := httpx.TxFrom(ctx)
			if !ok {
				return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
			}
			u, err := svc.Invite(ctx, tx, in.Body.Email, in.Body.DisplayName)
			if err != nil {
				return nil, rest.Fault(err)
			}
			if len(in.Body.Roles) > 0 {
				// In the same transaction as the invitation, so that a person
				// who is invited as an administrator was never, for a moment, a
				// person with no roles who had already been mailed a link.
				if u, err = svc.SetRoles(ctx, tx, u.ID, in.Body.Roles); err != nil {
					return nil, rest.Fault(err)
				}
			}
			return &rest.Item[*contracts.User]{Body: u}, nil
		})

	rest.Command(api, spec, "set-password",
		"Set a user's password",
		"Stores an argon2id hash and makes an invited user active. It is not idempotent: setting a password to what it already was is still a password change, and somebody who did it deliberately has to see it in their own audit trail.",
		[]string{contracts.EventPasswordSet},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in passwordBody) (*contracts.User, error) {
			if err := svc.SetPassword(ctx, tx, id, in.Password); err != nil {
				return nil, err
			}
			return svc.Get(ctx, tx, id)
		}, rest.CommandOptions{})

	rest.Command(api, spec, "roles",
		"Set a user's roles",
		"Replaces the roles this person holds. The same set again, in any order, changes nothing and publishes nothing.",
		[]string{contracts.EventRolesSet},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, in rolesBody) (*contracts.User, error) {
			return svc.SetRoles(ctx, tx, id, in.Roles)
		}, rest.CommandOptions{})

	rest.Command(api, spec, "deactivate",
		"Deactivate a user",
		"Stops the person signing in. Their sessions stop working with them, because a session is only honoured for an active user, so there is no list of sessions to walk. Deactivating them again changes nothing.",
		[]string{contracts.EventDeactivated},
		func(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, _ struct{}) (*contracts.User, error) {
			return svc.Deactivate(ctx, tx, id)
		}, rest.CommandOptions{})
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

// invitations is where somebody is invited. It is beside the users collection
// rather than under it because an invitation is not a sub-resource of the user
// it creates.
const invitations = "/api/v1/user/invitations"

// Invitation is what a caller sends to invite somebody. It is a named type and
// not the anonymous struct it was, because huma names a schema after the Go
// type behind it and an anonymous one came out of the generator as
// "InviteInputBody1" — a name with this package's plumbing and a deduplication
// counter in it, published in the OpenAPI document and compiled into every
// generated client.
type Invitation struct {
	Email       string   `json:"email" format:"email" maxLength:"320" doc:"The address to invite" example:"ada@acme.example.com"`
	DisplayName string   `json:"displayName,omitempty" maxLength:"200" doc:"Name to show" example:"Ada Lovelace"`
	Roles       []string `json:"roles,omitempty" doc:"Roles to grant in the same transaction" example:"admin"`
}

type inviteInput struct {
	Body Invitation `required:"true"`
}
