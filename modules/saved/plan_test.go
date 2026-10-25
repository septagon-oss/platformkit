package saved_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/modules/saved"
	"github.com/septagon-oss/platformkit/modules/saved/contracts"
	"github.com/septagon-oss/platformkit/modules/saved/contracts/savedtest"
	"github.com/septagon-oss/platformkit/pkit"
)

// desk is the fixture's authority owner. What it answers is not what these cases
// ask; that anybody answers the port at all is, because the plan's question comes
// before any command runs.
type desk struct{}

func (desk) Current(context.Context, db.Tx[db.Tenant], string) (string, error) {
	return "", contracts.ErrDenied
}

// authority is the composition's half of the two ports saved Needs: who is asking,
// and what its now is.
func authority(name string) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[contracts.Access](w, desk{})
		pkit.Put[contracts.Clock](w, func() time.Time { return time.Unix(1900000000, 0).UTC() })
		return module.Module{Name: name}, nil
	}, pkit.Provides[contracts.Access](), pkit.Provides[contracts.Clock]())
}

// mute is a provider that answers a port with nothing at all.
func mute(name string) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[contracts.Access](w, desk{})
		pkit.Put[contracts.Clock](w, contracts.Clock(nil))
		return module.Module{Name: name}, nil
	}, pkit.Provides[contracts.Access](), pkit.Provides[contracts.Clock]())
}

// contributing is a source module's half: the one pair it can capture, contributed
// to whichever module takes savables.
func contributing(name string, one contracts.Savable) *pkit.Module {
	return pkit.NewModule(name, func(w *pkit.Wiring) (module.Module, error) {
		pkit.Put[contracts.Savable](w, one)
		return module.Module{Name: name}, nil
	}, pkit.Contributes[contracts.Savable]())
}

// thing is one module's declaration of one savable pair, with a source behind it.
func thing(moduleName, kind string) contracts.Savable {
	return contracts.Savable{Module: moduleName, Kind: kind, Source: &savedtest.Source{}}
}

// taker reads back what the plan handed saved's module: the service another
// module's command would call, and the person's claim the erasure orchestrator
// collects. A contribution nobody takes is the same silence in the other
// direction, so this reads both.
func taker() *pkit.Module {
	return pkit.NewModule("library_fixture", func(w *pkit.Wiring) (module.Module, error) {
		if pkit.Get[contracts.Service](w) == nil {
			return module.Module{}, errors.New("no service reached the plan")
		}
		var mine personaldata.Part
		for _, part := range pkit.All[personaldata.Part](w) {
			if part.Module == "saved" {
				mine = part
			}
		}
		if mine.Data == nil {
			return module.Module{}, errors.New("no personal-data claim for saved")
		}
		return module.Module{Name: "library_fixture"}, nil
	}, pkit.Needs[contracts.Service](), pkit.Needs[[]personaldata.Part]())
}

func TestCompositionAnswersTheServiceAndThePersonsOwnClaim(t *testing.T) {
	plan := pkit.NewApp("saved-fixture").Use(
		authority("desk_fixture"), contributing("example", thing("example", "thing")), saved.Module, taker())
	if err := plan.Validate(pkit.Deployment{Environment: pkit.Development}); err != nil {
		t.Fatalf("a composition that names the port and one pair: %v", err)
	}
}

// TestCompositionComposesNothingYetAsAnEmptyLibrary is the absence the declaration
// promises: pairs arrive as contributions, so a product that composes no savable
// module still composes this one, with nothing to capture.
func TestCompositionComposesNothingYetAsAnEmptyLibrary(t *testing.T) {
	plan := pkit.NewApp("saved-fixture").Use(authority("desk_fixture"), saved.Module, taker())
	if err := plan.Validate(pkit.Deployment{Environment: pkit.Development}); err != nil {
		t.Fatalf("a composition with no savable pair: %v", err)
	}
}

// TestCompositionRefusesThePortsNobodyAnswers is the plan read at boot: a Need with
// no supplier is refused before a connection, a migration or a listener, and the
// refusal names the contracts that went unanswered. A port answered with a nil
// value is the same refusal by the service's own rule: an installation that named
// no clock keeps no copy.
func TestCompositionRefusesThePortsNobodyAnswers(t *testing.T) {
	err := pkit.NewApp("saved-fixture").Use(saved.Module).Validate(pkit.Deployment{Environment: pkit.Development})
	if err == nil || !strings.Contains(err.Error(), "Access") || !strings.Contains(err.Error(), "Clock") {
		t.Fatalf("a plan with nobody to answer who is asking, or what now is: %v", err)
	}
	err = pkit.NewApp("saved-fixture").Use(mute("desk_fixture"), saved.Module).
		Validate(pkit.Deployment{Environment: pkit.Development})
	if err == nil || !strings.Contains(err.Error(), "clock") {
		t.Fatalf("a plan whose provider answered the clock with nothing: %v", err)
	}
}

// TestCompositionRefusesADeclarationItCouldNotHonour asks the four shapes this
// service cannot serve, from two owners' contributions and saved's own decision:
// one pair named twice, which would make the source depend on the order modules
// were listed; a pair with nothing to ask; a pair whose name is no slug pair; and
// words no screen could print.
func TestCompositionRefusesADeclarationItCouldNotHonour(t *testing.T) {
	for _, tc := range []struct {
		named []contracts.Savable
		wants string
	}{
		{[]contracts.Savable{thing("example", "thing"), thing("example", "thing")}, "twice"},
		{[]contracts.Savable{{Module: "example", Kind: "thing"}, {Module: "example", Kind: "plan"}}, "no source"},
		{[]contracts.Savable{{Module: "Example", Kind: "thing", Source: &savedtest.Source{}}, thing("example", "plan")}, "slug"},
		{[]contracts.Savable{{Module: "example", Kind: "thing",
			Words:  contracts.Words{Save: strings.Repeat("word ", 60)},
			Source: &savedtest.Source{}}}, "word"},
	} {
		uses := []*pkit.Module{authority("desk_fixture"), saved.Module}
		for i, one := range tc.named {
			uses = append(uses, contributing([]string{"look_source", "plan_source"}[i], one))
		}
		err := pkit.NewApp("saved-fixture").Use(uses...).Validate(pkit.Deployment{Environment: pkit.Development})
		if err == nil || !strings.Contains(err.Error(), tc.wants) {
			t.Errorf("savables %+v: %v, want a refusal naming %q", tc.named, err, tc.wants)
		}
	}
}
