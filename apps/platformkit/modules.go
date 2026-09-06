package main

import (
	"context"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/billing"
	billingcontracts "github.com/septagon-oss/platformkit/modules/billing/contracts"
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/task"
	"github.com/septagon-oss/platformkit/modules/tenant"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/web"
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

// compose constructs complete dependencies in order: users, tenants,
// notification delivery, then authentication. Role provisioning is independent
// of the authentication service, so the graph needs no late binding.
func compose(cfg config.Config) composition {
	users, userModule := user.Module(user.Deps{})

	tenants, tenantModule := tenant.Module(tenant.Deps{
		OnCreate: []tenantcontracts.Hook{seedRoles},
		Invite:   firstAdmin{users: users},
	})
	active := tenantcontracts.Active{Service: tenants}
	hosts := tenantHosts{tenants: tenants}
	mail := mailer(cfg)
	notify, notificationModule := notification.Module(notification.Deps{
		// The app adapts, so notification never names user or tenant: both
		// interfaces are declared in notification/contracts and satisfied here.
		Recipients: recipients{users: users},
		Hosts:      hosts,
		Mailer:     mail,
		// The same rule the session cookie's Secure flag follows, so there is
		// one answer to "is this deployment https" and one place it is decided.
		Secure: !config.Local(cfg.Server.PublicHost),
	})

	auths, authModule := auth.Module(auth.Deps{
		Users:  users,
		Notify: notify,
		// The same sender and the same host lookup the notification module
		// takes, handed to the one module that has to put a secret in a message
		// without it becoming a row first: a set-password link belongs in the
		// mail and in nothing else. Everything else this application mails goes
		// out of the notification worker, which renders a row.
		Mailer:     mail,
		Hosts:      hosts,
		Tenants:    active,
		OIDC:       auth.OIDC(cfg.Auth.OIDC),
		PublicHost: cfg.Server.PublicHost,
	})

	// The file service is returned beside its manifest, as user's and
	// notification's are: a module that has to open a stored file takes
	// filecontracts.Opener, and this is where it would be handed one.
	contents, contentModule := content.Module(content.Deps{})
	sites, siteModule := site.Module(site.Deps{})
	_, fileModule := file.Module(file.Deps{
		Storage: file.Local(cfg.Files.Dir), MaxBytes: cfg.Files.MaxBytes,
		QuotaBytes: cfg.Files.QuotaBytes,
	})

	mods := []module.Module{
		userModule,
		tenantModule,
		notificationModule,
		authModule,
		task.Module(task.Deps{Tenants: active}),
		// The four reference modules a product is actually made of: what a
		// tenant pays, what it publishes, what its site looks like, and the
		// bytes behind both. Each takes the one thing it cannot decide for
		// itself — how money is taken, where files go — from here, which is the
		// file that names every module by definition.
		billing.Module(billing.Deps{Tenants: active, Payments: billing.Manual()}),
		contentModule,
		siteModule,
		fileModule,
		// The public site reads what the two above publish and claims the root.
		// A product with a storefront of its own composes that instead.
		web.Module(web.Deps{Site: sites, Content: contents, Theme: design.Default()}),
	}
	mods = append(mods, audit.Module(audit.Deps{
		Tenants:       active,
		RetentionDays: cfg.Audit.RetentionDays,
	}))
	// The shell is last, and for the same kind of reason audit is next to last:
	// it generates a screen for every resource the modules above it mounted, so
	// composing it earlier would generate screens for a prefix of the
	// application, silently. It draws navigation from the list it is handed and
	// asks the same authorizer the kernel enforces with.
	// design.Default() is where a client's own colours go, and the only line
	// that changes when they do: everything above the tokens is written in
	// terms of a role. See design.Pair.
	mods = append(mods, admin.Module(admin.Deps{
		Modules: mods, Authorize: auths, Tenants: tenants, Theme: design.Default()}))

	return composition{modules: mods, tenants: tenants, users: users, auth: auths, notify: notify, mail: mail}
}

// mailer is the one choice this application makes about mail: the SMTP sender
// when a server is configured, and the in-memory mailbox when none is. The
// mailbox is not a stub — it keeps every message and logs each one — so a
// deployment without mail records every notification, shows it in the
// application, and says what it would have sent. run() warns at boot.
func mailer(cfg config.Config) notificationcontracts.Mailer {
	if !cfg.Mail.Enabled() {
		return notification.NewMailbox()
	}
	return notification.SMTP(notification.Mail{
		Host: cfg.Mail.Host, Port: cfg.Mail.Port, Username: cfg.Mail.Username,
		Password: cfg.Mail.Password, From: cfg.Mail.From,
	})
}

// tenantHosts is the adapter that lets the notification and auth modules build a
// link to the recipient's own host without naming the tenant module.
//
// The lookup runs in the worker's own tenant transaction, so what it reads is
// the one tenant's rows the policy shows it. A tenant with several hosts answers
// at all of them and a message has to pick one, so it picks the primary — the
// first row of a list ordered by it (migrations/000020). It used to pick
// whichever name sorted first, which meant adding admin.acme.example.com moved
// every future link onto it.
type tenantHosts struct{ tenants tenantcontracts.Service }

func (h tenantHosts) PublicHost(ctx context.Context, tx db.Tx[db.Tenant]) (string, error) {
	hosts, err := h.tenants.Hosts(ctx, tx)
	if err != nil || len(hosts) == 0 {
		return "", err
	}
	return hosts[0], nil
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

// firstAdmin is the adapter behind POST /api/v1/tenant/tenants/{id}/invite.
//
// Provision with no password is the whole of it, and the two halves of that are
// deliberate. No password, so an operator who invites somebody into a customer's
// tenant does not know their credentials and never held them; the invitation
// event mails a link and the person chooses one. The admin role, because the
// route's purpose is a tenant that somebody can administer — an invitation that
// granted nothing would be a tenant still nobody can get into, which is the
// defect this route exists to close.
//
// It runs in the control plane's own system transaction, which is the only way
// to write a row into a tenant the request did not resolve to.
type firstAdmin struct{ users usercontracts.Service }

func (a firstAdmin) Invite(ctx context.Context, tx db.Tx[db.System], tenantID uuid.UUID, email, displayName string) error {
	_, err := a.users.Provision(ctx, tx, tenantID, email, displayName, "",
		[]string{authcontracts.RoleAdmin})
	return err
}

// seedRoles provisions auth's defaults in the tenant's creation transaction.
// Operator grants are named by the application that composes their owners.
func seedRoles(ctx context.Context, tx db.Tx[db.System], t *tenantcontracts.Tenant) error {
	return auth.SeedRoles(ctx, tx, t.Tenancy(), []string{
		tenantcontracts.PermissionTenantManage,
		billingcontracts.PermissionBillingCatalog,
	}, nil)
}
