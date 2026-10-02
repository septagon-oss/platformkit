package main

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"os"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/cache/providers/valkey"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	eventnats "github.com/septagon-oss/platformkit/kit/events/providers/nats"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/opa"
	"github.com/septagon-oss/platformkit/modules/admin"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/billing"
	billingcontracts "github.com/septagon-oss/platformkit/modules/billing/contracts"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/site"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/web"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/export"
	"github.com/septagon-oss/platformkit/ui/page"
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
	// plans answers what a tenant's subscription includes, for the operations
	// that declare a feature.
	plans httpx.Entitler
	// access is the reach an ask for access has in this product: the people who
	// hold role management, and the notice each of them gets.
	access httpx.AskForAccess
	// granter is who a refusal says may hand out what it refused.
	granter page.Granter
	// messages is this application's one catalogue — the same value the shells
	// below are given — carried here so the failure page the kernel renders is
	// worded from it rather than from a second read of the same files.
	messages xtext.Catalog
}

// compose constructs complete dependencies in order: users, tenants,
// notification delivery, then authentication. Role provisioning is independent
// of the authentication service, so the graph needs no late binding — the user
// module's floor below included, because it asks the roles table a question
// rather than the authentication service.
// taskRego is the reference application's object-scope policy for tasks, and taskPolicy
// that policy compiled once. A policy that does not compile is a defect in this build, so
// it fails at start rather than at the first request.
//
//go:embed policy/task.rego
var taskRego string

var taskPolicy = opa.MustNew("policy/task.rego", "platformkit.task", taskRego)

