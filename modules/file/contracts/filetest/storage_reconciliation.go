package filetest

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

func storageScopedReconciliation(t *testing.T, fresh func(*testing.T) StorageFixture) {
	first, second := fresh(t), fresh(t)
	key := contracts.Key(uuid.NewString())
	storagePut(t, first.Storage, first.Scope, key, "first scope")
	storageMissing(t, second.Storage, second.Scope, key)
	storagePut(t, second.Storage, second.Scope, key, "second scope")
	if got := storageRead(t, first.Storage, first.Scope, key); got != "first scope" {
		t.Fatal("another storage scope changed the bytes")
	}
	if err := second.Storage.Delete(t.Context(), second.Scope, key); err != nil {
		t.Fatal(err)
	}
	if got := storageRead(t, first.Storage, first.Scope, key); got != "first scope" {
		t.Fatal("deleting in another scope removed the bytes")
	}
	for i, store := range []StorageFixture{first, second} {
		reconciler, ok := store.Storage.(contracts.Reconciler)
		if !ok {
			continue
		}
		// A sweep asks with a system transaction; a test has none, and the
		// argument for the parameter is the type and not the value, so the
		// zero one is what a suite that is not running a database can hand
		// over. An implementation that used it to query would be caught by
		// the compile-time witness, not by this line.
		blobs, err := reconciler.Blobs(t.Context(), db.Tx[db.System]{}, time.Unix(0, 0))
		if err != nil || len(blobs) != 0 {
			t.Fatalf("new blobs listed before the historical cutoff: %v, %v", blobs, err)
		}
		blobs, err = reconciler.Blobs(t.Context(), db.Tx[db.System]{}, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("listing every object: %v", err)
		}
		var mine []contracts.Blob
		for _, b := range blobs {
			if b.TenantID == store.Scope.TenantID() || b.TenantID == uuid.Nil {
				mine = append(mine, b)
			}
		}
		// The second store's blob was deleted earlier in this case, so the
		// listing that follows a delete must say so.
		if i == 1 {
			if len(mine) != 0 {
				t.Fatalf("a deleted blob is still listed: %v", blobs)
			}
			continue
		}
		if len(mine) != 1 || mine[0].Key != key {
			t.Fatalf("this store should list one blob for its own scope, got %v", blobs)
		}
		if b := mine[0]; b.TenantID != uuid.Nil && b.TenantID != store.Scope.TenantID() {
			t.Fatalf("listing reported %s as this store's tenant", b.TenantID)
		}
		// Removing one tenant's blob leaves the other's: the sweep's worst
		// failure is a delete that crosses a prefix.
		before := len(blobs)
		if err := reconciler.RemoveBlob(t.Context(), db.Tx[db.System]{}, mine[0]); err != nil {
			t.Fatalf("remove a blob the listing returned: %v", err)
		}
		after, err := reconciler.Blobs(t.Context(), db.Tx[db.System]{}, time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("listing after a removal: %v", err)
		}
		if len(after) != before-1 {
			t.Fatalf("removing one blob left %d of %d listed", len(after), before)
		}
	}
}
