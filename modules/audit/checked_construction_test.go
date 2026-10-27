package audit_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/audit"
)

// The trail with its defaults left alone: the retention job is declared with the
// year rather than with no job at all.
func TestNewCheckedBuildsFromItsRequiredDependenciesAlone(t *testing.T) {
	m, err := audit.NewChecked(audit.Deps{})
	if err != nil {
		t.Fatalf("NewChecked with an empty Deps: %v", err)
	}
	if m.Name != "audit" {
		t.Errorf("manifest name = %q, want audit", m.Name)
	}
	if len(m.Jobs) != 1 {
		t.Errorf("retention jobs = %d, want the one sweep", len(m.Jobs))
	}
}
