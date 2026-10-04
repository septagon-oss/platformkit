package main

// product.go is the reference application's share of its own composition, said
// once, in the resolver's words.
//
// `Use` names the kernel modules; two modules here name what is this product's
// rather than any module's. `product` is first in the list because it needs
// nobody: it is the values this installation decides and no module can derive —
// where uploads go, which flag gates a settings write, the object-scope policy,
// the plan feature the trail is sold under, the two addresses the public site
// links, the sign-in form the shell posts to, the languages a locale may be set
// to, the roles a new tenant is seeded with, and the empty grant-check holder the
// auth module fills. Before this file they were thirteen `Deps` literals in a
// composition file that ran before the resolver did.
//
// `access` is second to last, because it is the first thing in the application
// that can name a person, a notice and a site: the reach a refusal's ask has
// here, and the one subject this product puts under change control. Both are
// joins across modules this product composes, and neither belongs in any of them.
//
// The one edge neither module declares is the user module's promotion check,
// answered by the auth service. Declaring it would be the cycle user → auth →
// user, which is a fact about the graph; the holder `product` puts and `auth`
// fills is the mechanism pkit tests for exactly that case.

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
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/problem"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/opa"
	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
	auditcontracts "github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/auth"
	changecontracts "github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/file"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/site"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/task"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	webcontracts "github.com/septagon-oss/platformkit/modules/web/contracts"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/ui/components/examples"
	"github.com/septagon-oss/platformkit/ui/export"
)

