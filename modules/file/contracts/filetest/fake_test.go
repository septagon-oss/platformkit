package filetest_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

// fixtureTenant is the tenant the fake's context carries. The fake takes its
// scope the way the real Upload does — from the request's context — so the suite
// has to hand it one, and the case where that context carries none is the same
// refusal the real service answers with.
var fixtureTenant = tenancy.Tenant{ID: uuid.New(), Slug: "fixture", Name: "Fixture"}

// TestFakeConforms runs the suite against the fake and the in-memory storage.
// This is what makes both worth having: a consumer that tests against them is
// testing against the same rules internal/service_test.go proves the real
// service keeps against a real disk.
func TestFakeConforms(t *testing.T) {
	filetest.RunService(t, func(t *testing.T, run func(filetest.Fixture)) {
		store := filetest.NewMemory()
		fake := filetest.NewFake(store, filetest.Limit)
		run(filetest.Fixture{
			Ctx: tenancy.WithTenant(t.Context(), fixtureTenant), Service: fake, Storage: store,
			Keys: store.Keys, Published: fake.Published,
		})
	})
}
