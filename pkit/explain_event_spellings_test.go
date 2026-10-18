package pkit_test

// A manifest may spell its event list two ways — `Events`, the bare names, and
// `Declared`, the same names with their payload types — and kit/module owns the
// rule that they are one list: Module.Emits says "a name given both ways is one
// event and appears once", and Validate, the coverage line and the AsyncAPI
// document all read that method rather than either field. Explain writes the
// composition file a client commits for each environment (decision 0074 rule 4),
// so that file has to agree with the document the same composition produces:
// an event named in both spellings appears once.

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestExplainNamesAnEventOnceHoweverTheModuleSpellsIt(t *testing.T) {
	billing := pkit.NewModule("billing", func(w *pkit.Wiring) (module.Module, error) {
		return module.Module{
			Name:   "billing",
			Events: []string{"billing.invoice_issued"},
			Declared: []events.Declared{
				{Name: "billing.invoice_issued", Payload: nil},
				{Name: "billing.invoice_paid"},
			},
		}, nil
	})

	txt, err := pkit.NewApp("collect").Use(billing).Explain(dev)
	if err != nil {
		t.Fatalf("the honest composition was refused: %v", err)
	}
	if n := strings.Count(txt, "billing.invoice_issued"); n != 1 {
		t.Errorf("the composition file names billing.invoice_issued %d times, so it disagrees with Module.Emits and the AsyncAPI document:\n%s", n, txt)
	}
	if !strings.Contains(txt, "billing.invoice_paid") {
		t.Errorf("the composition file dropped the event only Declared spells:\n%s", txt)
	}
}
