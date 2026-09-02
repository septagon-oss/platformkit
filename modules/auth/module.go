// Package auth is signing in: sessions, passwords, single sign-on, and the
// roles that decide what a caller may do.
//
// It is the top of the dependency order — it takes the user module's capability
// and nothing takes its — and the module the kernel asks two questions of on
// every request: who is calling (httpx.Options.Authenticate) and may they
// (httpx.Authorizer). The tenant module sits below it and is notified of a new
// tenant through a hook main hands over, so that seeding a tenant's roles does
// not make the control plane import this package.
package auth

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// OIDC is one OpenID Connect provider. It has the same shape as config.OIDC, so
// main converts one to the other in a line, and this module depends on a struct
// of its own rather than on the application's configuration surface.
type OIDC = internal.OIDC

// Deps is what this module cannot make for itself.
type Deps struct {
	// Users is how a password login finds the person an address belongs to,
	// and how a password that has been earned is stored. It is narrower than
	// the user module's own Service: a consumer depends on the capability it
	// uses.
	Users contracts.Users

	// Notify is how a set-password or reset link reaches somebody. A
	// composition that wires none issues no token either — a link nobody is
	// sent is a live credential in a table for an hour, for nothing — and the
	// forgotten-password route still answers 200, because it always does.
	Notify contracts.Notifier

	// Tenants is how the hourly sweep reaches every tenant, to delete the
	// sessions and tokens that have expired.
	Tenants jobs.TenantLister

	// OIDC is the optional identity provider. An empty issuer means there is
	// none, and then the two OIDC routes are not registered at all.
	OIDC OIDC

	// Operator are the permissions the operator's own administrator is granted
	// by name when their tenant is created. A wildcard does not satisfy an
	// operator grant, so they are listed rather than implied.
	//
	// The application supplies them because they belong to the modules that
	// declare them — tenant:manage is modules/tenant's — and this module is
	// composed before those exist. A name missing from the list is a permission
	// nobody can exercise, which is the safe direction for a list to be wrong in.
	Operator []string

	// PublicHost is the name the application believes it is reached at. One
	// thing is decided from it: whether the session cookie is marked Secure. A
	// browser refuses a Secure cookie over http://localhost, so a development
	// machine would be a development machine nobody could sign in to.
	PublicHost string
}

// Module is the manifest, and the service it is built on: main hands the same
// value to kit/app as the authorizer and the identity hook, and hands its
// SeedRoles to the tenant module as a create hook.
func Module(deps Deps) (contracts.Auth, module.Module) {
	svc := internal.NewService(deps.Users, deps.Notify, deps.Operator)
	secure := !config.Local(deps.PublicHost)
	cookies := internal.NewCookies(secure)
	return svc, module.Module{
		Name:        "auth",
		Permissions: permissions,
		Events:      contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Roles", Path: "/admin/auth/roles", Permission: contracts.PermissionRoleManage},
		},
		Jobs: []jobs.Job{internal.Sweep(svc, deps.Tenants)},
		// One subscription, and it is the invitation flow: the user module
		// creates somebody with no password and says so, and this module is
		// what turns that into a link they can use. It is a subscription rather
		// than a call inside Invite because sending mail is somebody else's
		// machine, and a request that waited on one would hold a transaction
		// open across it.
		Subscriptions: []events.Subscription{{
			Module: "auth", Name: usercontracts.EventInvited,
			Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
				var invited usercontracts.Invited
				if err := json.Unmarshal(ev.Payload, &invited); err != nil {
					return fmt.Errorf("auth: read the invitation: %w", err)
				}
				return svc.Offer(ctx, tx, invited.UserID)
			},
		}},
		Routes: func(api *httpx.API) {
			// The catalogue, taken at the one moment the kernel has it and this
			// module is being wired. The hourly sweep reads it back to say which
			// roles name a permission nothing defines any more.
			svc.Declare(api.Permissions())
			internal.RegisterRoutes(api, svc, cookies)
			if deps.OIDC.Issuer != "" {
				internal.RegisterOIDCRoutes(api, svc, deps.Users,
					internal.NewProvider(deps.OIDC, cookies, secure))
			}
		},
	}
}

// permissions is what the manifest declares: one, guarding the two roles
// routes. Every other route here is about the caller themselves. See
// contracts/permissions.go.
var permissions = []module.Permission{{Key: contracts.PermissionRoleManage}}
