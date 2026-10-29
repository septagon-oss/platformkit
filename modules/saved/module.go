package saved

import (
	"fmt"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/internal"
	"github.com/septagon-oss/platformkit/pkit"
)

// Deps is what this module cannot make for itself: the authority port, and the
// savable pairs the composition was built with.
//
// Which membership and which grant table answer "may this account save" is no
// fact this module owns, so Access arrives as one port the composition adapts to
// its own authority owner. A service with no port to ask refuses every command
// rather than guessing who is asking, which is the only safe answer for a copy
// that is private to one person.
type Deps struct {
	// Access rereads the acting person and their current grant inside each
	// command's own transaction, after any wait on a source.
	Access contracts.Access

	// Savables is the declaration set: every module that Contributes[contracts.Savable]
	// one pair. An empty set composes, and means nothing can be captured yet —
	// the library reads as empty and every save names the pair nobody declared.
	Savables []contracts.Savable

	// Clock is the instant a retained copy is stamped at, and so the order a
	// person's copies come back in. A composition that cannot say what its now is
	// has no business keeping a copy, so a nil Clock refuses the plan.
	Clock contracts.Clock
}

// Module is a person's retained copies as the resolver sees it, for an
// application that names it in Use: `Use(saved.Module)`.
//
// The pairs that can be saved arrive as contributions rather than as an argument,
// because which things are worth keeping is a fact about the modules a product
// composed and never about this one; a composition that contributes none gets the
// same service with nothing to capture, which Explain names rather than leaves
// silent.
//
// What is absent from the manifest below is a decision, not an omission: no
// Routes, no Nav and no generated screen, because a page belongs to the
// composition that has one, and the words a heart shows come from each Savable's
// own declaration.
var Module = pkit.NewModule("saved", wire,
	pkit.Needs[contracts.Access](),
	pkit.Needs[contracts.Clock](),
	pkit.Needs[[]contracts.Savable](),
	pkit.Provides[contracts.Service](),
	pkit.Contributes[personaldata.Part](),
)

// wire resolves the declaration set, refuses a set the service could not honour,
// and hands out the one service the plan builds.
func wire(w *pkit.Wiring) (module.Module, error) {
	savables := pkit.All[contracts.Savable](w)
	if err := internal.ValidDeclarations(savables); err != nil {
		return module.Module{}, fmt.Errorf("saved: %w", err)
	}
	stamp := pkit.Get[contracts.Clock](w)
	if stamp == nil {
		return module.Module{}, fmt.Errorf("%w: saved names no clock to stamp a capture with", contracts.ErrInvalid)
	}
	// The three ports this module cannot make for itself are described once, here,
	// and read out of that description rather than gathered twice.
	deps := Deps{Access: pkit.Get[contracts.Access](w), Savables: savables, Clock: stamp}
	svc := internal.NewService(deps.Access, deps.Savables)
	svc.Clock = deps.Clock
	pkit.Put[contracts.Service](w, svc)
	pkit.Put[personaldata.Part](w, internal.PersonalData())
	return manifest(), nil
}

// permissions are the two grants the service asks for, one per kind of call. They
// are declared here rather than in contracts/ because a label is the manifest's
// business: it is the words a refusal and the administration screen show.
var permissions = []module.Permission{
	{Key: contracts.PermissionRead, Label: "Read your own saved copies"},
	{Key: contracts.PermissionWrite, Label: "Save and remove your own copies"},
}

// emitted are the two events, each with the payload it carries. The payload is
// not decoration: the outbox refuses a body that is not one, and the composition's
// AsyncAPI document is written from it.
var emitted = []events.Declared{
	events.Declare[contracts.Changed](contracts.EventSaved),
	events.Declare[contracts.Changed](contracts.EventUnsaved),
}

// manifest describes this capability to the kernel. It takes no argument because
// nothing in it depends on the composition: the schema, the grants, the events and
// the personal-data claim are the same whichever pairs were contributed.
func manifest() module.Module {
	return module.Module{
		Name:        "saved",
		Migrations:  Migrations.Files,
		Permissions: permissions,
		Declared:    emitted,
		// One copy per person per source. person_id is the keeper, whose own rows
		// an erasure takes outright; title and values are the copy's words, and
		// they arrive verbatim from another module's row, so they are the two text
		// columns an identifier may sit in and the erasure scrubs them for whoever
		// it names over every copy of its tenant, not only the subject's own.
		// picture_file_id names a file, which is a row of another owner and a fact
		// about a thing. No other column of this table holds prose: source_module
		// and source_kind are refused unless they are slugs, locale is a language
		// tag, source_version and source_key are the source's own address — which
		// rewriting would take away, since the unique index that makes a second
		// save the same copy reads them — and the composed-picture columns hold
		// only the selections a declared template allows.
		PersonalData: personaldata.Implements(personaldata.Table{
			Name: "saved_items", Person: "person_id",
			Text:      []string{"title", "values"},
			NotPerson: []string{"picture_file_id"},
		}),
		// Written out so the absence is a decision: saved answers to no event and
		// keeps no clock of its own, so it subscribes to nothing and sweeps nothing.
		Subscriptions: nil,
		Jobs:          nil,
	}
}