func compose(cfg config.Config) composition {
	// auth.AdministeringRoles is what makes "the last person who can still
	// administer this tenant" answerable at all: the user module owns who holds
	// a role, the auth module owns what a role grants, and neither reads the
	// other's rows. This is where the two meet, and the only line in this
	// application that decides administration is made of roles.
	// roles is the late-bound half: the user module is built before auth, because
	// auth looks people up, and the two questions below are about roles. The
	// fields are filled a few lines further down, before anything listens.
	roles := &roleGranter{}

	users, userModule := user.Module(user.Deps{
		Administration: &usercontracts.AdministrationFunc{Ask: auth.AdministeringRoles},
		// Granting somebody an administering role is a promotion, and this is the
		// one line that decides who may promote: the same authorizer the kernel
		// enforces every route with, asked about the auth module's own permission.
		Granting: roles,
	})

	installed := catalogues()
	tenants, tenantModule := tenant.Module(tenant.Deps{
		OnCreate: []tenantcontracts.Hook{seedRoles},
		Invite:   firstAdmin{users: users},
		// A tenant created here is served in every language this installation's
		// copy is written in, read off the catalogues rather than named: the set
		// exists before the operator narrows it, so a page can answer a person in
		// the language its text was authored in on the day the tenant appears.
		Languages: installed.Languages(),
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
		Mailer:  mail,
		Hosts:   hosts,
		Tenants: active,
		// Password-first signup with mailbox confirmation. This composition
		// turns it on for one reason, and it is a promise the kernel made on the
		// product's behalf: kit/httpx/aliases.go vouches for the three old
		// inquiry doors — /api/v1/auth/register, /resend-verification and
		// /verify-email — and an alias is only worth writing if the address it
		// aims at answers at the installation reading it. The case
		// TestEveryAliasRowOfTheReferenceApplicationLeadsSomewhereThatAnswers
		// asks the running server exactly that, one row at a time; turning this
		// line off takes those three rows out with it, and that case says so.
		//
		// What the mode gives a stranger is an account that cannot sign in:
		// RegisterUnverified stores the chosen password against an `unverified`
		// row and nothing activates it but the link in the mailbox — which, in a
		// deployment with no SMTP configured, is the in-memory mailbox above, so
		// no message leaves this machine. The roles come from here and never from
		// the form, and the one named is the tenant's ordinary member: the least
		// of the two the seed provisions.
		EmailRegistration: &authcontracts.EmailRegistration{Users: users, Roles: []string{authcontracts.RoleMember}},
		// The installation's own provider is the fallback; the tenant's row wins
		// where it names one, which is what lets two tenants on this one process
		// send their people to two issuers. The secret is resolved from the
		// environment by reference, per request, so it is in no row, no outbox
		// payload and no audit record.
		FactorKey:     cfg.Auth.FactorKey,
		OIDC:          auth.OIDCFromConfig(cfg.Auth.OIDC),
		OIDCProviders: tenantProviders{tenants: tenants},
		Secrets:       auth.EnvironmentSecrets{},
		// A tenant that sets `provision` has said, at its own control-plane route,
		// that an address its provider verified is an account here. This
		// application honours that: the person is made over the user module — the
		// adapter is in oidc.go, beside the other one — with the roles the tenant's
		// row names and no others, and the address confirmation the callback
		// records is what makes them able to sign in. A deployment that would
		// rather not admit anyone leaves this field unwired, and auth then answers
		// every tenant as `existing`: a refusal, and not a half-made person.
		Provisioner: provisioner{users: users},
		PublicHost:  cfg.Server.PublicHost,
	})

	// The file service is returned beside its manifest, as user's and
	// notification's are: a module that has to open a stored file takes
	// filecontracts.Opener, and this is where it would be handed one.
	// The plan answer is returned beside the manifest, because the kernel asks
	// it for every operation that declares a feature. See app.Options.Entitle.
	plans, billingModule := billing.Module(billing.Deps{Tenants: active, Payments: billing.Manual()})

	// The installation's flags, read by kit/config out of the flags block and
	// answered once for the whole process: configFlags targets nobody, not even by
	// tenant. One evaluator is built here, for the one door that asks it a question.
	var flagEval configFlags
	if cfg.Flags != nil {
		flagEval = cfg.Flags.Values
	}
	// The file service is returned under its own name, and not with `_`, because
	// rich text resolves the images a body embeds through it: see contentFiles
	// below, which is the port modules/content and modules/web declare for that.
	files, fileModule := file.Module(file.Deps{
		Storage: file.Local(cfg.Files.Dir), MaxBytes: cfg.Files.MaxBytes,
		QuotaBytes: cfg.Files.QuotaBytes,
		// Which class lives how long is the deployment's table (files.retention
		// in the config file) and who to walk is the tenant module's answer; this
		// line only hands the two to the module. A deployment that names no class
		// schedules no sweep at all — the same file.Module call, no job — which is
		// why the reference application ships the table empty: the classes are
		// whatever this product's uploads name them, and nothing here invents one.
		Retention: cfg.Files.Retention, Tenants: active,
	})
	contentFiles := file.RichTextFiles{Opener: files}
	contents, contentModule := content.Module(content.Deps{Files: contentFiles})
	sites, siteModule := site.Module(site.Deps{Gate: settingsGate{eval: flagEval}})

	mods := []module.Module{
		userModule,
		tenantModule,
		notificationModule,
		authModule,
		// Object scope (decision 0011): tasks are decided by policy/task.rego, embedded OPA.
		task.Module(task.Deps{Tenants: active, Policy: taskPolicy}),
		// The four reference modules a product is actually made of: what a
		// tenant pays, what it publishes, what its site looks like, and the
		// bytes behind both. Each takes the one thing it cannot decide for
		// itself — how money is taken, where files go — from here, which is the
		// file that names every module by definition.
		billingModule,
		contentModule,
		siteModule,
		fileModule,
		// The public site reads what the two above publish and claims the root.
		// A product with a storefront of its own composes that instead.
		web.Module(web.Deps{
			Site: sites, Content: contents, Files: contentFiles, Theme: design.Default(),
			// The two addresses the public site links and does not serve. They
			// are written here because they are this product's facts: which
			// shell it composed, and which door of which module answers for a
			// file a visitor may see. A module that named them would be naming a
			// surface it does not serve (see web.Deps), and a composition that
			// writes one is pinning an address the kernel composed — which is
			// only safe because a test asks the running server: the sign-in page
			// by TestPinnedAddresses, and the logo's file by
			// TestThePublicPageLinksOnlyAddressesTheInstallationServes, which
			// uploads a public file, sets it as the tenant's logo, reads the src
			// back off the public home page and fetches it.
			//
			// The file address is the file module's public door as the surface
			// composes it — /api/v1/public/<module>/<rel> — and not the /files/<id>
			// the module used to spell for itself: that spelling is a *document*
			// address, and the module that claims the public root answers
			// documents at /{slug}, so a two-segment /files/<id> is served by
			// nothing at all.
			SignInPath:    pinnedSignIn,
			PublicFileURL: func(id string) string { return pinnedPublicFile + "/" + id },
			// The refusal sentences the site's two addresses can answer with are the
			// kernel layer's, so the site shows them to a visitor in the language the
			// tenant is served in. What the site writes itself — the bar, the footer,
			// the empty states — stays in the source language and says so.
			Messages: installed,
		}),
	}
	// The trail is this reference product's worked example of something a plan
	// includes or does not. The name is the price list's, and it is chosen here
	// rather than inside the module because which features a product sells is
	// this file's decision and not the audit module's: an installation that
	// sells the trail to everybody leaves it empty and nothing is asked.
	mods = append(mods, audit.Module(audit.Deps{
		Tenants:       active,
		RetentionDays: cfg.Audit.RetentionDays,
		Feature:       "audit-trail",
	}))
	// Change control, over the one subject this product applies proposals for:
	// the site settings composed just above. The subject list and the flag are both
	// written in apps/platformkit/change.go, and this line is where they meet the
	// manifest — so the six proposal routes are mounted, the three permissions are
	// declared, and the settings door answers 409 with the proposal address when the
	// installation turns the switch on.
	mods = append(mods, change.Module(change.Deps{Subjects: changeSubjects(sites, site.NewLockedReader())}))
	// The shell is last, and for the same kind of reason audit is next to last:
	// it generates a screen for every resource the modules above it mounted, so
	// composing it earlier would generate screens for a prefix of the
	// application, silently. It draws navigation from the list it is handed and
	// asks the same authorizer the kernel enforces with.
	// design.Default() is where a client's own colours go, and the only line
	// that changes when they do: everything above the tokens is written in
	// terms of a role. See design.Pair.
	mods = append(mods, admin.Module(admin.Deps{
		Modules: mods, Authorize: auths, Tenants: tenants, Roles: auths, Sessions: auths,
		Theme: design.Default(), Storybook: operatorStorybook(cfg.Server.StorybookDir),
		Messages: installed, Locale: loginLocale,
		// The form on the shell's login page posts to the auth module's door.
		SignIn: pinnedSignInAPI}))

	roles.auth = auths
	checkPersonas(mods)
	return composition{modules: mods, tenants: tenants, users: users, auth: auths,
		notify: notify, mail: mail, plans: plans, messages: installed,
		access: accessReach{users: users, notify: notify, may: roles.May},
		// The words a refusal is allowed to use: the label of the grant that gates
		// role management, read off the manifest that defines it rather than
		// written again here.
		granter: page.Granter{Permission: authcontracts.PermissionRoleManage,
			Label: permissionLabel(mods, authcontracts.PermissionRoleManage)}}
}

