package content_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/content"
)

// A page with no store for an inline picture and no use ledger: both default to
// the answer that refuses the picture rather than the one that loses it.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	svc, m, err := content.NewChecked(content.Deps{})
	if err != nil {
		t.Fatalf("NewChecked with an empty Deps: %v", err)
	}
	if svc == nil {
		t.Fatal("the page service is nil")
	}
	if m.Name != "content" {
		t.Errorf("manifest name = %q, want content", m.Name)
	}
}