// product is what this application adds before any module builds: the values it
// owns and no module may decide.
//
// Its declarations are built from the configuration the process was started
// with, which is the one thing a *pkit.Module made by a function can do that one
// at package scope cannot: an installation with no storybook directory composes
// no Storybook provider, and the shell's gallery answers Core to the operator
// alone rather than the app lying about having a gallery.
func product(cfg config.Config) *pkit.Module {
	decls := []pkit.Declaration{
		pkit.Provides[usercontracts.Administration](),
		pkit.Provides[usercontracts.Granting](),
		pkit.Provides[filecontracts.Storage](),
		pkit.Provides[auditcontracts.Plan](),
		pkit.Provides[tenancy.Policy](),
		pkit.Provides[sitecontracts.WriteGate](),
		// The door in front of one record's generated writes. The type is kit/rest's
		// own because a Spec hands that door to the routes it mounts; a second
		// product opinion about a different record's writes names its own port rather
		// than colliding on this one, and the resolver refuses two providers of the
		// same key rather than letting a gate be whichever module built last.
		pkit.Provides[rest.Gate](),
		pkit.Provides[webcontracts.Links](),
		pkit.Provides[admincontracts.Signin](),
		pkit.Provides[admincontracts.Locale](),
		pkit.Provides[tenantcontracts.Languages](),
		pkit.Contributes[tenantcontracts.Hook](),
		pkit.Contributes[changecontracts.SubjectBinding](),
	}
	if cfg.Server.StorybookDir != "" {
		decls = append(decls, pkit.Provides[admincontracts.Storybook]())
	}
	return pkit.NewModule("product", func(w *pkit.Wiring) (module.Module, error) {
		installed := catalogues()
		// auth.AdministeringRoles is what makes "the last person who can still
		// administer this tenant" answerable at all: the user module owns who
		// holds a role, the auth module owns what a role grants, and neither reads
		// the other's rows. This is where the two meet, and the only line in this
		// application that decides administration is made of roles.
		pkit.Put[usercontracts.Administration](w, &usercontracts.AdministrationFunc{Ask: auth.AdministeringRoles})
		// The empty holder. user reads it and auth fills its Ask — the one edge in
		// this application that is late-bound rather than built, and product puts
		// it precisely because it needs nobody and so is built first.
		pkit.Put[usercontracts.Granting](w, &usercontracts.GrantingFunc{})
		pkit.Put[filecontracts.Storage](w, file.Local(cfg.Files.Dir))
		// The trail is this reference product's worked example of something a plan
		// includes or does not. The name is the price list's, and it is chosen here
		// rather than inside the module because which features a product sells is
		// this file's decision and not the audit module's: an installation that
		// sells the trail to everybody composes no Plan and nothing is asked.
		pkit.Put[auditcontracts.Plan](w, auditcontracts.Plan("audit-trail"))
		// Object scope (decision 0011): tasks are decided by policy/task.rego.
		pkit.Put[tenancy.Policy](w, taskPolicy)
		// The installation's flags, read by kit/config out of the flags block and
		// answered once for the whole process: configFlags targets nobody, not even
		// by tenant. One evaluator is built here, for the one door that asks it.
		var flagEval configFlags
		if cfg.Flags != nil {
			flagEval = cfg.Flags.Values
		}
		pkit.Put[sitecontracts.WriteGate](w, settingsGate{eval: flagEval})
		// The task desk's five generated routes get the same kind of door, over the
		// same one evaluator and a different key: which of a task's fields this
		// installation will not let one account move alone is apps/platformkit/change.go,
		// and modules/task hands the answer to its own mount because a Spec mounts its
		// own routes (task.Deps.Gate).
		pkit.Put[rest.Gate](w, taskGate(flagEval))
		// The two addresses the public site links and does not serve. They are
		// written here because they are this product's facts: which shell it
		// composed, and which door of which module answers for a file a visitor may
		// see. A module that named them would be naming a surface it does not serve,
		// and a composition that writes one is pinning an address the kernel
		// composed — which is only safe because a test asks the running server: the
		// way in by TestThePublicFrameLinksOnlyTheWorkspaceRoot, and the logo's file
		// by TestThePublicPageLinksOnlyAddressesTheInstallationServes, which uploads
		// a public file, sets it as the tenant's logo, reads the src back off the
		// public home page and fetches it.
		//
		// The way in is the workspace root, not the sign-in page below it.
		// pinnedSignIn stays the address the guard turns a refused caller towards,
		// and the fault chrome keeps linking it; this line is what an anonymous
		// visitor on a tenant's own host is offered, and a public frame may offer no
		// address deeper into a workspace than its root. The root asks no permission
		// of its own — httpx.SignedIn guards it — so the workspace decides whether
		// the visitor gets a page or the form, and returns them to the root once
		// they have signed in.
		pkit.Put[webcontracts.Links](w, webcontracts.Links{
			SignIn:     pinnedWorkspace,
			PublicFile: func(id string) string { return pinnedPublicFile + "/" + id },
		})
		// The form on the shell's login page posts to the auth module's door, and
		// the link to the registration form is the same value, so neither half can
		// offer what the other did not mount. The address is the public door as the
		// surface composes it — see pinnedRegisterAPI.
		pkit.Put[admincontracts.Signin](w, admincontracts.Signin{
			Address: pinnedSignInAPI,
			Registration: &admincontracts.Registration{
				Kind: admincontracts.KindPassword, Address: pinnedRegisterAPI,
			},
		})
		pkit.Put[admincontracts.Locale](w, admincontracts.Locale(loginLocale))
		// A tenant created here is served in every language this installation's
		// copy is written in, read off the catalogues rather than named: the set
		// exists before the operator narrows it, so a page can answer a person in
		// the language its text was authored in on the day the tenant appears.
		pkit.Put[tenantcontracts.Languages](w, tenantcontracts.Languages{Tags: installed.Languages()})
		pkit.Put[tenantcontracts.Hook](w, tenantcontracts.Hook(seedRoles))
		// A task is this product's second change subject, and it needs no service to
		// be named: the locked read a diff is made against and the whole-row write an
		// approved proposal comes through are two named constructors of the module that
		// owns the table (task.NewWriter says why the gate never stands on that write).
		// Which of a task's fields this installation will not let one account move is
		// apps/platformkit/change.go's opinion, over the switch this module already
		// holds — which is why the subject is contributed here, needing nobody, and not
		// from access below, which had to wait for a site service to read its own.
		pkit.Put[changecontracts.SubjectBinding](w, taskSubjectBinding(task.NewWriter(), task.NewLockedReader()))
		if cfg.Server.StorybookDir != "" {
			pkit.Put[admincontracts.Storybook](w, admincontracts.Storybook(operatorStorybook(cfg.Server.StorybookDir)))
		}
		return module.Module{Name: "product"}, nil
	}, decls...)
}