// transports is the one place this application names an event provider. The
// kernel selects memory or jetstream by nats.transport and the role, and
// builds neither; a product that composes a different broker supplies its own
// constructor here and nowhere else. See app.Transports.
func transports() app.Transports {
	return app.Transports{Memory: memory.New, JetStream: eventnats.Connect}
}

// caches is the one place this application names a value store. cache.adapter
// selects between the in-process store the kernel builds itself and the shared one
// named here; a product that runs one process forever may leave this nil, and is
// refused the day it sets cache.adapter to valkey. See app.Caches.
func caches() app.Caches {
	return app.Caches{Valkey: valkey.Connect}
}

func operatorStorybook(dir string) func(context.Context) (export.Storybook, error) {
	if dir == "" {
		return nil
	}
	book := export.Storybook{Title: "Components", Theme: design.Default(), Examples: examples.Gallery(), Files: os.DirFS(dir)}
	return func(ctx context.Context) (export.Storybook, error) {
		tenant, ok := tenancy.FromContext(ctx)
		if !ok || !tenant.Operator {
			return export.Storybook{}, problem.New(http.StatusForbidden, "No storybook is available for this tenant.")
		}
		return book, nil
	}
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

// personas are the people this application is for beyond its administrator, each a
// role every new tenant is created with (decision 0011, item 6). A coordinator runs
// the task desk — raises, assigns and resolves — and an observer follows it and
// changes nothing. The admin role (everything but the operator's) and the member role
// (nothing until somebody grants it) are auth's own and are not repeated here.
//
// What each may do, and what each is refused, is persona_test.go's table; which task a
// coordinator may resolve is policy/task.rego's, not a grant's.
var personas = []authcontracts.Role{
	{Name: "coordinator", Grants: authcontracts.Permissions{taskcontracts.PermissionTaskRead, taskcontracts.PermissionTaskUpdate}},
	{Name: "observer", Grants: authcontracts.Permissions{taskcontracts.PermissionTaskRead}},
}

// checkPersonas refuses to compose an application whose personas grant a permission no
// composed module declares, or an operator one: a role naming it would grant nothing, or
// would hand the control plane to every tenant. It runs in compose, so bootstrap and
// every start fail before a database is opened rather than seeding a role that lies.
func checkPersonas(mods []module.Module) {
	var declared []tenancy.Grant
	for _, m := range mods {
		for _, p := range m.Permissions {
			declared = append(declared, tenancy.Grant{Permission: p.Key, Operator: p.Operator})
		}
	}
	for _, r := range personas {
		if _, err := authcontracts.CheckedPermissions(r.Grants, declared, tenancy.Tenant{}); err != nil {
			panic(fmt.Sprintf("platformkit: persona %q: %v", r.Name, err))
		}
	}
}

// seedRoles provisions auth's defaults and this application's personas in the
// tenant's creation transaction. Operator grants are named by the application that
// composes their owners.
func seedRoles(ctx context.Context, tx db.Tx[db.System], t *tenantcontracts.Tenant) error {
	return auth.SeedRoles(ctx, tx, t.Tenancy(), []string{
		tenantcontracts.PermissionTenantManage,
		billingcontracts.PermissionBillingCatalog,
	}, personas)
}

// roleGranter is Deps.Granting: may the caller of this write hand out a role that
// administers the tenant?
//
// The answer is the authorizer's, asked of the auth module's own permission, in
// the request's own tenant transaction — the same question every route is asked,
// one line earlier and about a different act. It is late-bound because the user
// module is built before the authentication service is; filling the field happens
// in compose, in the same order that makes the graph a list somebody wrote down
// rather than a discovery mechanism.
type roleGranter struct{ auth authcontracts.Auth }

func (g *roleGranter) May(ctx context.Context, _ db.Tx[db.Tenant]) (bool, error) {
	if g.auth == nil {
		// Compose has not finished: nothing can be serving requests yet, and a
		// promotion could not have reached a door. Refused rather than assumed.
		return false, nil
	}
	p, hasPrincipal := tenancy.PrincipalFrom(ctx)
	t, hasTenant := tenancy.FromContext(ctx)
	if !hasPrincipal || p.UserID == uuid.Nil || !hasTenant {
		return false, nil
	}
	return g.auth.Allowed(ctx, t, tenancy.Grant{Permission: authcontracts.PermissionRoleManage})
}

// accessReach is httpx.AskForAccess for this product.
//
// Recipients joins the two modules that each own half the answer: auth knows
// which of this tenant's roles manage roles, user knows who holds them. The join
// is here, in the composition, and in neither module. Tell writes one notice
// through the notification module — the bell in the application, with no mail:
// a message to every administrator for every refusal would make the notice
// worthless and put this ask in the queue beside a real set-password link.
//
// The link is the person's own generated screen: /app/user/users/<id>, the
// address the kernel composed for the user resource. It carries the person, not a
// token, so forwarding the notice grants nothing — the form behind it is guarded
// by the same door the API is.
type accessReach struct {
	users  usercontracts.Service
	notify notificationcontracts.Service
	// may is the composition's own way of asking the same question the kernel
	// asks a request: may this caller manage roles. A request carries a principal
	// and asks the authorizer; a caller with no principal on its context is not a
	// person and gets a no.
	may func(ctx context.Context, tx db.Tx[db.Tenant]) (bool, error)
}

func (a accessReach) Recipients(ctx context.Context, tx db.Tx[db.Tenant]) ([]uuid.UUID, error) {
	roles, err := auth.AdministeringRoles(ctx, tx)
	if err != nil {
		return nil, err
	}
	return a.users.Holders(ctx, tx, roles)
}

func (a accessReach) Tell(ctx context.Context, tx db.Tx[db.Tenant], n httpx.AccessNotice) error {
	_, err := a.notify.Notify(ctx, tx, notificationcontracts.Notice{
		Recipient: n.To,
		Title:     "Access requested",
		Body: fmt.Sprintf("%s asked for %s, at %s. Their roles are on the person's page.",
			n.Requester, n.Permission, n.RefusedPath),
		Link:  pinnedUsers + "/" + n.Requester.String(),
		Email: false,
	})
	return err
}

// permissionLabel is the words the defining module chose for one permission, read
// off the manifest that declares it. The refusal page is allowed to name a grant
// and not a person; what it may not do is invent the grant's name.
func permissionLabel(mods []module.Module, key string) string {
	for _, m := range mods {
		for _, p := range m.Permissions {
			if p.Key == key {
				return p.Label
			}
		}
	}
	return ""
}
