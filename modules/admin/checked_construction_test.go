package admin_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/admin"
)

// Nothing in admin's Deps is required: an empty Deps is a shell with less beside
// it, not a shell that was wired wrong.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	m, err := admin.NewChecked(admin.Deps{})
	if err != nil {
		t.Fatalf("NewChecked with an empty Deps: %v", err)
	}
	if m.Name != "admin" {
		t.Errorf("manifest name = %q, want admin", m.Name)
	}
	if m.Routes == nil {
		t.Error("the shell mounts no routes, so the operator has no address to open")
	}
}
