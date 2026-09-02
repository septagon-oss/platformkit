// Package user is the module manifest: the people in a tenant.
//
// It is the exemplar's shape with one entity, one Spec and three lifecycle
// commands, and it takes no dependencies at all: a user belongs to a tenant by
// carrying its id, which row-level security matches on, so there is nothing for
// this module to ask the tenant module for.
package user

import (
	"context"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/internal"
)

// Deps is what this module cannot make for itself, and it is empty. That is
// worth a struct rather than no parameter: the day it needs something, every
// call site gains a named field instead of a new argument.
type Deps struct{}

// spec is the entity's presence in the application: five routes, two
// permissions, three events and the schema a generated screen reads.
var spec = crud.Spec[*contracts.User]{
	Module:     "user",
	Entity:     "user",
	Path:       "/api/v1/user/users",
	Read:       contracts.PermissionUserRead,
	Write:      contracts.PermissionUserManage,
	SoftDelete: true,
	// status is moved by Deactivate, which publishes user.deactivated; a
	// caller who could set it through the generic update would deactivate
	// somebody and tell nobody.
	//
	// roles is not here and cannot be: kit/crud's schema covers a closed set of
	// field types and a slice is not one of them, so `roles` is refused by the
	// PATCH already — as a field that does not exist, which is the right answer
	// arrived at for the wrong reason. Naming it here would panic at mount.
	Immutable: []string{"status"},
}

// Module is the manifest, and the service it is built on: the auth module takes
// this value from main, because signing somebody in means finding them first.
func Module(_ Deps) (contracts.Service, module.Module) {
	svc := internal.NewService()
	mounted := spec
	mounted.AfterCreate = refuseRolesOnCreate
	return svc, module.Module{
		Name: "user",
		Permissions: []module.Permission{
			{Key: contracts.PermissionUserRead},
			{Key: contracts.PermissionUserManage},
		},
		Events: []string{
			contracts.EventCreated, contracts.EventUpdated, contracts.EventDeleted,
			contracts.EventInvited, contracts.EventPasswordSet,
			contracts.EventRolesSet, contracts.EventDeactivated,
		},
		Nav: []module.NavEntry{
			{Label: "Users", Path: "/admin/user/users", Permission: contracts.PermissionUserRead},
		},
		Jobs:          nil,
		Subscriptions: nil,
		Routes: func(api *httpx.API) {
			mounted.Mount(api)
			internal.RegisterRoutes(api, mounted, svc)
		},
	}
}

// refuseRolesOnCreate is the create route's hook: a user is created without
// roles, and roles are granted by the command that publishes an event saying so.
//
// The hook runs inside the request's transaction, after the row and its event,
// so returning an error rolls the whole create back and the caller gets a 422.
// The PATCH route needs no such guard: kit/crud's schema covers a closed set of
// field types, a slice is not one of them, and a body naming `roles` is already
// refused as a field that does not exist.
func refuseRolesOnCreate(_ context.Context, _ db.Tx[db.Tenant], u *contracts.User) error {
	if len(u.Roles) == 0 {
		return nil
	}
	return fmt.Errorf("%w: roles are granted by POST %s/{id}/roles, so that a grant is an event somebody can audit",
		crud.ErrInvalid, spec.Path)
}
