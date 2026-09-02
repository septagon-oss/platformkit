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
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/internal"
)

// Deps is what this module cannot make for itself, and it is empty. That is
// worth a struct rather than no parameter: the day it needs something, every
// call site gains a named field instead of a new argument.
type Deps struct{}

// spec is the entity's presence in the application: five routes, two
// permissions, three events and the schema a generated screen reads.
var spec = rest.Spec[*contracts.User]{
	Module:     "user",
	Entity:     "user",
	Path:       "/api/v1/user/users",
	Read:       contracts.PermissionUserRead,
	Write:      contracts.PermissionUserManage,
	SoftDelete: true,
	// The two fields a command owns. status is moved by Deactivate, which
	// publishes user.deactivated, and roles by SetRoles, which publishes
	// user.roles_set; a caller who could set either through the generic update
	// would deactivate somebody, or make them an administrator, and tell
	// nobody.
	//
	// roles used to be refused by accident: kit/crud's schema had no list type,
	// so the field did not exist as far as a patch was concerned. It exists
	// now, it renders, and it is refused by name — which is also what tells the
	// caller which door to use.
	Immutable: []string{"status", "roles"},
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
// The PATCH route is guarded by spec.Immutable instead; a create cannot be,
// because there is no row yet to refuse a change to.
func refuseRolesOnCreate(_ context.Context, _ db.Tx[db.Tenant], u *contracts.User) error {
	if len(u.Roles) == 0 {
		return nil
	}
	return fmt.Errorf("%w: roles are granted by POST %s/{id}/roles, so that a grant is an event somebody can audit",
		crud.ErrInvalid, spec.Path)
}
