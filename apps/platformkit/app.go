package main

// app.go is the reference application read as one sentence, and decision 0074's
// pilot: the modules it is made of are named once, in the order the composition
// builds them, and every edge between them is a contract one of the two declares
// rather than a hand-off this file performs. `run` and `start` both go through it,
// so what the image runs and what a laptop runs are one sentence, worded out of
// pkit.Server.Explain's text and committed beside this file as
// COMPOSITION.development.md and COMPOSITION.production.md.
//
// It reads in build order: the product's own values first, because everything
// below reads them; then the modules, in the order their needs place them; then
// the words, the colours and the doors, which are what this product adds to them.

import (
	"encoding/json"
	"fmt"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/locale/providers/xtext"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/tenancy"
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
	"github.com/septagon-oss/platformkit/modules/history"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/saved"
	scimcontracts "github.com/septagon-oss/platformkit/modules/scim/contracts"
	"github.com/septagon-oss/platformkit/modules/site"
	sitecontracts "github.com/septagon-oss/platformkit/modules/site/contracts"
	"github.com/septagon-oss/platformkit/modules/subject"
	"github.com/septagon-oss/platformkit/modules/task"
	taskcontracts "github.com/septagon-oss/platformkit/modules/task/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/translation"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/web"
	webcontracts "github.com/septagon-oss/platformkit/modules/web/contracts"
	"github.com/septagon-oss/platformkit/modules/webhook"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/ui/page"
)

// composition holds the resolved modules and the capabilities used by app commands.
// Every output comes from pkit.Value; module construction has one declared path.
type composition struct {
	modules      []module.Module
	options      app.Options
	planned      *pkit.Planned
	tenantList   tenantcontracts.Lister
	hostLookup   tenantcontracts.HostLookup
	oidcSettings tenantcontracts.OIDCReader
	samlSettings tenantcontracts.SAMLReader
	bootstrap    tenantcontracts.Bootstrapper
	locale       tenantcontracts.LocaleSetter
	provisioner  usercontracts.SystemProvisioner
	addresses    usercontracts.AddressLookup
	seedUsers    usercontracts.SeedWriter
	permissions  authcontracts.PermissionReader
	roleRepair   authcontracts.RoleRepairer
	notify       notificationcontracts.Service
	mail         notificationcontracts.Mailer
	files        filecontracts.Service
	plans        httpx.Entitler
	// contents, sites, tasks and files are the four owners a seeded record is
	// written through: this application's seed runs each resource's own write core
	// rather than a second spelling of it, so it reads the same values its routes
	// were mounted with. Named in the plural, like tenants and users, because each
	// is a store of many rows and not the one row.
	contents contentcontracts.Service
	tasks    taskcontracts.Service
	sites    sitecontracts.Service
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
	// messages is the copy this composition serves — the same value the sentence
	// handed to Languages, read here so the shell the kernel's failure page renders
	// is worded from it rather than from a second read of the same files. It is the
	// interface and not the provider: composeCopy is what replaces it.
	messages pkit.Catalogue
}

// catalogue is this application's permission catalogue: every permission the
// modules its sentence resolved declare, read off their manifests, and the one
// answer to a question two readers ask. The roles hook seeds a new tenant's
// administrator with the operator permissions of this list and nothing else, and
// repair-roles may take away only a grant this list says no module defines. A
// second list at either call site is how an installation ends up holding a grant
// no route in it will ever accept: a permission is defined by the module that
// owns it, and a module that is not composed defines nothing.
func (c composition) catalogue() []tenancy.Grant { return module.Grants(c.modules) }

// lateCatalogue is that answer's empty holder, the same shape as the seed
// provisioner, the fault shell and the ask's reach: the sentence contributes the
// roles hook before anything is resolved, and what the composition declares is
// only answerable once it is. composeReference fills it once, from the plan.
//
// An unfilled holder declares nothing, which is not a quiet answer for this
// application: it seeds two initial roles, and an initial role naming a
// permission nothing declares is refused rather than written.
type lateCatalogue struct {
	get func() []tenancy.Grant
}

