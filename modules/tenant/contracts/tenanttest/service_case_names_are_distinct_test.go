package tenanttest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/kit/trace"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
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
			Language:        fake,
			Operator:        operator.ID,
			Published:       fake.Published,
			PublishedScopes: fake.PublishedScopes,
			PublishedTraces: fake.PublishedTraces,
			// The tenant's own language command needs a committed world to be
			// inside and an answer at the grant it rechecks; the suite refuses to
			// run those cases against a harness that wired neither.
			LocaleDoor: fake.AllowSetOwnLocale,
			OwnWorld: func(seed func(context.Context, db.Tx[db.System]) (*contracts.Tenant, error), body func(tenanttest.Own) error) error {
				created, err := seed(t.Context(), db.Tx[db.System]{})
				if err != nil {
					return err
				}
				return body(tenanttest.Own{
					Ctx: tenancy.WithTenant(t.Context(), created.Tenancy()), Tx: db.Tx[db.Tenant]{},
					Published: fake.Published,
				})
			},
		})
	})
	if ran == 0 {
		t.Fatal("the suite ran no case")
	}
}
