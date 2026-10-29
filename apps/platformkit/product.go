package main

// product.go declares the reference application's own share of its composition.
// Role-backed answers come from auth's grants provider, built before User and Auth.

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
	"github.com/septagon-oss/platformkit/kit/secrets"
	secretsenv "github.com/septagon-oss/platformkit/kit/secrets/providers/env"
	secretsfile "github.com/septagon-oss/platformkit/kit/secrets/providers/file"
	secretsbao "github.com/septagon-oss/platformkit/kit/secrets/providers/openbao"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/tenancy/providers/opa"
	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
	auditcontracts "github.com/septagon-oss/platformkit/modules/audit/contracts"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/change"
	changecontracts "github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/file"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	webcontracts "github.com/septagon-oss/platformkit/modules/web/contracts"
	webhookcontracts "github.com/septagon-oss/platformkit/modules/webhook/contracts"
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
//
// `grants` is the permission catalogue's empty holder, contributed to the roles
// hook below: this module builds first, so it is the one that can name the hook,
// and the catalogue the hook seeds from belongs to the resolution this module's
// own manifest is a part of. See lateCatalogue.
func product(cfg config.Config, grants *lateCatalogue) *pkit.Module {
	return productPictures(cfg, grants, admincontracts.Pictures{Source: filecontracts.PictureSource})
}

// productPictures uses the application's declared picture doors in its ordinary
// provider, so fixture and production compositions share the same typed wiring.
func productPictures(cfg config.Config, grants *lateCatalogue, pictures admincontracts.Pictures) *pkit.Module {
	return productWithPolicy(cfg, grants, pictures, taskPolicy)
}

