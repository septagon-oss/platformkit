package pkit_test

// The machine-readable refusal carries the same fields the CompositionError
// does — the module, the contract, the requiredness and the suppressed modules —
// under the composition document's existing keys, with no graph beside them.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestDescribeNamesTheFailedReadAndWhatItStopped(t *testing.T) {
	ran := false
	app := undeclaredReadApp(&ran)
	doc, out, err := described(t, app, dev)
	ce := compositionError(t, err)
	if doc.Resolved || len(doc.Modules) != 0 {
		t.Errorf("a refused composition reads resolved=%v with %d modules:\n%s", doc.Resolved, len(doc.Modules), out)
	}
	if !reflect.DeepEqual(doc.Problems, ce.Problems) || !reflect.DeepEqual(doc.Suppressed, ce.Suppressed) {
		t.Errorf("the document and the error disagree:\n%+v\n%+v", doc, ce)
	}
	compact := strings.Join(strings.Fields(out), "")
	for _, want := range []string{
		`"module":"reader"`, `"contract":"pkit_test.readPort"`, `"requiredness":"undeclared"`,
		`"suppressed":[{"module":"dependent","by":"reader"}]`,
	} {
		if !strings.Contains(compact, want) {
			t.Errorf("the document does not carry %s:\n%s", want, out)
		}
	}
	second, _ := json.Marshal(doc.Problems[1])
	if !strings.Contains(string(second), `"module":"other"`) || strings.Contains(string(second), "contract") || strings.Contains(string(second), "requiredness") {
		t.Errorf("a refusal that is not a failed read carries read keys: %s", second)
	}
}
