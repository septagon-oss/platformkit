package cache_test

import (
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache"
	"github.com/septagon-oss/platformkit/kit/cache/cachetest"
)

// storesByName is the factory shape that failed the nightly: one store per case
// name, never forgotten. Under -count a case's second run is handed the store its
// first run closed. It lives at package level because -count calls the test
// function again, and only a package variable outlives that call.
var storesByName sync.Map // t.Name() -> cache.Backend

// TestTheSuiteRefusesAStoreAnEarlierRunClosed runs the suite twice in a child
// process (-test.count=2, as `make flakes` does) over that factory, and holds that
// the suite refuses it in the case that says every case is handed a store of its
// own — the refusal has to come from the suite's own isolation case, not from
// whichever case happened to read the closed store first.
func TestTheSuiteRefusesAStoreAnEarlierRunClosed(t *testing.T) {
	const child = "PKIT_CACHE_SUITE_NAME_KEYED_CHILD"
	if os.Getenv(child) == "" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.CommandContext(t.Context(), executable,
			"-test.run=^TestTheSuiteRefusesAStoreAnEarlierRunClosed$", "-test.count=2", "-test.v", "-test.timeout=2m")
		cmd.Env = append(os.Environ(), child+"=1")
		out, err := cmd.CombinedOutput()
		const isolation = "TestTheSuiteRefusesAStoreAnEarlierRunClosed/every_case_is_handed_a_store_of_its_own"
		if !strings.Contains(string(out), "=== RUN   "+isolation) {
			t.Fatalf("the child never reached the suite's isolation case (err %v):\n%s", err, out)
		}
		if err == nil {
			t.Fatalf("the suite passed a factory that hands a case's second run the store its first run closed:\n%s", out)
		}
		if !strings.Contains(string(out), "--- FAIL: "+isolation) {
			t.Fatalf("the suite failed that factory, but not in its isolation case:\n%s", out)
		}
		return
	}
	byName := func(t *testing.T) cache.Cache {
		t.Helper()
		b, _ := storesByName.LoadOrStore(t.Name(), cache.MemoryBackend())
		c, err := cache.New("pkit", b.(cache.Backend))
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return c
	}
	cachetest.Conformance(t, byName, broken(t))
}
