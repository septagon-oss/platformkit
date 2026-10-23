package filetest

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// StorageFixture is a fresh, empty storage scope: one store, one tenant, and the
// scope that tenant's bytes live under. It carries no capability flags: an
// implementation that could refuse the length a stream does not declare would be
// refusing this module's own upload route, which declares none, so the question
// has no second answer left to record.
type StorageFixture struct {
	Storage  contracts.Storage
	Scope    contracts.Scope
	TenantID uuid.UUID
}

// RunStorage checks the byte-storage contract without a database or service.
// Every factory call must return an independent scope, including calls made by
// the same subtest. Reconciler checks run only when that optional contract
// exists.
//
// The scope cases at the top are the ones this suite exists for. They are here
// and not beside each adapter because one signature change is worth more than
// one test per implementation: whatever implements this port inherits the case
// that a second tenant's key opens nothing, and an adapter that gets the prefix
// wrong fails a test it never had to be told about.
func RunStorage(t *testing.T, fresh func(*testing.T) StorageFixture) {
	t.Helper()
	t.Run("a scope names one tenant's prefix and no other tenant's bytes", func(t *testing.T) { storageTenantIsolation(t, fresh) })
	t.Run("a scope with no tenant is refused and writes nothing", func(t *testing.T) { storageRequiresTenant(t, fresh) })
	t.Run("a key this module could not mint is refused before any byte is read", func(t *testing.T) { storageValidatesKeys(t, fresh) })
	t.Run("non-seekable streams and empty blobs", func(t *testing.T) { storageStreams(t, fresh) })
	t.Run("a body that declares no length arrives, and collides", func(t *testing.T) { storageUnknownLength(t, fresh) })
	t.Run("collisions preserve the original bytes", func(t *testing.T) { storageCollisions(t, fresh) })
	t.Run("concurrent creation has one complete winner", func(t *testing.T) { storageConcurrentCreation(t, fresh) })
	t.Run("missing blobs and repeated deletion", func(t *testing.T) { storageRepeatedDeletion(t, fresh) })
	t.Run("reader failure cannot become upload success", func(t *testing.T) { storageReaderFailure(t, fresh) })
	t.Run("independent stores, and a listing that honours its cutoff and its scopes", func(t *testing.T) { storageScopedReconciliation(t, fresh) })
}

func storagePut(t *testing.T, store contracts.Storage, scope contracts.Scope, key contracts.Key, body string) {
	t.Helper()
	stream := struct{ io.Reader }{strings.NewReader(body)}
	if err := store.Put(t.Context(), scope, key, stream, int64(len(body)), contracts.Meta{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func storageRead(t *testing.T, store contracts.Storage, scope contracts.Scope, key contracts.Key) string {
	t.Helper()
	body, err := store.Get(t.Context(), scope, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if body == nil {
		t.Fatal("Get returned no reader and no error")
	}
	data, readErr := io.ReadAll(body)
	if err := errors.Join(readErr, body.Close()); err != nil {
		t.Fatalf("read/close blob: %v", err)
	}
	return string(data)
}

func storageMissing(t *testing.T, store contracts.Storage, scope contracts.Scope, key contracts.Key) {
	t.Helper()
	body, err := store.Get(t.Context(), scope, key)
	if body != nil {
		_ = body.Close()
	}
	if !errors.Is(err, contracts.ErrNoBlob) {
		t.Fatalf("Get missing blob: %v, want ErrNoBlob", err)
	}
}

type storageFailureReader struct{ read bool }

func (r *storageFailureReader) Read(p []byte) (int, error) {
	r.read = true
	return copy(p, "incomplete"), errors.New("fixture source stream failed")
}
