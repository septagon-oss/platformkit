package main

// app.go is the reference application read as one sentence, and decision 0074's
// pilot: the modules it is made of are named once, in the order the composition
// builds them, and every edge between them is a contract one of the two declares
// rather than a hand-off this file performs.
//
// `run` and `start` are the two commands that go through it, which is the
// difference between a sentence and a comment about one: what the image runs and
// what a laptop runs are composed here, and what they are composed from is
// `pkit.Server.Explain`'s text, committed beside this file as
// COMPOSITION.development.md and COMPOSITION.production.md.
//
// The sentence reads in the order the composition is built: the product's own
// values first, because everything below reads them; then the modules, in the
// order their needs place them; then the words, the colours and the doors, which
// are what this product adds to them.

import (
	"fmt"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/admin"
	admincontracts "github.com/septagon-oss/platformkit/modules/admin/contracts"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/auth"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/billing"
	"github.com/septagon-oss/platformkit/modules/change"
	"github.com/septagon-oss/platformkit/modules/content"
	contentcontracts "github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/file"
	filecontracts "github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/site"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/web"
	webcontracts "github.com/septagon-oss/platformkit/modules/web/contracts"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/ui/page"
)

// composition is the application answered with no effects: every module it is
// made of, and the values main itself has to hold — the tenant service, because
// the kernel resolves every host through it; the auth service, because the kernel
// asks it who is calling and what they may do; the user service, because
// bootstrap creates the first administrator; the notification service, because the
// modules that raise notices are wired against it; and the mailer, so that a test
// can read what would have been sent.
//
// Every one of those is a composition's *output* now, read off the plan the
// sentence resolved to, which is what `pkit.Value` is for. There is no second
// place a service is handed over: the same values the resolver put, seen from
// outside it.
type composition struct {
	modules []module.Module
	options app.Options
	planned *pkit.Planned
	tenants tenantcontracts.Service
	users   usercontracts.Service
	auth    authcontracts.Auth
	notify  notificationcontracts.Service
	mail    notificationcontracts.Mailer
	files   filecontracts.Service
	plans   httpx.Entitler
	task    taskcontracts.Service
	content contentcontracts.Service
	sites   sitecontracts.Service
	// The three values the workspace's own face is read from, and the two facts
	// about the installation that no module owns: whether it mounted a password
	// door at all, and whether a mail could leave it. See connection.go.
	passkeys     authcontracts.PasskeyDoor
	links        webcontracts.Links
	signin       admincontracts.Signin
	recoveryMail bool
	// connection is the face this composition mounts at /api/v1/app/connection,
	// filled below for the same reason `ask` and `shell` are: the mount is written
	// in the sentence, and the answers come out of the plan.
	connection *connectionFace
	// passwordDoor is whether this installation mounted a password door at all,
	// which is a fact about the composition and not about a tenant: a workspace
	// with no door cannot be signed in to with a password whatever its person's
	// account says.
	passwordDoor bool
	access       httpx.AskForAccess
	granter      page.Granter
	messages     xtext.Catalog
}

// reference is the application, its deployment, and the composition it resolved.
type reference struct {
	app  *pkit.App
	once pkit.Deployment
	composition
}

// compose is the reference application for the development deployment, which is
// what every test in this repository boots: the same sentence, the same
// resolution, the same order, no process and no connection.
func compose(cfg config.Config) composition {
	return composeReference(cfg, pkit.Development).composition
}

