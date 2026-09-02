package internal

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// RegisterRoutes mounts the three lifecycle commands under path, the same path
// the Spec mounts the five CRUD routes on.
//
// They are routes rather than fields of a PATCH because each is a rule about
// the state the user is in and each publishes an event. Setting a password
// through a generic update would put a password in a request body beside a
// display name and in an event beside it too; granting a role through one would
// make "who was made an administrator, and when" a question with no answer.
func RegisterRoutes(api *httpx.API, svc contracts.Service, at string) {
	command(api, at, "set-password", contracts.EventPasswordSet,
		"Set a user's password",
		"Stores an argon2id hash and makes an invited user active. It is not idempotent: setting a password to what it already was is still a password change.",
		func(ctx context.Context, tx db.Tx[db.Tenant], in *passwordInput) (*contracts.User, error) {
			if err := svc.SetPassword(ctx, tx, in.ID, in.Body.Password); err != nil {
				return nil, err
			}
			return svc.Get(ctx, tx, in.ID)
		})

	command(api, at, "roles", contracts.EventRolesSet,
		"Set a user's roles",
		"Replaces the roles this person holds. The same set again, in any order, changes nothing and publishes nothing.",
		func(ctx context.Context, tx db.Tx[db.Tenant], in *rolesInput) (*contracts.User, error) {
			return svc.SetRoles(ctx, tx, in.ID, in.Body.Roles)
		})

	command(api, at, "deactivate", contracts.EventDeactivated,
		"Deactivate a user",
		"Stops the person signing in. Their sessions stop working with them, because a session is only honoured for an active user. Deactivating them again changes nothing.",
		func(ctx context.Context, tx db.Tx[db.Tenant], in *idInput) (*contracts.User, error) {
			return svc.Deactivate(ctx, tx, in.ID)
		})
}

// command registers one POST {path}/{id}/{verb}: the same permission, the
// request's transaction, and kit/crud's mapping, so the lifecycle routes and
// the CRUD routes on one resource cannot disagree about what a status code
// means.
//
// It is a copy of modules/task/internal's, and it is the second one, which is
// the signal that it belongs in kit/crud. The E2 review is landing it there as
// crud.Command; this goes when that does.
func command[I any](api *httpx.API, at, verb, event, summary, description string,
	run func(context.Context, db.Tx[db.Tenant], *I) (*contracts.User, error),
) {
	httpx.Register(api, huma.Operation{
		OperationID: "user-user-" + verb,
		Method:      http.MethodPost,
		Path:        at + "/{id}/" + verb,
		Summary:     summary,
		Description: description,
		Tags:        []string{"user"},
		Errors: []int{http.StatusNotFound, http.StatusConflict,
			http.StatusUnprocessableEntity, http.StatusServiceUnavailable},
		Extensions: map[string]any{httpx.EventsExtension: []string{event}},
	}, httpx.Permission(contracts.PermissionUserManage), func(ctx context.Context, in *I) (*userOutput, error) {
		tx, ok := httpx.TxFrom(ctx)
		if !ok {
			return nil, problem.New(http.StatusServiceUnavailable, "the database is not reachable right now")
		}
		u, err := run(ctx, tx, in)
		if err != nil {
			return nil, crud.Fault(err)
		}
		return &userOutput{Body: u}, nil
	})
}

type idInput struct {
	ID uuid.UUID `path:"id" format:"uuid" doc:"The user's id"`
}

type passwordInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"The user's id"`
	Body struct {
		Password string `json:"password" minLength:"12" maxLength:"256" doc:"The new password; at least twelve characters"`
	}
}

type rolesInput struct {
	ID   uuid.UUID `path:"id" format:"uuid" doc:"The user's id"`
	Body struct {
		Roles []string `json:"roles" doc:"The roles this person holds from now on" example:"admin"`
	}
}

// userOutput is the user as they stand after the command.
type userOutput struct {
	Body *contracts.User
}
