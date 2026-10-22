package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestTheNewActionReadsInSentenceCase pins that the list's create action is
// "New <noun>" with a lower-case noun, like every other New… label.
func TestTheNewActionReadsInSentenceCase(t *testing.T) {
	t.Parallel()
	out := render(t, resource.List(note(), opts, nil, 0, 1, "", false).Body)
	if strings.Contains(out, "New Note") {
		t.Errorf("the create action is title-cased:\n%s", out)
	}
}
