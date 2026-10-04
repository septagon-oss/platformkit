package tenanttest_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts"
	"github.com/septagon-oss/platformkit/modules/tenant/contracts/tenanttest"
)

// TestFakeConforms runs the suite against the fake. This is what makes the fake
// worth having: a consumer that tests against it is testing against the same
// rules internal/service_test.go proves the real service keeps.
func TestFakeConforms(t *testing.T) {
	tenanttest.RunService(t, func(t *testing.T, run func(tenanttest.Fixture)) {
		fake := tenanttest.NewFake()
		run(tenanttest.Fixture{
			Ctx: t.Context(), Service: fake, Published: fake.Published,
			LocaleDoor: fake.AllowSetOwnLocale,
			// The fake's "committed world" is the same map, and that is the point:
			// the case cannot tell them apart. What the case does have to see is a
			// context naming the tenant the seed created — the fake takes its subject
			// from the request, exactly as the service takes it from the row
			// row-level security leaves visible — and the same event list both
			// harnesses report, so the suite's filter decides both implementations.
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