// access is the reach an ask for access has in this product, and the site settings
// subject this product puts under change control. Both are joins across modules it
// composes — auth knows which of a tenant's roles manage roles, user knows who
// holds them, notification writes the notice — and in none of them. The product's
// second subject, the task, is contributed by product, which needs no service to
// name one (pkit counts one contribution per contributing module).
//
// It is composed second to last because it is the first thing here that can name
// a person, a notice, an authorizer and a site at once. The two values it fills
// are the app's own, made before the sentence and read after it: `ask` is what
// the kernel's refusal door calls, and `granter` is who a refusal may say may
// hand out what it refused — the label of which is the composed manifest's word,
// so the application reads it off the resolved plan rather than a module.
func access(ask *accessReach) *pkit.Module {
	return pkit.NewModule("access", func(w *pkit.Wiring) (module.Module, error) {
		users, notices := pkit.Get[usercontracts.Service](w), pkit.Get[notificationcontracts.Service](w)
		sites := pkit.Get[sitecontracts.Service](w)
		// Recipients joins the two modules that each own half the answer. Tell
		// writes one notice through the notification module — the bell in the
		// application, with no mail: a message to every administrator for every
		// refusal would make the notice worthless and put this ask in the queue
		// beside a real set-password link.
		//
		// The link is the person's own generated screen: /app/user/users/<id>, the
		// address the kernel composed for the user resource. It carries the person,
		// not a token, so forwarding the notice grants nothing — the form behind it
		// is guarded by the same door the API is.
		*ask = accessReach{users: users, notify: notices, may: pkit.Get[usercontracts.Granting](w).May}
		// The site settings the shell writes are the subject this module contributes:
		// a subject is a row, and this is the first module in the composition with a
		// site service to read one with. The flag that gates that write is this
		// application's own (apps/platformkit/change.go says which, and why a module
		// that knew would be a module with a customer in it).
		// One contribution each: a module contributes the one of a contract it is, so
		// this product's second subject — the task — is contributed by the product
		// module, which needs nobody and can build it from two named constructors, and
		// a slice the resolver would refuse is what a loop over both here would be.
		pkit.Put[changecontracts.SubjectBinding](w, siteSubjectBinding(sites, site.NewLockedReader()))
		return module.Module{Name: "access"}, nil
	},
		pkit.Needs[usercontracts.Service](),
		pkit.Needs[notificationcontracts.Service](),
		pkit.Needs[sitecontracts.Service](),
		pkit.Needs[usercontracts.Granting](),
		pkit.Contributes[changecontracts.SubjectBinding](),
	)
}

// taskRego is the reference application's object-scope policy for tasks, and
// taskPolicy that policy compiled once. A policy that does not compile is a
// defect in this build, so it fails at start rather than at the first request.
//
//go:embed policy/task.rego
var taskRego string

var taskPolicy = opa.MustNew("policy/task.rego", "platformkit.task", taskRego)

// transports is the one place this application names an event provider. The
// kernel selects memory or jetstream by nats.transport and the role, and builds
// neither; a product that composes a different broker supplies its own
// constructor here and nowhere else. See app.Transports.
func transports() app.Transports {
	return app.Transports{Memory: memory.New, JetStream: eventnats.Connect}
}

// caches is the one place this application names a value store. cache.adapter
// selects between the in-process store the kernel builds itself and the shared
// one named here; a product that runs one process forever may leave this nil, and
// is refused the day it sets cache.adapter to valkey. See app.Caches.
func caches() app.Caches {
	return app.Caches{Valkey: valkey.Connect}
}

// deploymentInputs is the process naming which implementations its deployment has
// inputs for. It is not an edge and it names no module: the key list is what
// makes an implementation choosable (pkit refuses one whose inputs are absent),
// and the values it runs on are read from the typed section by the module that
// picked it. The one thing that can go wrong here is the pair disagreeing with
// cfg.Mail.Enabled(), and composition_deployment_inputs_test.go refuses that.
func deploymentInputs(cfg config.Config) map[string]string {
	if !cfg.Mail.Enabled() {
		return nil
	}
	return map[string]string{
		"mail.host": cfg.Mail.Host, "mail.port": fmt.Sprint(cfg.Mail.Port), "mail.from": cfg.Mail.From,
	}
}

// operatorStorybook is the gallery's own door: no directory named answers no
// gallery, and the composition names no Storybook provider at all in that case
// (see product). What it does name when there is one is the operator's tenancy:
// the selection is never read from a query parameter.
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

// accessReach is httpx.AskForAccess for this product, filled by the access module
// once the two services it joins exist.
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