// productWithPolicy supplies explicit policy rules through the same product
// declarations as the reference composition. The caller owns the adapter.
func productWithPolicy(cfg config.Config, grants *lateCatalogue, pictures admincontracts.Pictures, policy tenancy.Policy) *pkit.Module {
	decls := []pkit.Declaration{
		pkit.Provides[filecontracts.Storage](),
		// The door to the deployment's secrets, opened once here: which store it is
		// — the environment, a mounted directory, an OpenBao — is secrets.adapter's
		// answer, and a module that knew which would know the deployment. Both
		// halves of the port are put under their own keys, because a module that
		// reads a secret and a module that seals one are two needs that the same
		// store answers.
		pkit.Provides[secrets.Secrets](),
		pkit.Provides[secrets.Keys](),
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
		pkit.Provides[rest.TranslationSource](),
		pkit.Provides[admincontracts.Signin](),
		pkit.Provides[admincontracts.Pictures](),
		pkit.Provides[admincontracts.Locale](),
		pkit.Provides[admincontracts.PrimaryAction](),
		pkit.Provides[tenantcontracts.Languages](),
		pkit.Provides[authcontracts.ConfirmationChrome](),
		pkit.Provides[webhookcontracts.Catalogue](),
		pkit.Contributes[tenantcontracts.Hook](),
		pkit.Contributes[changecontracts.SubjectBinding](),
	}
	if cfg.Server.StorybookDir != "" {
		decls = append(decls, pkit.Provides[admincontracts.Storybook]())
	}
	return pkit.NewModule("product", func(w *pkit.Wiring) (module.Module, error) {
		installed := catalogues()
		// The bytes go wherever the deployment said they go, read off the configuration
		// the profile resolved rather than the one this function was handed: a profile
		// that picked the object store picks it for this product too, and a store built
		// from the settings as they arrived would be a different store from the one
		// Explain names. A store missing an input its adapter cannot do without is
		// refused here, before any effect; whether the address answers is asked of the
		// first upload, because nothing here dials (see objectStore).
		store, err := objectStore(pkit.Config(w, func(c config.Config) config.Files { return c.Files }))
		if err != nil {
			return module.Module{}, err
		}
		pkit.Put[filecontracts.Storage](w, store)
		// The secret store, opened once and put under both halves of the port;
		// `store` above is the object store, and the why sits with the Provides.
		secretsStore, err := secretStore(cfg)
		if err != nil {
			return module.Module{}, err
		}
		pkit.Put[secrets.Secrets](w, secretsStore)
		pkit.Put[secrets.Keys](w, secretsStore)
		// The trail is this reference product's worked example of something a plan
		// includes or does not. The name is the price list's, and it is chosen here
		// rather than inside the module because which features a product sells is
		// this file's decision and not the audit module's: an installation that
		// sells the trail to everybody composes no Plan and nothing is asked.
		pkit.Put[auditcontracts.Plan](w, auditcontracts.Plan("audit-trail"))
		// The product supplies object-scope rules; modules keep their lifecycle rules.
		pkit.Put[tenancy.Policy](w, policy)
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
		// see. A module that named them would name a surface it does not serve, and a
		// composition that writes one pins an address the kernel composed — safe only
		// because a test asks the running server what it actually answers.
		//
		// The way in is the workspace root, not the sign-in page below it.
		// pinnedSignIn stays the address the guard turns a refused caller towards, and
		// the fault chrome keeps linking it; this is what an anonymous visitor on a
		// tenant's own host is offered, and a public frame may offer no address deeper
		// into a workspace than its root. The root asks no permission of its own, so
		// the workspace decides whether the visitor gets a page or the form.
		pkit.Put[webcontracts.Links](w, webcontracts.Links{
			SignIn:     pinnedWorkspace,
			PublicFile: func(id string) string { return pinnedPublicFile + "/" + id },
		})
		// Which entity's records this product's public site renders, as the
		// translation port has to be handed it. It is a value and not a call: the
		// row set is derived from the entity's own schema tags, so naming it here
		// builds nothing and needs nobody — which is why this is the one edge the
		// sentence cannot draw between two modules. content needs the port that
		// holds the other languages, translation needs to know which table has
		// fields worth holding them for, and an edge in either direction between
		// the two is a cycle the resolver would refuse. So the application names
		// the entity — the same line this composition wrote by hand before the
		// resolver existed, now said in a place the graph can read.
		pkit.Put[rest.TranslationSource](w, content.TranslationSource())
		// The form on the shell's login page posts to the auth module's door, and
		// the link to the registration form is the same value, so neither half can
		// offer what the other did not mount. The address is the public door as the
		// surface composes it — see pinnedRegisterAPI.
		pkit.Put[admincontracts.Signin](w, admincontracts.Signin{
			Address: pinnedSignInAPI, OperationID: "auth-login",
			Registration: &admincontracts.Registration{
				Kind: admincontracts.KindPassword, Address: pinnedRegisterAPI, OperationID: "auth-register",
			},
		})
		// Where a stored picture's bytes are fetched from. The workspace catalogue
		// publishes this same address to a shell that is not a browser, and the
		// generated screens fetch through it; the door is the file module's and the
		// document is the kernel's, so this composition is the only party that has met
		// the two. See apps/platformkit/fault.go on the document's `pictures` block.
		pkit.Put[admincontracts.Pictures](w, pictures)
		pkit.Put[admincontracts.Locale](w, admincontracts.Locale(loginLocale))
		// What a person comes to this installation's workspace to do: the home leads
		// with it, for whoever may read the tasks it opens.
		pkit.Put[admincontracts.PrimaryAction](w, admincontracts.PrimaryAction{
			Label: "Go to tasks", Screen: "task/tasks", Permission: taskcontracts.PermissionTaskRead,
		})
		// A tenant created here is served in every language this installation's
		// copy is written in, read off the catalogues rather than named: the set
		// exists before the operator narrows it, so a page can answer a person in
		// the language its text was authored in on the day the tenant appears.
		pkit.Put[tenantcontracts.Languages](w, tenantcontracts.Languages{Tags: installed.Languages()})
		// The two addresses the sheet a mailed confirmation link opens has to link.
		// They are written here because every part of them is this product's fact:
		// which mount hands out the stylesheet, and which sign-in page a person who
		// has just confirmed their address is sent to. The module names the port and
		// never an address — TestTheEmailedLinkOpensAPageThatConfirms asks the running
		// application that the page the link opens answers, links the sheet named
		// here, and returns the person to the page named here.
		pkit.Put[authcontracts.ConfirmationChrome](w, authcontracts.ConfirmationChrome{
			Assets: pinnedAssets, SignIn: pinnedSignIn,
		})
		pkit.Put[tenantcontracts.Hook](w, seedRoles(grants.grants))
		// Which event names may be put on a webhook endpoint is this file's fact:
		// the modules that emit them are composed by this file, and the module that
		// checks a subscription against them cannot enumerate them. The literal is
		// webhook_catalogue.go's, and a case asks the resolved composition whether it
		// still matches what that composition emits.
		pkit.Put[webhookcontracts.Catalogue](w, webhookCatalogue)
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

// objectStore is this product's reading of files.store: the directory the kernel
// has always wired, or the object store the deployment named an address for.
//
// It branches on the setting and never on cloud.profile: a profile exists to pick
// adapters for a deployment that did not write this line, and a product that
// branched on it would have a vendor in it and would disagree with its own
// configuration whenever an operator overrode the profile. What it refuses is the
// input its adapter cannot build without — an endpoint and a bucket. It asks
// nothing of the network: construction is not readiness, and whether the address
// answers is what the first Put finds out.
//
// An empty scheme means https, as config.Load already settled; http is the one
// spelling that must be written down on purpose, because a store inside a private
// network is real and one on the public internet in plain text is not.
func objectStore(files config.Files) (filecontracts.Storage, error) {
	if files.Store != config.StoreS3 {
		return file.Local(files.Dir), nil
	}
	return file.S3(file.S3Config{
		Endpoint:     files.S3.Endpoint,
		Bucket:       files.S3.Bucket,
		AccessKey:    files.S3.AccessKey,
		SecretKey:    files.S3.SecretKey,
		Region:       files.S3.Region,
		Secure:       files.S3.Scheme != config.S3SchemeHTTP,
		BucketLookup: files.S3.BucketLookup,
	})
}

// workspace is the chrome this product hands to a module's own screen. It names no
// service and needs none: which shell a person's own list of notices sits in is the
// installation's answer, and the module that owns the screen asks for it the same way
// change asks for the shell of its two review pages. It is its own module rather than
// another value inside access because access needs the notification service, and a
// chrome provided by the module that needs the service it lists would be a cycle.
//
// Where the unread bell sits, and how many are in it, stays the shell's decision:
// the module mounts its list at its own address and names no link to it.
func workspace() *pkit.Module {
	return pkit.NewModule("workspace", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[*notification.Page](w, &notification.Page{Shell: noticeShell(catalogues())})
		return module.Module{Name: "workspace"}, nil
	},
		pkit.Provides[*notification.Page](),
	)
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
		users, notices := pkit.Get[usercontracts.RoleHolders](w), pkit.Get[notificationcontracts.Service](w)
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
		*ask = accessReach{users: users, notify: notices, may: pkit.Get[usercontracts.Granting](w).May, administration: pkit.Get[usercontracts.Administration](w)}
		// The site settings the shell writes are the subject this module contributes:
		// a subject is a row, and this is the first module in the composition with a
		// site service to read one with. The flag that gates that write is this
		// application's own (apps/platformkit/change.go says which, and why a module
		// that knew would be a module with a customer in it).
		// One contribution each: a module contributes the one of a contract it is, so
		// this product's second subject — the task — is contributed by the product
		// module, which needs nobody and can build it from two named constructors, and
		// a slice the resolver would refuse is what a loop over both here would be. The
		// locked read is the service above under the contract a caller about to write
		// is handed: a second door to it could not reach what the wiring gives it, and a
		// base document whose accent read differently from the API's answer would be a
		// proposal diffed against a site nobody was shown.
		pkit.Put[changecontracts.SubjectBinding](w, siteSubjectBinding(sites, pkit.Get[sitecontracts.LockedReader](w)))
		// The same join makes the two facts change control needs to be more than a
		// row: who is told about a decision, and where the thing decided is read.
		// modules/change owns the proposal, its page's content and no address, and
		// knows nobody owns delivery records (contracts.Notifier says why reviewers
		// are told nothing here). This is the one line that supplies both, over the
		// notification service the module above it handed out.
		pkit.Put[changecontracts.Notifier](w, proposalNotifier{notices: notices})
		pkit.Put[changecontracts.ProposalPage](w, change.NewProposalPage(pinnedProposals))
		// The queue and the decision page are this module's screens and the shell is
		// this product's chrome — the same four facts the fault pages use. The read
		// and the four commands are filled by change's own wire, from the service the
		// resolver builds, so nothing here can name a queue over a different service
		// than the one the routes are mounted on.
		pkit.Put[*change.ReviewChrome](w, &change.ReviewChrome{
			Shell:     reviewShell(catalogues()),
			Authorize: pkit.Get[httpx.Authorizer](w),
		})
		return module.Module{Name: "access"}, nil
	},
		pkit.Needs[usercontracts.RoleHolders](),
		pkit.Needs[notificationcontracts.Service](),
		pkit.Needs[sitecontracts.Service](),
		pkit.Needs[sitecontracts.LockedReader](),
		pkit.Needs[usercontracts.Granting](),
		pkit.Needs[usercontracts.Administration](),
		pkit.Needs[httpx.Authorizer](),
		pkit.Provides[changecontracts.Notifier](),
		pkit.Provides[changecontracts.ProposalPage](),
		pkit.Provides[*change.ReviewChrome](),
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

// secretStore is the one place this application names a secret store. The kernel
// builds no default the way it builds an in-process cache: here the default is the
// process's own environment, which opens nothing, so a deployment that says nothing
// about secrets still has one and is refused for no store it never named.
//
// Every adapter secrets.adapter can select is imported here, because a reference
// application that could name two of three would be a composition that silently
// cannot run the third.
func secretStore(cfg config.Config) (secrets.Store, error) {
	switch cfg.Secrets.Adapter {
	case "file":
		return secretsfile.New(cfg.Secrets)
	case "openbao":
		return secretsbao.New(cfg.Secrets)
	default:
		return secretsenv.FromOS()
	}
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
// picked it. Mail is the only port with three implementations, so this is the one
// place the state a server answers for is named in the resolver's words; naming
// nothing at all leaves the mail-less choice to the module itself — its simulated
// mailbox in a development deployment, the sender that refuses every send
// anywhere else. What can go wrong here is the list disagreeing with what
// kit/config loaded, and the composition tests refuse that: one boots a running
// production application that names no mail server, and every case that reads a
// link out of this process's memory boots the development one that does not.
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
	users          usercontracts.RoleHolders
	administration usercontracts.Administration
	notify         notificationcontracts.Service
	// may is the composition's own way of asking the same question the kernel
	// asks a request: may this caller manage roles. A request carries a principal
	// and asks the authorizer; a caller with no principal on its context is not a
	// person and gets a no.
	may func(ctx context.Context, tx db.Tx[db.Tenant]) (bool, error)
}

func (a accessReach) Recipients(ctx context.Context, tx db.Tx[db.Tenant]) ([]uuid.UUID, error) {
	roles, err := a.administration.Administering(ctx, tx)
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
		Link: pinnedUsers + "/" + n.Requester.String(),
		// No Wants: its zero value is in-app alone, which is what an access
		// request is - a note for somebody who is already in the application.
	})
	return err
}
