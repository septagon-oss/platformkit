// Package tenant is the control plane: which customers exist, and which host
// belongs to which one.
//
// It is the module the kernel itself depends on. kit/httpx resolves every
// request's host through the Service here before the request is about anything,
// and kit/jobs walks the list of tenants it returns. That is why Module hands
// its service back to main as well as its manifest: main holds the value,
// because three other things need it.
//
// It imports no other module, and the modules above it reach it through hooks
// (contracts.Hook) rather than the other way round.
package tenant

import (
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/internal"
)

// permissions is what the manifest declares. Operator, because the control
// plane is every tenant rather than one: it is served on a customer's host,
// where a customer's administrator holds the wildcard, and a wildcard does not
// satisfy an operator grant. kit/app checks this against what the routes
// declare, so the two cannot drift.
var permissions = []module.Permission{{Key: contracts.PermissionTenantManage, Operator: true}}

// Deps is what this module cannot make for itself.
type Deps struct {
	// OnCreate runs inside the transaction that creates a tenant, in order.
	// main fills it: the auth module's role seeding is the one hook today.
	//
	// A hook rather than a subscription, because a tenant.created subscriber
	// runs when the worker gets to it, and the administrator this same
	// transaction creates tries to sign in before that.
	OnCreate []contracts.Hook

	// Invite gives a tenant its first administrator. A composition that wires
	// none does not mount the route, because a control plane that can create a
	// tenant nobody can ever sign in to is a control plane that creates
	// wreckage — and answering 501 to a route that exists is worse than not
	// having it.
	Invite contracts.Inviter
}

// Module is the manifest, and the service it is built on.
//
// It is the one module that returns its implementation as well as its
// description, and the reason is written in the package comment: the kernel
// resolves every host through it, the periodic jobs walk its list, and the task
// module takes it as a dependency, so main has to hold the value. Every other
// module keeps its implementation to itself.
func Module(deps Deps) (contracts.Service, module.Module) {
	svc := internal.NewService(deps.OnCreate)
	return svc, module.Module{
		Name:        "tenant",
		Permissions: permissions,
		Events:      []string{contracts.EventCreated, contracts.EventSuspended, contracts.EventHostAdded},
		// The invitation route publishes user.invited, which the user module
		// declares and the auth module subscribes to. It is not listed above
		// because a module's Events are the ones it owns: kit/app checks a
		// route's declaration against the union of every manifest, so an event
		// somebody else owns is still an event a subscriber can be written
		// against.

		Nav: []module.NavEntry{
			{Label: "Tenants", Path: "/admin/tenant/tenants", Permission: contracts.PermissionTenantManage},
		},
		// Written out so the absence is a decision. This module has no periodic
		// work of its own: it is what the other modules' periodic work walks.
		Jobs:          nil,
		Subscriptions: nil,
		// The one call in the application that takes cross-tenant access, at
		// the one moment the kernel offers it, in the manifest a reviewer is
		// already reading. See docs/adr/0006.
		Routes: func(api *httpx.API) { internal.RegisterRoutes(api, svc, deps.Invite, api.SystemToken()) },
	}
}

// Bootstrap creates the first tenant of an empty installation, refusing when
// there already is one. `platformkit bootstrap` is its only caller; it is
// exported here rather than reached through Service so that "the write with no
// caller to authorize" is one name a reviewer can grep for.
var Bootstrap = internal.Bootstrap
