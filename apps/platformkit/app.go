package main

// app.go is the reference application read as one sentence, and decision 0074's
// pilot: the same modules modules.go builds, named once, in the order the
// composition is built in, with the four ports the kernel cannot answer for
// itself put by the module that owns them and the chrome this product owns
// recorded by the app.
//
// `run` and `start` are the two commands that go through it, which is the
// difference between a sentence and a comment about one: what the image runs and
// what a laptop runs are composed here, and what they are composed from is
// `pkit.Server.Explain`'s text, committed beside this file as
// COMPOSITION.development.md and COMPOSITION.production.md.
//
// The sentence reads in the order the composition is built: modules first,
// because everything below is drawn from what they are; then the words, the
// colours and the doors, which are what this product adds to them.

import (
	"fmt"

	"github.com/septagon-oss/platformkit/design"
	"github.com/septagon-oss/platformkit/kit/app"
	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
	"github.com/septagon-oss/platformkit/ui/page"
)

// reference is the application, its deployment, and the services this process
// reads for itself — the tenant service bootstrap creates the first tenant
// through, the user service it invites the first administrator with. Those two
// values are the composition's outputs rather than its inputs, so they travel
// with it here. A command that needs the resolved composition and nothing else —
// the module list `bootstrap` migrates over, the providers a composition file
// names — reads them off r.plan() instead of a second list.
type reference struct {
	app  *pkit.App
	once pkit.Deployment
	composition
}

// composeReference is the sentence: the composition, its deployment, and the
// process's reading of the services it resolved.
func composeReference(cfg config.Config, env pkit.Environment) reference {
	c := compose(cfg)
	return reference{
		composition: c,
		once: pkit.Deployment{
			Environment: env,
			Config:      cfg,
			Transports:  transports(),
		},
		app: sentences(cfg, c),
	}
}

// sentences is the one statement of this application's composition. Everything
// that boots the reference application goes through it: `run` and `start`, which
// hand the whole thing to pkit, and appOptions, which is what a test in this
// repository boots the same composition with when it wants the handler rather
// than a process. There is one of it because a second statement of a
// composition is a second composition, and the two drift.
func sentences(cfg config.Config, c composition) *pkit.App {
	uses := append([]*pkit.Module{kernelOf(c)}, manifestsOf(c)...)
	return pkit.NewApp("platformkit").Use(uses...).
		Theme(design.Default()).
		Languages(c.messages).
		Home(pinnedHome).
		ErrorPage(func(pkit.Skin) httpx.Fault { return faultPage(c) }).
		AskForAccess(c.access, func(router *httpx.Router) { page.MountAccess(router, faultShell(c)) }).
		Roles(startingRoles()...).
		WorkspaceCatalog(workspaceCatalog())
}

// plan is the composition answered with no effects: every module built, every
// port answered, every recorded claim checked, nothing opened.
func (r reference) plan() *pkit.Planned {
	p, err := r.app.Plan(r.once)
	if err != nil {
		// A composition this file wrote that does not resolve is a defect in
		// this build, and a defect in a binary is announced by not starting.
		panic("platformkit: " + err.Error())
	}
	return p
}

// kernelOf is the composition's own module: the three ports every application
// must answer, plus the plan answer this product sells features against, put by
// the one file that knows which service answers each. The module names no
// permission, registers no route and claims no address: it is the wiring the
// kernel asks for and nothing else, which is why it is one module and not a
// field on the app.
//
// Before pkit, these four values were four fields of app.Options set at the one
// call to app.New, which meant the composition existed only at the moment a
// process started. Put as a contract, they are answers the resolver checks — one
// provider each, named in the composition file — before a connection is opened.
func kernelOf(c composition) *pkit.Module {
	return pkit.NewModule("composition", func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[httpx.TenantLoader](w, c.tenants)
		pkit.Put[httpx.Authorizer](w, c.auth)
		pkit.Put[pkit.Authenticator](w, c.auth.Authenticate)
		pkit.Put[httpx.Entitler](w, c.plans)
		return module.Module{Name: "composition"}, nil
	},
		pkit.Provides[httpx.TenantLoader](),
		pkit.Provides[httpx.Authorizer](),
		pkit.Provides[pkit.Authenticator](),
		pkit.Provides[httpx.Entitler]())
}

// startingRoles is this application's claim about who a tenant begins as, in
// pkit's words: Build refuses a grant no composed module defines and an operator
// grant a tenant's role may not hold, and Explain prints what survives. The
// personas themselves are still seeded by seedRoles, the tenant module's own
// creation hook — the engine has no seam for a starting role, and pkit's doc
// says as much.
func startingRoles() []pkit.Role {
	roles := make([]pkit.Role, 0, len(personas))
	for _, r := range personas {
		roles = append(roles, pkit.Role{Name: r.Name, Grants: append([]string(nil), r.Grants...)})
	}
	return roles
}

// server is the process's own half: one listener, the applications it hosts, and
// the tenant hosts it claims. `run` and `start` both arrive here, which is why
// the role, the installation's host and the transports are filled once.
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
