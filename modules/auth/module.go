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
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
)

// OIDC is one OpenID Connect provider. It has the same shape as config.OIDC, so
// main converts one to the other in a line, and this module depends on a struct
// of its own rather than on the application's configuration surface.
type OIDC = internal.OIDC

// Deps is what this module cannot make for itself.
type Deps struct {
	// Users is how a password login finds the person an address belongs to.
	// It is narrower than the user module's own Service: a consumer depends on
	// the capability it uses.
	Users contracts.Users

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
// value to kit/app as the authorizer and the identity hook, and hands its
// SeedRoles to the tenant module as a create hook.
func Module(deps Deps) (contracts.Auth, module.Module) {
	svc := internal.NewService(deps.Users)
	secure := !config.Local(deps.PublicHost)
	cookies := internal.NewCookies(secure)
	return svc, module.Module{
		Name: "auth",
		// None, and the absence is a decision: see contracts/permissions.go.
		Permissions: nil,
		Events: []string{
			contracts.EventLoggedIn, contracts.EventLoggedOut, contracts.EventLoginFailed,
		},
		Nav:           nil,
		Jobs:          nil,
		Subscriptions: nil,
		Routes: func(api *httpx.API) {
			internal.RegisterRoutes(api, svc, cookies)
			if deps.OIDC.Issuer != "" {
				internal.RegisterOIDCRoutes(api, svc, deps.Users,
					internal.NewProvider(deps.OIDC, cookies, secure))
			}
		},
	}
}
