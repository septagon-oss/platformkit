package cache_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/cache/cachetest"
)

// TestTheSuiteRepeatedUnderOneNameConformsEveryTime runs the conformance suite
// three times in one process under case names that collide across the runs, which
// is what -count does to a factory that keys its stores by name. The factory
// keeps the contract cachetest.Store states — two calls in one case share a
// store, and the entry is forgotten when the case that minted it ends — so every
// run must pass, and no store may outlive its case.
func TestTheSuiteRepeatedUnderOneNameConformsEveryTime(t *testing.T) {
	var named sync.Map // case name with the run's #NN removed -> cache.Backend
	repeated := func(t *testing.T) cache.Cache {
		t.Helper()
		key := withoutRepetition(t.Name())
		b, loaded := named.LoadOrStore(key, cache.MemoryBackend())
		if !loaded {
			t.Cleanup(func() { named.Delete(key) })
		}
		c, err := cache.New("pkit", b.(cache.Backend))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return c
	}
	for range 3 {
		t.Run("run", func(t *testing.T) { cachetest.Conformance(t, repeated, broken(t)) })
	}
	named.Range(func(k, _ any) bool {
		t.Errorf("the store minted for %q outlived the case that made it", k)
		return true
	})
}

// withoutRepetition removes the #NN Go appends to a repeated subtest name, so
// the three runs above ask for each case's store under one name.
func withoutRepetition(name string) string {
	parts := strings.Split(name, "/")
	for i, p := range parts {
		if j := strings.LastIndex(p, "#"); j >= 0 {
			parts[i] = p[:j]
		}
	}
	return strings.Join(parts, "/")
}
