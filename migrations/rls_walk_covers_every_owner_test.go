package migrations_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// TestTheTenantWalkReachesEveryModuleThatOwnsMigrations holds rls_test.go to
// the sentence above `everything`, which ARCHITECTURE.md repeats: the walk is
// about every table this repository creates. A module whose migrations are not
// in the list creates tables the walk never sees, so a policy it forgot would
// pass. Every directory modules/<owner>/migrations must have its owner in
// `everything`.
func TestTheTenantWalkReachesEveryModuleThatOwnsMigrations(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("..", "modules", "*", "migrations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatal("no modules/*/migrations directory found; the walk has nothing to compare")
	}
	var walked []string
	for _, src := range everything {
		walked = append(walked, src.Owner)
	}
	for _, dir := range dirs {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}
		owner := filepath.Base(filepath.Dir(dir))
		if !slices.Contains(walked, owner) {
			t.Errorf("modules/%s owns migrations, but rls_test.go's `everything` (%v) does not migrate them, so no table it creates is checked for its tenant policy", owner, walked)
		}
	}
}
