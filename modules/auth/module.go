// Package auth is signing in: sessions, passwords, single sign-on, and the
// roles that decide what a caller may do.
//
// The application constructs tenants with SeedRoles before wiring notification
// delivery and authentication. Role provisioning needs no running auth service.
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
	"github.com/septagon-oss/platformkit/kit/tenancy"
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

	// Notify is how somebody is told, inside the application, that a link was
	// sent. It never carries the link: the notice points at /auth/reset and the
	// secret is in the mail and nowhere else. A composition that wires none
	// still sends the mail.
	Notify contracts.Notifier

	// Mailer is where the link itself goes, and it is the only place it goes.
	// A composition that wires none issues no token either — a link nobody is
	// sent is a live credential in a table for an hour, for nothing — and the
	// forgotten-password route still answers as though it had.
	//
	// It is the notification module's own Mailer, wired by the application to
	// the same sender everything else uses, because this module needs to hand a
	// message over without it becoming a row first. See contracts.Mailer.
	Mailer contracts.Mailer

	// Hosts turns the tenant a link belongs to into the host its people reach
	// the application at, which a mailed link has to be built on: a link on the
	// installation's public host would send one customer's people to another
	// customer's front door. It is the notification module's own lookup, wired
	// by the application over the tenant module.
	Hosts contracts.Hosts

	// Tenants is how the hourly sweep reaches every tenant, to delete the
	// sessions and tokens that have expired.
	Tenants jobs.TenantLister

	// OIDC is the optional identity provider. An empty issuer means there is
	// none, and then the two OIDC routes are not registered at all.
	OIDC OIDC

	// PublicHost is the name the application believes it is reached at. One
	// thing is decided from it: whether the session cookie is marked Secure. A
	// browser refuses a Secure cookie over http://localhost, so a development
	// machine would be a development machine nobody could sign in to.
	PublicHost string
}

// Module is the manifest, and the service it is built on: main hands the same
// value to kit/app as the authorizer and the identity hook.
func Module(deps Deps) (contracts.Auth, module.Module) {
	secure := !config.Local(deps.PublicHost)
	svc := internal.NewService(deps.Users, deps.Notify, internal.Delivery{
		Mailer: deps.Mailer, Hosts: deps.Hosts, Secure: secure,
	})
	cookies := internal.NewCookies(secure)
	return svc, module.Module{
		Name:        "auth",
		Permissions: permissions,
		Events:      contracts.Events,
		Nav: []module.NavEntry{
			{Label: "Roles", Path: "/admin/auth/roles", Permission: contracts.PermissionRoleManage},
		},
		Jobs: []jobs.Job{internal.Sweep(svc, deps.Tenants)},
		// Two subscriptions, and they are the same fact from two directions:
		// somebody who cannot sign in has to be sent a link they can use.
		//
		// Both are subscriptions rather than calls inside the request, and for
		// two different reasons. The invitation one, because sending mail is
		// somebody else's machine and a request that waited on one would hold a
		// transaction open across it. The reset one, because the request may
		// not look the address up at all: a public route that took longer for
		// an address somebody has is an account enumeration oracle with a
		// stopwatch, so the lookup happens here, where nobody is timing it.
		Subscriptions: []events.Subscription{{
			Module: "auth", Name: usercontracts.EventInvited,
			Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
				var invited usercontracts.Invited
				if err := json.Unmarshal(ev.Payload, &invited); err != nil {
					return fmt.Errorf("auth: read the invitation: %w", err)
				}
				return svc.Offer(ctx, tx, invited.UserID)
			},
		}, {
			Module: "auth", Name: contracts.EventResetRequested,
			Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
				var asked contracts.ResetRequested
				if err := json.Unmarshal(ev.Payload, &asked); err != nil {
					return fmt.Errorf("auth: read the reset request: %w", err)
				}
				return svc.Reissue(ctx, tx, asked.Email)
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

// SeedRoles provisions a newly created tenant through auth's own storage path,
// independently of sessions, delivery and periodic jobs. Pass trusted defaults
// checked against the composition with contracts.CheckedPermissions before
// bootstrap or startup. Existing role grants are preserved on repeated calls.
func SeedRoles(ctx context.Context, tx db.Tx[db.System], tenant tenancy.Tenant, operator []string, defaults []contracts.Role) error {
	return internal.SeedRoles(ctx, tx, tenant, operator, defaults)
}
