package pkit_test

// The reference application holds two values its modules fill after the module
// that reads them is built: the grant check the user module asks before a
// promotion, answered by auth, which is built after user; and the ask-for-access
// reach, made of services that exist only once their modules are built. Both are
// a pointer the app makes, hands to the earlier reader, and fills in a later
// module's build — and both are sentences pkit already composes, with no
// declaration of their own, before anything is opened.

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

// people stands for the user module's service; clearance for auth's grant check.
type people interface{ Count() int }

type clearance interface{ Cleared() bool }

// everyone counts the people a promotion may reach, and asks the holder at
// call time — the earlier module reading what the later one filled.
type everyone struct{ roles *promoter }

func (e everyone) Count() int {
	if e.roles.gate == nil || !e.roles.gate.Cleared() {
		return 0
	}
	return 1
}

type alwaysCleared struct{}

func (alwaysCleared) Cleared() bool { return true }

// promoter is the late-bound half: built empty, read by the earlier module,
// filled by the later one.
type promoter struct{ gate clearance }

// reach is an ask-for-access reach whose services are filled as their modules
// build.
type reach struct {
	people people
	gate   clearance
}

func (*reach) Recipients(context.Context, db.Tx[db.Tenant]) ([]uuid.UUID, error) { return nil, nil }

func (*reach) Tell(context.Context, db.Tx[db.Tenant], httpx.AccessNotice) error { return nil }

func TestALaterModuleFillsWhatAnEarlierOneReads(t *testing.T) {
	roles := &promoter{}
	ask := &reach{}

	users := pkit.NewModule("people", func(w *pkit.Wiring) (module.Module, error) {
		// the earlier module's service holds the holder, as user.Deps.Granting does
		pkit.Put[people](w, everyone{roles: roles})
		return module.Module{Name: "people"}, nil
	}, pkit.Provides[people]())
	auths := pkit.NewModule("clearance", func(w *pkit.Wiring) (module.Module, error) {
		ask.people = pkit.Get[people](w)
		roles.gate, ask.gate = alwaysCleared{}, alwaysCleared{}
		pkit.Put[clearance](w, alwaysCleared{})
		return module.Module{Name: "clearance"}, nil
	}, pkit.Needs[people](), pkit.Provides[clearance]())

	a := pkit.NewApp("collect").Use(auths, users, doors, desk).
		AskForAccess(ask, func(*httpx.Router) {})
	p, err := a.Plan(pkit.Deployment{Environment: pkit.Development})
	if err != nil {
		t.Fatalf("a composition with a late-bound holder was refused: %v", err)
	}
	if roles.gate == nil || ask.people == nil || ask.gate == nil {
		t.Error("the later module's build did not fill the holders before Plan answered")
	}
	if got, _ := pkit.Value[people](p); got == nil || got.Count() != 1 {
		t.Error("the earlier module's service does not answer through the holder the later module filled")
	}
	if p.Options().Access != httpx.AskForAccess(ask) {
		t.Error("the reach the app named is not the one the engine is handed")
	}
}
