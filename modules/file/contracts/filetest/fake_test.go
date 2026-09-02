package filetest_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

// TestFakeConforms runs the suite against the fake and the in-memory storage.
// This is what makes both worth having: a consumer that tests against them is
// testing against the same rules internal/service_test.go proves the real
// service keeps against a real disk.
func TestFakeConforms(t *testing.T) {
	filetest.RunService(t, func(t *testing.T, run func(filetest.Fixture)) {
		store := filetest.NewMemory()
		fake := filetest.NewFake(store, filetest.Limit)
		run(filetest.Fixture{
			Ctx: t.Context(), Service: fake, Storage: store,
			Keys: store.Keys, Published: fake.Published,
		})
	})
}