// composeReference is the sentence and the one resolution of it: the composition,
// its deployment, and the process's reading of the services it resolved.
func composeReference(cfg config.Config, env pkit.Environment) reference {
	// The two values the app's own chrome reads and no module can build: the
	// reach an ask has here, which the access module fills once the two services
	// it joins exist, and the shell the ask's two pages are drawn in, which is
	// filled below from the skin the resolved composition answered with.
	once := pkit.Deployment{
		Environment: env,
		Config:      cfg,
		Inputs:      deploymentInputs(cfg),
		Transports:  transports(),
		Caches:      caches(),
	}
	a, ask, shell, face := sentences(cfg)
	p, err := a.Plan(once)
	if err != nil {
		// A composition this file wrote that does not resolve is a defect in this
		// build, and a defect in a binary is announced by not starting.
		panic("platformkit: " + err.Error())
	}
	c := composition{
		planned: p, modules: p.Modules(), options: p.Options(), messages: catalogues(),
		tenants: value[tenantcontracts.Service](p), users: value[usercontracts.Service](p),
		auth: value[authcontracts.Auth](p), notify: value[notificationcontracts.Service](p),
		mail: value[notificationcontracts.Mailer](p), files: value[filecontracts.Service](p),
		plans:   value[httpx.Entitler](p),
		task:    value[taskcontracts.Service](p),
		content: value[contentcontracts.Service](p),
		sites:   value[sitecontracts.Service](p),
		access:  ask,
		// The words a refusal is allowed to use: the label of the grant that
		// gates role management, read off the manifest that defines it.
		granter:      refusalGrant(p.Skin()),
		passkeys:     auth.NewPasskeyDoor(),
		links:        value[webcontracts.Links](p),
		signin:       value[admincontracts.Signin](p),
		recoveryMail: cfg.Mail.Enabled(),
		connection:   face,
	}
	// Whether a password door stands here at all is read off the plan the sentence
	// resolved and not written as a constant: a composition that took auth out
	// would otherwise keep advertising a door it stopped mounting, which is the
	// same lie the ask above is built to avoid.
	c.passwordDoor = c.auth != nil
	*shell = faultShell(c.messages, c.granter)
	// The face, filled from the composition the plan answered. The mount was
	// written into the sentence with an empty holder beside it, because a mount is
	// part of the sentence and the answers are not.
	*face = connectionFace{describe: c.describe}
	return reference{app: a, once: once, composition: c}
}

// sentences is the one statement of this application's composition, and the two
// values its own chrome reads: the reach an ask has here, which the access module
// fills once the services it joins exist, and the shell of the ask's two pages,
// which is drawn from the skin the resolved composition answered with.
// that boots the reference application goes through it: `run` and `start`, which
// hand the whole thing to pkit, and appOptions, which is what a test in this
// repository boots the same composition with when it wants the handler rather
// than a process. There is one of it because a second statement of a
// composition is a second composition, and the two drift.
func sentences(cfg config.Config, without ...string) (*pkit.App, *accessReach, *page.Shell, *connectionFace) {
	ask, shell, face := &accessReach{}, &page.Shell{}, &connectionFace{}
	a := pkit.NewApp("platformkit").Use(
		omitted([]*pkit.Module{
			product(cfg),
			user.Module,
			tenant.Module,
			notification.Module,
			auth.Module,
			auth.EmailRegistration,
			file.Module,
			task.Module,
			billing.Module,
			content.Module,
			site.Module,
			web.Module,
			audit.Module,
			access(ask),
			change.Module,
			admin.Module,
		}, without)...).
		Theme(design.Default()).
		Languages(catalogues()).
		Home(pinnedHome).
		ErrorPage(func(s pkit.Skin) httpx.Fault { return faultPage(s.Copy, refusalGrant(s)) }).
		AskForAccess(ask, func(router *httpx.Router) { page.MountAccess(router, *shell) }).
		Roles(startingRoles()...).
		WorkspaceCatalog(workspaceFace(workspaceCatalog(), face.mount))
	return a, ask, shell, face
}

// omitted takes the names a case asked to leave out of the list. It exists for
// one reason: a case that asks what the composition resolves to with one of its
// providers gone has to take a module out of the only sentence that names them,
// and a second, quieter composition inside the test would answer a question
// nobody asked. Every name that is not one of the list's is a mistake, so it
// refuses rather than planning the whole thing back.
func omitted(uses []*pkit.Module, without []string) []*pkit.Module {
	if len(without) == 0 {
		return uses
	}
	dropped := map[string]bool{}
	kept := make([]*pkit.Module, 0, len(uses))
	for _, m := range uses {
		name := m.Name()
		want := false
		for _, w := range without {
			if w == name {
				want = true
				break
			}
		}
		if !want {
			kept = append(kept, m)
			continue
		}
		dropped[name] = true
	}
	for _, w := range without {
		if !dropped[w] {
			panic("platformkit: asked to leave out " + w + ", which this composition never named")
		}
	}
	return kept
}

