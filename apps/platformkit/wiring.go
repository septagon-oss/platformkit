package main

// wiring.go is the composition's dependency edges in the resolver's words.
//
// compose builds every service by hand — that graph stays a list somebody wrote
// down — and this file says, for each module it builds, which of those hand-offs
// is a *contract* dependency rather than a product value. `pkit` reads the answer
// three ways: it refuses a module whose provider is not composed, it places each
// module after the one that supplies it, and Explain prints the edge, so the
// committed COMPOSITION files say who provides what to whom instead of saying
// that nobody needs anybody.
//
// The edges here are the ones that exist at construction: web.Deps takes the site
// service, so web declares Needs[sitecontracts.Service]. A hand-off that is not a
// contract is deliberately absent — the mailer, the theme, the pinned addresses
// and the policy are this product's values, not another module's service — and so
// is the one edge that is late-bound rather than built: the user module's grant
// check, answered by the auth service a few lines after user is built (see
// roleGranter). Declaring that as a need would ask the resolver to build user
// after the module that needs user, which is a cycle and a lie at once.

import (
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

// wire is one composed module as the resolver sees it: the contracts it cannot be
// built without, and the services it hands out when it builds.
type wire struct {
	// known says compose wrote this module's edges. manifestOf sets it when it
	// uses them and TestEveryModuleTheCompositionNamesItsEdges reads it back: a
	// module composed with no stated edge would otherwise pass as a module that
	// needs nobody, which is the silence this type exists to prevent.
	known bool
	decls []pkit.Declaration
	puts  func(*pkit.Wiring)
}

// manifestsOf hands every module this product composes to the resolver, in the
// order modules.go builds them — which is the order that decides who generates
// screens for whom (see the comments there: the shell is last for a reason) — and
// with the edges compose records for it.
//
// Each module's dependencies stay where they are constructed, in compose, and not
// in a builder pkit would run blind: user.Deps takes the granter that asks the
// auth service a question auth is built to answer, and that graph is a list
// somebody wrote down rather than a discovery mechanism. What moves here is where
// the *composition* is stated, which is what Explain, Validate, the ordering and
// the ports above all read.
func manifestsOf(c composition) []*pkit.Module {
	uses := make([]*pkit.Module, 0, len(c.modules))
	for _, m := range c.modules {
		uses = append(uses, c.manifestOf(m))
	}
	return uses
}

// manifestOf is one module of the composition, with its declared edges. A module
// compose builds that the edge table does not know is a composition whose
// dependencies nobody wrote down, and TestEveryModuleTheCompositionBuildsNames
// ItsEdges is what keeps that from happening quietly.
func (c composition) manifestOf(m module.Module) *pkit.Module {
	w := c.wires[m.Name]
	w.known = true
	c.wires[m.Name] = w
	return pkit.NewModule(m.Name, func(built *pkit.Wiring) (module.Module, error) {
		if w.puts != nil {
			w.puts(built)
		}
		return m, nil
	}, w.decls...)
}
