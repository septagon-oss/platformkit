package app

import (
	"fmt"
	"strings"

	"github.com/septagon-oss/platformkit/kit/screen"
)

// validateTours asks every tour the composition's modules declares what it asks
// of this composition: that the views and controls its steps point at were
// published, that a control reports the interaction a step waits for, and that an
// operation it waits a completion on is a mounted write behind that control. It
// runs after the screens are answered, so a step whose view was never published
// reads as the screen's own refusal and not twice.
func (a *App) validateTours() error {
	c := screenContract{views: a.screenInventory, ops: mountedOperations(a.screenInventory)}
	var bad []string
	for _, m := range a.mods {
		for _, t := range m.Tours {
			bad = append(bad, t.CompositionProblems(c)...)
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("app: invalid tours:\n  %s", strings.Join(bad, "\n  "))
}

func mountedOperations(inventory screen.Inventory) map[string]screen.Operation {
	ops := map[string]screen.Operation{}
	for _, op := range inventory.Snapshot().Operations {
		ops[op.ID] = op
	}
	return ops
}

// screenContract answers the screen contract as a tour may ask it. The inventory is
// the frozen composition and the operations are the ones its snapshot carries, so
// a tour reads the same names a generated reference or a phone pin reads.
type screenContract struct {
	views screen.Inventory
	ops   map[string]screen.Operation
}

func (p screenContract) View(ref screen.ViewRef) (screen.View, error) { return p.views.View(ref) }

func (p screenContract) Anchor(ref screen.AnchorRef) (screen.Anchor, error) {
	return p.views.Anchor(ref)
}

func (p screenContract) Operation(id string) (screen.Operation, bool) {
	op, ok := p.ops[id]
	return op, ok
}
