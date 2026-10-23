package tenanttest_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/trace"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
)

// TestServiceCaseNamesAreDistinct runs the service suite against the fake and
// requires a non-empty set of cases, none of which carries the "#NN" suffix the
// testing package adds when two subtests share a name: a split that reused a
// name would hide one case behind another.
func TestServiceCaseNamesAreDistinct(t *testing.T) {
	ran := 0
	tenanttest.RunService(t, func(t *testing.T, run func(tenanttest.Fixture)) {
		ran++
		if strings.Contains(t.Name(), "#") {
			t.Fatalf("case %q shares its name with another case", t.Name())
		}
		fake, operator, _ := tenanttest.Installed()
		run(tenanttest.Fixture{
			Ctx:             trace.With(t.Context(), trace.New()),
			Service:         fake,
			Operator:        operator.ID,
			Published:       fake.Published,
			PublishedScopes: fake.PublishedScopes,
			PublishedTraces: fake.PublishedTraces,
		})
	})
	if ran == 0 {
		t.Fatal("the suite ran no case")
	}
}
