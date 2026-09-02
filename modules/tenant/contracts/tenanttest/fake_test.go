package tenanttest_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
)

// TestFakeConforms runs the suite against the fake. This is what makes the fake
// worth having: a consumer that tests against it is testing against the same
// rules internal/service_test.go proves the real service keeps.
func TestFakeConforms(t *testing.T) {
	tenanttest.RunService(t, func(t *testing.T, run func(tenanttest.Fixture)) {
		fake := tenanttest.NewFake()
		run(tenanttest.Fixture{Ctx: t.Context(), Service: fake, Published: fake.Published})
	})
}

// TestTheCreateHookRunsInsideCreate: the mechanism the whole module hangs on —
// a module above this one is notified without this one importing it — is not
// something the interface can express, so it is checked here and again against
// the real service.
func TestTheCreateHookRunsInsideCreate(t *testing.T) {
	fake := tenanttest.NewFake()
	var seen []string
	fake.Hooks = []contracts.Hook{
		func(_ context.Context, _ db.Tx[db.System], created *contracts.Tenant) error {
			seen = append(seen, created.Slug)
			return nil
		},
	}
	if _, err := fake.Create(t.Context(), db.Tx[db.System]{}, contracts.NewTenant{
		Slug: "acme", Name: "Acme", Host: "acme.example.com",
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(seen) != 1 || seen[0] != "acme" {
		t.Errorf("the hook saw %v, want the tenant that was created", seen)
	}
}
