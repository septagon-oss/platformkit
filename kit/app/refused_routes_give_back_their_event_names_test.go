package app

// The event shapes a composition declares are the one piece of process state a boot
// above the connection touches: New claims them on its last line, and a Runtime
// gives them back when it closes. A boot refused for its routes never becomes a
// Runtime, so the door that refuses is the door that gives the grip back — otherwise
// the composition that stands in the catalog is one that never started, and the
// corrected spelling of a name it got wrong is refused beside it. Both route doors
// are pinned here: the three dry registrations Declarations answers, and the fourth
// registration whose surface would serve, which is answered inside Start.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
)

// freeAgain reads the name the refused composition declared and asks whether this
// app answers it under any shape. Nothing does once the boot that declared it was
// given back, which is what lets the corrected composition claim it its own way.
func freeAgain(t *testing.T) {
	t.Helper()
	if err := events.CheckDeclared([]events.Declared{events.Declare[claimedNumber]("ledger.posted")}); err != nil {
		t.Errorf("a composition that never started still describes this app's event names: %v", err)
	}
}

func TestARouteRefusalGivesBackTheEventNamesItsCompositionDeclared(t *testing.T) {
	cfg, opts := compose(t)
	built := 0
	opts.Caches = sharedStore(&cfg, &built, nil)
	t.Run("dry registrations", func(t *testing.T) {
		a, err := New(t.Context(), cfg, []module.Module{mounting(2), declaringLedger()}, opts)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		err = a.Declarations()
		if err == nil || !RefusedBeforeEffects(err) {
			t.Fatalf("Declarations = %v, want the pre-effect refusal of a changed registration", err)
		}
		freeAgain(t)
	})
	t.Run("the registration whose surface would serve", func(t *testing.T) {
		a, err := New(t.Context(), cfg, []module.Module{mounting(4), declaringLedger()}, opts)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		rt, err := a.Start(t.Context())
		if rt != nil {
			_ = rt.Close()
			t.Fatal("Start handed back a Runtime whose last registration mounted elsewhere")
		}
		if err == nil {
			t.Fatal("Start accepted a composition whose fourth registration mounted elsewhere")
		}
		freeAgain(t)
	})
	if built != 0 {
		t.Errorf("both refusals built the store the deployment named %d times", built)
	}
}
