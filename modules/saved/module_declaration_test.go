package saved_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/saved"
)

// Every module in this repository is a `pkit` declaration in module.go: the plan
// is what says a composition may ask for the service at all, and it is checked
// before anything opens. The schema, the contract and the adapter are already
// here, so the declaration is the only piece of `saved` an app cannot reach.
func TestSavedIsADeclaredModule(t *testing.T) {
	if got := saved.Module.Name(); got != "saved" {
		t.Errorf("the plan knows this module as %q, want %q", got, "saved")
	}
}
