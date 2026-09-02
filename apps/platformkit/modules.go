package main

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/tenant"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// composition is the application: every module it is made of, and the values
// main itself has to hold — the tenant service, because the kernel resolves
// every host through it; the auth service, because the kernel asks it who is
// calling and what they may do; the user service, because bootstrap creates the
// first administrator; the notification service, because the modules that raise
// notices are wired against it; and the mailer, so that a test can read what
// would have been sent.
type composition struct {
	modules []module.Module
	tenants tenantcontracts.Service
	users   usercontracts.Service
	auth    authcontracts.Auth
	notify  notificationcontracts.Service
	mail    notificationcontracts.Mailer
}

// compose is the whole wiring graph and there is nothing else to read.
//
// The order below is the construction order, and the construction order is the
// dependency order: notification and auth take the user module's capability,
// and the tenant module takes a hook from auth. That last edge is the one worth
// pausing on. By imports, tenant is the lowest module here and auth is the
// highest: nothing in modules/tenant names modules/auth. By construction,
// tenant comes later, because it is handed a function auth owns. A hook is how
// a module low in the graph is notified by one above it without depending on
// it, and this line is where the two orders meet — visibly, in one file,
// checked by the compiler.
//
// Audit is last, and for a different reason: it subscribes to every event every
// other module declares, so it takes the list the composition above it produced
// (module.EventNames). Composing it earlier would audit a prefix of the
// application, silently.
//
// A dependency somebody forgot is a compile error on the line that forgot it,
// and a cycle cannot be expressed. See docs/adr/0002.
func compose(cfg config.Config) composition {
	users, userModule := user.Module(user.Deps{})

	mail := mailer(cfg)
	notify, notificationModule := notification.Module(notification.Deps{
		// The app adapts, so notification never names user: the interface is
		// declared in notification/contracts and satisfied here.
		Recipients: recipients{users: users},
		Mailer:     mail,
		PublicHost: cfg.Server.PublicHost,
	})

	// active is the tenant list the periodic jobs walk, and it is filled in
	// three lines below, once the tenant module exists.
	//
	// The knot is real and this is where it is tied. By construction the tenant
	// module comes after auth, because it is handed auth's role-seeding hook;
	// by need, auth's hourly sweep walks every tenant. Two edges pointing
	// opposite ways, and one of them has to be late. It is read at the first
	// tick of an hourly job, long after compose has returned, and a nil one is
	// an error the job reports rather than a panic.
	active := &deferred{}
	auths, authModule := auth.Module(auth.Deps{
		Users:   users,
		Notify:  notify,
		Tenants: active,
		// The permissions the operator's own administrator is granted by name.
		// The auth module cannot name tenant:manage — it is composed before the
		// module that declares it, and naming another module's manifest is gate
		// 6 — so the application, which names every module by definition, says
		// which permissions belong to the installation rather than to a
		// customer. kit/app refuses to start if a route and a manifest disagree
		// about that, so a name that drifts is a boot failure and not a hole.
		Operator:   []string{tenantcontracts.PermissionTenantManage},
		OIDC:       auth.OIDC(cfg.Auth.OIDC),
		PublicHost: cfg.Server.PublicHost,
	})

	tenants, tenantModule := tenant.Module(tenant.Deps{
		OnCreate: []tenantcontracts.Hook{seedRoles(auths)},
	})

	// Active rather than the service itself: the periodic jobs walk the tenants
	// that are being served, and a suspended one is not.
	active.lister = tenantcontracts.Active{Service: tenants}
	mods := []module.Module{
		userModule,
		notificationModule,
		authModule,
		tenantModule,
		task.Module(task.Deps{Tenants: active}),
	}
	mods = append(mods, audit.Module(audit.Deps{
		Events:        module.EventNames(mods),
		Tenants:       active,
		RetentionDays: cfg.Audit.RetentionDays,
	}))

	return composition{modules: mods, tenants: tenants, users: users, auth: auths, notify: notify, mail: mail}
}

// mailer is the one choice this application makes about mail: the SMTP sender
// when a server is configured, and the in-memory mailbox when none is. The
// mailbox is not a stub — it keeps every message and logs each one — so a
// deployment without mail records every notification, shows it in the
// application, and says what it would have sent. run() warns at boot.
func mailer(cfg config.Config) notificationcontracts.Mailer {
	if !cfg.Mail.Enabled() {
		return notificationtest.NewMailbox()
	}
	return notification.SMTP(notification.Mail{
		Host: cfg.Mail.Host, Port: cfg.Mail.Port, Username: cfg.Mail.Username,
		Password: cfg.Mail.Password, From: cfg.Mail.From,
	})
}

// deferred is the tenant list the modules composed before modules/tenant walk,
// filled in by compose as soon as that module exists.
//
// It is the one late binding in the wiring graph and it is written down rather
// than hidden. Composition order is dependency order everywhere else; here two
// dependencies point opposite ways — the tenant module takes a hook the auth
// module owns, and the auth module's sweep takes the list the tenant module
// answers — so one of them is a value that arrives a few lines later. Nothing
// reads it until the first tick of an hourly job, and a composition that forgot
// to fill it in is an error in that job's log rather than a nil dereference.
type deferred struct{ lister jobs.TenantLister }

func (d *deferred) List(ctx context.Context, tx db.Tx[db.System]) ([]tenancy.Tenant, error) {
	if d.lister == nil {
		return nil, errors.New("app: the tenant list was never wired; see compose")
	}
	return d.lister.List(ctx, tx)
}

// recipients is the adapter that lets the notification module send an email
// without knowing that users exist. It is four lines in the composition rather
// than an import in either module, which is idea 3 read in the direction that
// is easy to get wrong: the consumer declares the interface it needs, and the
// application — not the provider — decides who satisfies it.
type recipients struct{ users usercontracts.Service }

func (r recipients) Email(ctx context.Context, tx db.Tx[db.Tenant], userID uuid.UUID) (string, error) {
	u, err := r.users.Get(ctx, tx, userID)
	if err != nil {
		return "", err
	}
	return u.Email, nil
}

// seedRoles is the hook the tenant module runs inside the transaction that
// creates a tenant: a new customer gets the two roles their first administrator
// is about to be granted.
//
// Grepping SystemToken and this function is how a reader finds every place the
// application crosses a tenant boundary on purpose.
func seedRoles(a authcontracts.Auth) tenantcontracts.Hook {
	return func(ctx context.Context, tx db.Tx[db.System], t *tenantcontracts.Tenant) error {
		// The operator's own tenant gets the control plane's permission by
		// name; every customer's gets the wildcard, which is everything in
		// their own tenant and nothing outside it.
		return a.SeedRoles(ctx, tx, t.ID, t.Operator)
	}
}