func (c *lateCatalogue) grants() []tenancy.Grant {
	if c == nil || c.get == nil {
		return nil
	}
	return c.get()
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

// composeCopy is compose with its copy supplied. `installed` is the catalogue this
// installation ships and stays the answer to "which languages exist"; `served`, when
// non-nil, is the value the sentence is worded from, which is how the pseudo-locale
// gate (kit/locale/providers/pseudo) renders the shipped graph with every reached
// word marked. It is the same sentence with one of its own words replaced — not a
// second composition — and no production caller passes one: main.go, start.go,
// bootstrap.go and migrate.go all arrive through compose or composeReference.
func composeCopy(cfg config.Config, installed xtext.Catalog, served pkit.Catalogue) composition {
	return composeReferenceReading(cfg, pkit.Development, installed, served).composition
}

// composeReference is the sentence and the one resolution of it: the composition,
// its deployment, and the process's reading of the services it resolved.
func composeReference(cfg config.Config, env pkit.Environment) reference {
	return composeReferenceReading(cfg, env, catalogues(), nil)
}

// composeReferenceReading resolves the sentence over a named catalogue and, optionally,
// the value it is served in. See composeCopy for what that second value is for and who
// may name it.
func composeReferenceReading(cfg config.Config, env pkit.Environment, installed xtext.Catalog, served pkit.Catalogue) reference {
	if served == nil {
		served = installed
	}
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
	a, ask, shell, provision, grants, face, _ := sentence(cfg, served)
	p, err := a.Plan(once)
	if err != nil {
		// A composition this file wrote that does not resolve is a defect in this
		// build, and a defect in a binary is announced by not starting.
		panic("platformkit: " + err.Error())
	}
	c := composition{
		planned: p, modules: p.Modules(), options: p.Options(), messages: served,
		tenantList: value[tenantcontracts.Lister](p), hostLookup: value[tenantcontracts.HostLookup](p),
		oidcSettings: value[tenantcontracts.OIDCReader](p), samlSettings: value[tenantcontracts.SAMLReader](p),
		bootstrap: value[tenantcontracts.Bootstrapper](p), locale: value[tenantcontracts.LocaleSetter](p),
		provisioner: value[usercontracts.SystemProvisioner](p), addresses: value[usercontracts.AddressLookup](p),
		seedUsers: value[usercontracts.SeedWriter](p), permissions: value[authcontracts.PermissionReader](p),
		roleRepair: value[authcontracts.RoleRepairer](p), notify: value[notificationcontracts.Service](p),
		files:    value[filecontracts.Service](p),
		plans:    value[httpx.Entitler](p),
		tasks:    value[taskcontracts.Service](p),
		contents: value[contentcontracts.Service](p),
		sites:    value[sitecontracts.Service](p),
		access:   ask,
		// The words a refusal is allowed to use: the label of the grant that
		// gates role management, read off the manifest that defines it.
		granter:  refusalGrant(p.Skin()),
		passkeys: auth.NewPasskeyDoor(),
		links:    value[webcontracts.Links](p),
		signin:   value[admincontracts.Signin](p),
		// Whether a person can be promised a mail is the carrier's own answer, read
		// off the plan — the one reading the API's doors act on, so the page and the
		// door cannot disagree about the same installation. A composition with no
		// notification module has no deliverable mail either, and an absent answer is
		// read as false rather than through `value`, which panics.
		recoveryMail: mailDelivers(p),
		connection:   face,
	}
	// The mailer is read without the panic every other value gets, because nil is
	// one of the three answers this deployment can give for it: an installation
	// that names no mail server and does not ask for the in-process sink composes
	// no transport (modules/notification's "none"), which is the state a
	// mail-less production deployment boots on purpose. Every reader of this
	// field already treats nil as "nothing sends here", which is what makes the
	// module's own refusal and this line the same fact.
	c.mail, _ = pkit.Value[notificationcontracts.Mailer](p)
	// Whether a password door stands here at all is read off the plan the sentence
	// resolved and not written as a constant: a composition that took auth out
	// would otherwise keep advertising a door it stopped mounting, which is the
	// same lie the ask above is built to avoid.
	c.passwordDoor = value[pkit.Authenticator](p) != nil
	*shell = faultShell(c.messages, c.granter)
	// The seed's tenant-creation hook, filled from the plan the sentence resolved
	// to. It is another of the values this file fills after the resolution, beside
	// the shell above, the face below and the reach the access module fills, and it
	// is the one that cannot be a declared need: the hook is contributed to the
	// tenant module, and the owners it writes through — task's lifecycle service,
	// the auth service — are built after the tenant module needs them, so a module
	// that declared both is the cycle pkit names (`seed → task → tenant → seed`).
	// See seeding's comment in seed.go. Until this line the hook has no owners, and
	// it says so rather than writing a half-filled tenant's content.
	*provision = seedProvisioner{
		users: c.seedUsers, contents: c.contents, sites: c.sites, tasks: c.tasks,
		files: c.files, auth: c.permissions, demoPassword: cfg.Demo.Password,
	}
	// The roles hook's catalogue, filled from the same resolved plan for the same
	// reason seeding gives: the hook is contributed while the sentence is written
	// and the modules it asks about are only known once it is resolved. Until this
	// line the hook seeds nothing, and the initial roles this application names make
	// that a refusal rather than a thin tenant.
	*grants = lateCatalogue{get: c.catalogue}
	// The face, filled from the composition the plan answered. The mount was
	// written into the sentence with an empty holder beside it, because a mount is
	// part of the sentence and the answers are not.
	*face = connectionFace{describe: c.describe}
	return reference{app: a, once: once, composition: c}
}

// sentences is the one statement of this application's composition, and the six
// values its own chrome reads: the reach an ask has here, which the access module
// fills once the services it joins exist; the shell of the ask's two pages, drawn
// from the skin the resolved composition answered with; the seed's tenant-creation
// hook, the permission catalogue the roles hook seeds from, the workspace's face
// and the counting door's face — the last four filled by composeReference from the
// resolved plan, for the reason seeding gives. Every caller in this binary that
// boots the reference application goes through it: `run` and `start` hand the whole
// thing to pkit, and appOptions is what a test boots it with when it wants the
// handler rather than a process. There is one of it because a second statement of a
// composition is a second composition, and the two drift.
//
// `served` is the copy that one sentence is worded from: for every caller in this
// binary, the catalogue `catalogues` loads.
func sentence(cfg config.Config, served pkit.Catalogue, without ...string) (*pkit.App, *accessReach, *page.Shell, *seedProvisioner, *lateCatalogue, *connectionFace, *summaryFace) {
	ask, shell, provision, grants, face, capability := &accessReach{}, &page.Shell{}, &seedProvisioner{}, &lateCatalogue{}, &connectionFace{}, &keptSurface{}
	counts := &summaryFace{}
	a := pkit.NewApp("platformkit").Use(
		omitted([]*pkit.Module{
			product(cfg, grants),
			auth.Grants,
			user.Module,
			tenant.Module,
			notification.Module,
			// The chrome the module above draws its own list in, named beside it: the
			// screen is notification's and the shell is this product's.
			workspace(),
			auth.Module,
			auth.EmailRegistration,
			// The door a tenant's identity provider provisions through, which needs
			// people to write and roles to name, and so comes after both.
			scimProvisioning,
			// The media library's chrome, immediately before the module that mounts
			// the page: the authorizer its controls ask is the auth service above it.
			media(),
			file.Module,
			history.Module,
			task.Module,
			// A person's own copies of the records they may read: this product's
			// answer to "who is asking" and "which of our records are worth keeping",
			// then the capability that keeps them, then the screens they are kept
			// from. The three are named together because the first and the last are
			// nothing without the middle one, and the order is the order they are
			// built in: the service is made out of the authority read and the pairs
			// on the table before it, and a screen reads the service after it.
			saving(),
			saved.Module,
			keeping(shell, capability),
			billing.Module,
			// The translations of the module below: one table, no routes and no
			// permissions of its own. It is named before content because content
			// needs the port it provides — a page store whose title and body
			// declare themselves translatable is built after the module that
			// holds the other languages.
			translation.Module,
			content.Module,
			site.Module,
			web.Module,
			audit.Module,
			// Outbound webhooks: the machine's half of the same outbox the trail
			// reads. It needs the tenant lister and this file's own catalogue of
			// event names, and it is placed here because the events it can be asked
			// for are everything above it has already declared.
			webhook.Module,
			seeding(provision),
			access(ask),
			change.Module,
			subject.Module,
			admin.Module,
		}, without)...).
		Theme(design.Default()).
		Languages(served).
		Home(pinnedHome).
		ErrorPage(func(s pkit.Skin) httpx.Fault { return faultPage(s.Copy, refusalGrant(s)) }).
		AskForAccess(ask, func(router *httpx.Router) { page.MountAccess(router, *shell) }).
		Roles(startingRoles()...).
		WorkspaceCatalog(workspaceFace(workspaceCatalog(capability), face.mount, counts.mount))
	return a, ask, shell, provision, grants, face, counts
}

// sentences is that sentence in the copy this installation ships, for every caller
// with no opinion about the words — including the cases that take a module out of
// the list to ask what the composition resolves to without its provider.
func sentences(cfg config.Config, without ...string) (*pkit.App, *accessReach, *page.Shell, *seedProvisioner, *lateCatalogue, *connectionFace, *summaryFace) {
	return sentence(cfg, catalogues(), without...)
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

// mailDelivers is whether this installation's sender attempts a message, which is
// the answer the connection screen shows and the auth doors act on. A composition
// that provides no answer mails nothing, so absence reads as unavailable.
func mailDelivers(p *pkit.Planned) bool {
	delivery, ok := pkit.Value[notificationcontracts.MailDelivery](p)
	return ok && delivery != nil && delivery.Available()
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

// describe is the same resolution the composition file states in sentences,
// stated as values: the modules in the order they are built, who supplies whose
// need, what Choose settled, which implementation this environment picked, the
// configuration sections each module read and the roles the app recorded. The
// text is what a person reads and this is what a tool reads, both rendered from
// the one resolved plan, so the two cannot be settled about different
// compositions.
//
// It is names only — no configuration value, no deployment input, no credential
// and no tenant row — which is what lets it be committed beside the text file at
// all: a machine-readable file is a wider surface than a markdown one a reviewer
// reads, so it takes the narrower statement of what is in it.
func (r reference) describe() ([]byte, error) {
	doc, err := r.app.Describe(r.once)
	if err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("platformkit: the resolved composition cannot be encoded: %w", err)
	}
	return append(out, '\n'), nil
}

// startingRoles is this application's claim about who a tenant begins as, in
// pkit's words: Build refuses a grant no composed module defines and an operator
// grant a tenant's role may not hold, and Explain prints what survives. The
// initial roles themselves are still seeded by seedRoles, the tenant module's own
// creation hook — the engine has no seam for a starting role, and pkit's doc says
// as much.
//
// The list is roles.go's own literal, and the census test
// TestEveryRoleEveryComposedModuleDeclaresIsOneThisApplicationSeeds asks the
// resolved composition whether it names a role the list omits: what a new tenant
// is seeded with and what the modules declare cannot drift quietly apart.
func startingRoles() []pkit.Role {
	roles := make([]pkit.Role, 0, len(initialRoles))
	for _, r := range initialRoles {
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
	// SCIM names the media type its JSON is sent and answered in. A protocol's
	// type is registered here rather than by the module that serves it because the
	// API's format table is the kernel's, and which protocols an installation
	// speaks is this file's decision: a module that could register a type could
	// make every other door on the same API accept a body it never chose to read.
	opts.JSONMediaTypes = []string{scimcontracts.ProtocolMediaType}
	return opts
}
