package pkit_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/module"
	"github.com/septagon-oss/platformkit/pkit"
)

func TestExplainRefusesConflictingPayloadsInEitherOrder(t *testing.T) {
	number := events.Declare[postedNumber]("ledger.posted")
	text := events.Declare[postedText]("ledger.posted")
	for _, declarations := range [][]events.Declared{{number, text}, {text, number}} {
		ledger := pkit.NewModule("ledger", func(*pkit.Wiring) (module.Module, error) {
			return module.Module{Name: "ledger", Declared: declarations}, nil
		})
		document, err := pkit.NewApp("collect").Use(ledger).Explain(dev)
		if err == nil || !strings.Contains(err.Error(), "ledger.posted") {
			t.Errorf("contradictory payloads must be refused by event name: %v", err)
		}
		if document != "" {
			t.Errorf("refused composition returned a document: %s", document)
		}
	}

	// A different Go type for the same JSON document is still one promise.
	type sameNumber struct {
		Number int64 `json:"number"`
	}
	ledger := pkit.NewModule("ledger", func(*pkit.Wiring) (module.Module, error) {
		return module.Module{Name: "ledger", Declared: []events.Declared{
			number, events.Declare[sameNumber]("ledger.posted"),
		}}, nil
	})
	document, err := pkit.NewApp("collect").Use(ledger).Explain(dev)
	if err != nil || !strings.Contains(document, "ledger.posted") {
		t.Errorf("equivalent payloads must produce their composition document: %q, %v", document, err)
	}
}
