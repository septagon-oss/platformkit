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
	"github.com/google/uuid"

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
	// Notify is the port a decision about a proposal is told through, and nil means this
	// installation tells nobody: the manifest keeps Subscriptions nil, which is the
	// absence written down rather than a silence.
	Notify contracts.Notifier
	// ProposalPage is the proposal page as this application serves it, so a notice can
	// name where to read the thing it is reporting. modules/change owns the proposal and
	// not the address it answers at.
	ProposalPage contracts.ProposalPage
	// Reviews is the queue and the decision page, with the shell they are drawn in. A
	// composition that wires nothing serves no queue, and the Nav entry below is not
	// there to lead nowhere, because the entry names the screen this field mounts.
	Reviews ReviewPages
}

// ReviewPages is the queue and the decision page as internal composes them, named here so the
// application that wires them names this module and not its internal package.
type ReviewPages = internal.Pages

// NewProposalPage is the notice's link for a composition that mounts the queue at this
// module's own address: the page path, with the proposal on the end. It exists so an
// application writes that address once instead of spelling the same concatenation twice.
func NewProposalPage(at string) contracts.ProposalPage {
	return func(id uuid.UUID) string { return at + "/" + id.String() }
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
func New(deps Deps) module.Module {
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
		// The queue this module serves, named by the permission that decides who sees the
		// link. The manifest used to say Nav: nil on the reasoning that a review queue is a
		// screen and a screen belongs to whoever composes one; the brief that asked for the
		// approval flow asked for the screen as well, and a kernel that ships a proposal
		// object with no way to read it leaves every product to write its own queue, which
		// is the one-client module this repository exists to prevent.
		Nav: []module.NavEntry{{
			Label: "Reviews", Screen: "change/proposals",
			Permission: contracts.PermissionChangeRead,
		}},
		// No periodic work: nothing about a proposal happens because time passed, and a
		// proposal never acts on another module's event — the point is that a human decided.
		// The subscriptions are the proposer's notices, and only when a composition asked
		// for them: Deps.Notify is nil otherwise and Tell answers nil for nil.
		Jobs:          nil,
		Subscriptions: internal.Tell(deps.Notify, deps.ProposalPage),
		Routes: func(s httpx.Surfaces) {
			internal.RegisterRoutes(s, svc)
			internal.MountReviews(s, deps.Reviews)
		},
	}
}
