// Package cachetest is the one conformance suite every adapter of cache.Cache
// runs — the kernel's version of a module's contracts/<slug>test harness.
//
// Both adapters in this repository call it: the in-process store in kit/cache's
// own test, the Valkey provider in its package's. That is the reason the
// in-process adapter is not allowed to be a different cache — it runs the same
// cases for the same reasons, so a green run over it is evidence and not theatre.
// Every case says what a wrong adapter would do, so the suite cannot be passed by
// an implementation that quietly means something else.
package cachetest

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/cache"
)

// tenantA and tenantB are two customers. They are fixed bytes rather than fresh
// uuids so a key that lets one reach the other is reproducible from the failure
// message alone.
var (
	tenantA = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	tenantB = uuid.MustParse("22222222-2222-4222-8222-222222222222")
)

// Store returns a handle for one case. Calls with the same *testing.T share
// one store; different cases and repetitions own independent stores and cleanup.
// Closing a handle must not close a connection shared with other cases.
// The suite runs its explicit close case last.
type Store func(t *testing.T) cache.Cache

// Failing returns one handle onto a store that cannot be reached — no process
// listening, or one that stopped answering — and answers with an error within a
// bounded time rather than hanging.
type Failing func(t *testing.T) cache.Cache

// Conformance runs the adapter's cases.
func Conformance(t *testing.T, store Store, failing Failing) {
	t.Helper()

	t.Run("a miss is not an error", func(t *testing.T) { miss(t, store) })
	t.Run("a value is read back byte for byte", func(t *testing.T) { valueBytes(t, store) })
	t.Run("an entry with no lifetime is refused", func(t *testing.T) { requireLifetime(t, store) })
	t.Run("an entry expires", func(t *testing.T) { expires(t, store) })
	t.Run("two replicas share one entry", func(t *testing.T) { sharedEntry(t, store) })
	t.Run("what one replica deletes the other cannot read", func(t *testing.T) { sharedDelete(t, store) })
	t.Run("another tenant's entry is unreachable", func(t *testing.T) { tenantIsolation(t, store) })
	t.Run("a scope naming no tenant is refused", func(t *testing.T) { requireScope(t, store) })
	t.Run("a key name cannot forge another owner's prefix", func(t *testing.T) { keyPrefix(t, store) })
	t.Run("deleting a thousand keys is one invalidation", func(t *testing.T) { bulkDelete(t, store) })
	t.Run("the key spelling is one spelling", func(t *testing.T) { keySpelling(t) })
	t.Run("a move closes what was written before it", func(t *testing.T) { moveScope(t, store) })
	t.Run("an unreachable store is an error and not a panic", func(t *testing.T) { unreachableStore(t, store, failing) })
	t.Run("every case is handed a store of its own", func(t *testing.T) { caseIsolation(t, store) })
	t.Run("a close releases the store and is idempotent", func(t *testing.T) { closeStore(t, store) })
}