// value is the plan's answer, and a composition this file wrote that does not
// provide one is the same defect the panic in composeReference names.
func value[T any](p *pkit.Planned) T {
	v, ok := pkit.Value[T](p)
	if !ok {
		panic("platformkit: the composition resolves with no " + fmt.Sprintf("%T", v) + " to read")
	}
	return v
}

// refusalGrant is who a refusal says may hand out what it refused, as a role and
// never as a name: the grant that gates role management is the auth module's
// fact, and its label is that module's words, read off the manifest that defines
// it.
func refusalGrant(s pkit.Skin) page.Granter {
	return page.Granter{Permission: authcontracts.PermissionRoleManage, Label: s.Label(authcontracts.PermissionRoleManage)}
}

// plan is the composition answered with no effects: every module built, every
// port answered, every recorded claim checked, nothing opened.
func (r reference) plan() *pkit.Planned { return r.planned }

// server is the process's own half: one listener, the applications it hosts, and
// the tenant hosts it claims. `run` and `start` both arrive here, which is why the
// role, the installation's host and the transports are filled once.
func (r reference) server(role app.Role) *pkit.Server {
	return pkit.NewServer().
		Config(r.once.Config).
		Deploy(r.once).
		Role(role).
		Host(r.app, pkit.Tenant("platformkit", startHost))
}

// explain is the composition file for the environment this reference was
// composed for: the resolved composition, the four answers the kernel asks the
// application for and who provides each, and the tenant hosts the process claims.
func (r reference) explain(role app.Role) string {
	text, err := r.server(role).Explain()
	if err != nil {
		// An unresolvable composition is a defect in this file, and this is the
		// one place a reader of a composition file can see it said out loud.
		return fmt.Sprintf("# COMPOSITION — platformkit\n\nrefused: %v\n", err)
	}
	return text
}

// startingRoles is this application's claim about who a tenant begins as, in
// pkit's words: Build refuses a grant no composed module defines and an operator
// grant a tenant's role may not hold, and Explain prints what survives. The
// personas themselves are still seeded by seedRoles, the tenant module's own
// creation hook — the engine has no seam for a starting role, and pkit's doc says
// as much.
//
// The list is roles.go's own literal, and the census test
// TestEveryRoleEveryComposedModuleDeclaresIsOneThisApplicationSeeds asks the
// resolved composition whether it names a role the list omits: what a new tenant
// is seeded with and what the modules declare cannot drift quietly apart.
func startingRoles() []pkit.Role {
	roles := make([]pkit.Role, 0, len(personas))
	for _, r := range personas {
		roles = append(roles, pkit.Role{Name: r.Name, Grants: append([]string(nil), r.Grants...)})
	}
	return roles
}

// appOptions is the same composition app.go sentences, read as app.Options: the
// in-process answer for a test that wants the handler rather than a process, and
// the reason the two cannot disagree about who provides what. The four fields
// that belong to a process rather than to a composition — the role, the
// transports, the stores, the installation's host — are filled here, because pkit
// leaves them to whoever is starting something.
func appOptions(cfg config.Config, c composition, role app.Role) app.Options {
	opts := c.options
	opts.Role = role
	// Transports and Caches are not decoration: kit/app refuses a role whose mode
	// has no constructor and a composition whose cache.adapter names a store it
	// never learned to reach, so a test that lost these lines fails in app.New
	// with a message about memory and jetstream, or about Caches.Valkey, rather
	// than booting the wrong thing.
	opts.Transports = transports()
	opts.Caches = caches()
	opts.Installation = app.Installation{Host: cfg.Server.InstallationHost}
	// The reach an ask has here is read back off the composition rather than left
	// in the plan's copy, because this is the seam a case uses to ask what happens
	// when the reach cannot answer: the port is this product's, and the only way to
	// ask whether its failure reads as an outage or as "nobody can grant this" is
	// to compose one that fails. Every other answer comes from the resolved plan.
	opts.Access = c.access
	return opts
}
