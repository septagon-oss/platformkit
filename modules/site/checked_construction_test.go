package site_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/site"
)

// The settings door with no gate in front of it, which is what a composition
// that composed no proposal flow asked for.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	svc, m, err := site.NewChecked(site.Deps{})
	if err != nil {
		t.Fatalf("NewChecked with an empty Deps: %v", err)
	}
	if svc == nil {
		t.Fatal("the settings service is nil")
	}
	if m.Name != "site" {
		t.Errorf("manifest name = %q, want site", m.Name)
	}
}
