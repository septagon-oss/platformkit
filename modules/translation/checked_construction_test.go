package translation_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/translation"
)

// An installation whose operator named no provider: the suggest route still
// refuses and the button is still not drawn.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	svc, m, err := translation.NewChecked(translation.Deps{})
	if err != nil {
		t.Fatalf("NewChecked with an empty Deps: %v", err)
	}
	if svc == nil {
		t.Fatal("the translation service is nil")
	}
	if m.Name != "translation" {
		t.Errorf("manifest name = %q, want translation", m.Name)
	}
}
