package main

import (
	"context"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/auth"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/tenant"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// composition is the application: every module it is made of, and the three
// services main itself has to hold — the tenant service, because the kernel
// resolves every host through it; the auth service, because the kernel asks it
// who is calling and what they may do; and the user service, because bootstrap
// creates the first administrator.
type composition struct {
	modules []module.Module
	tenants tenantcontracts.Service
	users   usercontracts.Service
	auth    authcontracts.Auth
}

// compose is the whole wiring graph and there is nothing else to read.
//
// The order below is the construction order, and the construction order is the
// dependency order: auth takes the user module's capability, and the tenant
// module takes a hook from auth. That last edge is the one worth pausing on. By
// imports, tenant is the lowest module here and auth is the highest: nothing in
// modules/tenant names modules/auth. By construction, tenant comes last,
// because it is handed a function auth owns. A hook is how a module low in the
// graph is notified by one above it without depending on it, and this line is
// where the two orders meet — visibly, in one file, checked by the compiler.
//
// A dependency somebody forgot is a compile error on the line that forgot it,
// and a cycle cannot be expressed. See docs/adr/0002.
func compose(cfg config.Config) composition {
	users, userModule := user.Module(user.Deps{})

	auths, authModule := auth.Module(auth.Deps{
		Users:      users,
		OIDC:       auth.OIDC(cfg.Auth.OIDC),
		PublicHost: cfg.Server.PublicHost,
	})

	tenants, tenantModule := tenant.Module(tenant.Deps{
		OnCreate: []tenantcontracts.Hook{seedRoles(auths)},
	})

	return composition{
		modules: []module.Module{
			userModule,
			authModule,
			tenantModule,
			// Active rather than the service itself: the periodic jobs walk the
			// tenants that are being served, and a suspended one is not.
			task.Module(task.Deps{Tenants: tenantcontracts.Active{Service: tenants}}),
		},
		tenants: tenants,
		users:   users,
		auth:    auths,
	}
}

// seedRoles is the hook the tenant module runs inside the transaction that
// creates a tenant: a new customer gets the two roles their first administrator
// is about to be granted.
//
// Grepping SystemToken and this function is how a reader finds every place the
// application crosses a tenant boundary on purpose.
func seedRoles(a authcontracts.Auth) tenantcontracts.Hook {
	return func(ctx context.Context, tx db.Tx[db.System], t *tenantcontracts.Tenant) error {
		return a.SeedRoles(ctx, tx, t.ID)
	}
}
