package pkit

// A required read the plan has no supplier for stops the build at the call and
// is the module's problem, named required. The resolver refuses that plan at Use
// before any build runs, so the branch is reached here directly: a read that
// answered zero for a declared Needs would be a silent hole, and this is what
// keeps it from becoming one.

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/module"
)

type heldPort interface{ Hold() }

func TestARequiredReadWithNoSupplierFailsItsModule(t *testing.T) {
	m := NewModule("reader", func(w *Wiring) (module.Module, error) {
		Get[heldPort](w).Hold()
		return module.Module{Name: "reader"}, nil
	}, Needs[heldPort]())
	w := &Wiring{at: m, plan: &plan{from: map[edge]*Module{}}, values: map[*Module]map[any][]any{}}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("the required read reached the caller as a panic: %v", r)
		}
	}()
	_, failed, err := buildOne(m, w)
	if err != nil || failed == nil {
		t.Fatalf("buildOne answered err %v and failed %v, want the failed read", err, failed)
	}
	const sentence = "reader needs pkit.heldPort, and no module in the plan supplies it"
	if failed.method != "Build" || failed.module != "reader" || failed.contract != "pkit.heldPort" ||
		failed.requiredness != "required" || failed.err.Error() != sentence {
		t.Errorf("the failed read is %+v, want Build, reader, pkit.heldPort, required, saying %q", *failed, sentence)
	}
}
