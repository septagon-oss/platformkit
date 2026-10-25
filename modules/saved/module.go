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
}

// Module is a person's retained copies as the resolver sees it, for an
// application that names it in Use: `Use(saved.Module)`.
//
// The pairs that can be saved arrive as contributions rather than as an argument,
// because which things are worth keeping is a fact about the modules a product
// composed and never about this one: a look, a plan and a place to stay are three
// captures written by three owners, and saved's service holds no switch among them.
// A composition that contributes none gets the same service with nothing to
// capture, which Explain names rather than leaves silent.
//
// What is absent from the manifest below is a decision, not an omission: no
// Routes, no Nav and no generated screen, because the heart on a savable detail
// and the saved grid are mounted by a composition that has a page to mount them
// on, and the words they show come out of each Savable's own declaration. What is
// here is the capability — the schema, the two grants, the two events and the
// person's claim on their own rows.
var Module = pkit.NewModule("saved", wire,
	pkit.Needs[contracts.Access](),
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
	svc := internal.NewService(pkit.Get[contracts.Access](w), savables)
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
		// One copy per person per source, and the whole row goes when they ask to
		// be forgotten: nothing here outlives the person's own hold on it, so no
		// column is filed as text an identifier may sit in. picture_file_id names
		// a file, which is a row of another owner and a fact about a thing.
		PersonalData: personaldata.Implements(personaldata.Table{
			Name: "saved_items", Person: "person_id", NotPerson: []string{"picture_file_id"},
		}),
		// Written out so the absence is a decision: saved answers to no event and
		// keeps no clock of its own, so it subscribes to nothing and sweeps nothing.
		Subscriptions: nil,
		Jobs:          nil,
	}
}
