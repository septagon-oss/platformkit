package cache_test

import (
	"testing"

	"github.com/septagon-oss/platformkit/kit/cache/cachetest"
)

// TestTheInProcessFactoryForgetsEveryStoreItsCasesMade runs the suite over the
// in-process factory inside one case and holds that the factory's map holds no
// more stores afterwards than it did before: each store leaves the map when the
// case that minted it ends, as cachetest.Store requires, so no store outlives
// the case it belongs to.
func TestTheInProcessFactoryForgetsEveryStoreItsCasesMade(t *testing.T) {
	before := heldStores()
	t.Run("suite", func(t *testing.T) { cachetest.Conformance(t, memory(t), broken(t)) })
	if after := heldStores(); after != before {
		t.Errorf("the in-process factory holds %d stores after the suite and %d before it: a store outlived the case that made it", after, before)
	}
}

// heldStores counts the stores the in-process factory still answers for.
func heldStores() int {
	n := 0
	stores.Range(func(_, _ any) bool { n++; return true })
	return n
}
