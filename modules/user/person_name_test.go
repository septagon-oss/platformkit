package user

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// TestEveryPersonIsNamedByAFieldTheyAlwaysHave: the catalogue's primaryField is
// what a catalogue reader titles a person by, and the native shell has no
// fallback of its own, so it names a field every person row carries. An invited
// person and a bootstrapped administrator have an address and no display name.
func TestEveryPersonIsNamedByAFieldTheyAlwaysHave(t *testing.T) {
	named := spec.Present.PrimaryField
	for _, f := range entity.Fields[*contracts.User]() {
		if f.Name != named {
			continue
		}
		if !f.Required {
			t.Errorf("people are titled by %q, which a person may not have; a catalogue reader would call them untitled", named)
		}
		return
	}
	t.Fatalf("primaryField %q names no field of a person", named)
}
