// Package change is the module manifest: change control as a thing the kernel
// owns, rather than a pattern each product writes again and gets subtly wrong.
//
// The object is a proposal, and the module owns exactly four things about it: the
// digest of the exact bytes a verdict was made about, the state machine that says
// a decision is one way, the actor rule that a proposal's author cannot be its
// decider, and the apply that writes once, against the revision it was reviewed
// at, or not at all.
//
// What it deliberately does not own is which writes need it. There is no Spec here
// — a Spec is five routes on a collection, three of which write whatever the body
// says, and a proposal moves through four named commands with an actor rule each —
// and no flag read here either, because a capability that decided which of its
// host's operations were sensitive would be a capability with a product in it. The
// composition says which operations a gate refuses, and the module that owns a row
// says how to lock and save it. Those two are the ports in contracts/, and the list
// of subjects is written out by one author in the application's composition file.
package change

import (
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/modules/change/contracts"
	"github.com/septagon-oss/platformkit/modules/change/internal"
)

// Deps is what this module cannot make for itself. Subjects is the literal list of
// what may be applied here, in the order somebody wrote it down; Service is for a
// composition that shares one service with its other callers, and supplying it with
// Subjects configures the same thing twice, which is a panic rather than a tie
// broken by whichever field the constructor reads first.
type Deps struct {
	Service  contracts.Service
	Subjects []contracts.SubjectBinding
}

// permissions is what the manifest declares. kit/app checks every route's
// declaration against it at boot, so a route guarded by a permission that is not
// here fails startup instead of denying everyone forever.
var permissions = []module.Permission{
	{Key: contracts.PermissionChangeRead, Label: "read change proposals"},
	{Key: contracts.PermissionChangePropose, Label: "propose a change"},
	{Key: contracts.PermissionChangeDecide, Label: "decide and apply a proposed change"},
}

// NewService builds the commands over the subjects this installation applies.
// Passing the returned value through Deps.Service shares it with other modules.
func NewService(bindings []contracts.SubjectBinding) contracts.Service {
	return internal.NewService(bindings)
}

// Module is the manifest, and the service it is built on.
func Module(deps Deps) module.Module {
	svc := deps.Service
	if svc == nil {
		svc = NewService(deps.Subjects)
	} else if len(deps.Subjects) > 0 {
		panic("change: bind the subjects on the shared service or supply them to Module, not both")
	}
	return module.Module{
		Name:        "change",
		Migrations:  Migrations.Files,
		Adopts:      Migrations.Adopts,
		Permissions: permissions,
		Declared:    contracts.Events,
		// No Nav entry: this module has no screen of its own to go to. The list of
		// what is waiting for a decision is a screen, and it belongs to whoever
		// composes a review queue — the same reason there is no HTML in internal/.
		Nav: nil,
		// Written out so the absence is a decision: nothing about a proposal happens
		// because time passed, and a proposal never acts on another module's event —
		// the point is that a human decided.
		Jobs:          nil,
		Subscriptions: nil,
		Routes:        func(s httpx.Surfaces) { internal.RegisterRoutes(s, svc) },
	}
}
