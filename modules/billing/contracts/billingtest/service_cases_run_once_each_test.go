package billingtest_test

import (
	"strings"
	"testing"

	"github.com/septagon-oss/platformkit/modules/billing/contracts/billingtest"
)

// TestServiceCasesRunOnceEach runs the service suite against the fake and
// requires a non-empty set of cases, none of which carries the "#NN" suffix the
// testing package adds when two subtests share a name.
func TestServiceCasesRunOnceEach(t *testing.T) {
	ran := 0
	billingtest.RunService(t, func(t *testing.T, run func(billingtest.Fixture)) {
		ran++
		if strings.Contains(t.Name(), "#") {
			t.Fatalf("case %q shares its name with another case", t.Name())
		}
		fake := billingtest.NewFake()
		run(billingtest.Fixture{
			Ctx: t.Context(), Service: fake,
			Plan: fake.Put, Expire: fake.Expire, Reprice: fake.Reprice,
			Age: fake.Age, Published: fake.Published,
		})
	})
	if ran == 0 {
		t.Fatal("the suite ran no case")
	}
}
