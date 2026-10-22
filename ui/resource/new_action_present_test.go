package resource_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/ui/resource"
)

// TestTheListOffersTheNewActionInSentenceCase pins that the create action is
// rendered at all, as "New note", so its absence cannot pass the case check.
func TestTheListOffersTheNewActionInSentenceCase(t *testing.T) {
	t.Parallel()
	out := render(t, resource.List(note(), opts, []map[string]any{{"id": "1"}}, 1, 1, "", true).Body)
	if !strings.Contains(out, "New note") {
		t.Errorf("the create action is missing or not in sentence case:\n%s", out)
	}
}
