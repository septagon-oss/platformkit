package filetest

import (
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

func storageRepeatedDeletion(t *testing.T, fresh func(*testing.T) StorageFixture) {
	store, key := fresh(t), contracts.Key(uuid.NewString())
	storageMissing(t, store.Storage, store.Scope, key)
	for range 2 {
		if err := store.Storage.Delete(t.Context(), store.Scope, key); err != nil {
			t.Fatalf("Delete missing key: %v", err)
		}
	}
	storagePut(t, store.Storage, store.Scope, key, "remove me")
	for range 2 {
		if err := store.Storage.Delete(t.Context(), store.Scope, key); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		storageMissing(t, store.Storage, store.Scope, key)
	}
}

func storageReaderFailure(t *testing.T, fresh func(*testing.T) StorageFixture) {
	store, key := fresh(t), contracts.Key(uuid.NewString())
	stream := &storageFailureReader{}
	if err := store.Storage.Put(t.Context(), store.Scope, key, stream, 128, contracts.Meta{}); err == nil {
		t.Fatal("Put reported success after the caller's stream failed")
	}
	if !stream.read {
		t.Fatal("Put failed before exercising the caller's stream")
	}
	// Storage does not promise atomic cleanup on every provider error. Its
	// retry-safe Delete must still allow the owner to remove a failed write.
	if err := store.Storage.Delete(t.Context(), store.Scope, key); err != nil {
		t.Fatalf("clean failed upload: %v", err)
	}
	storageMissing(t, store.Storage, store.Scope, key)
}
