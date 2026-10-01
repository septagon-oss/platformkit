package filetest

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
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
	t.Run("a scope names one tenant's prefix and no other tenant's bytes", func(t *testing.T) {
		a, b := fresh(t), fresh(t)
		if a.Scope.TenantID() == b.Scope.TenantID() {
			t.Fatal("the fixture handed two subtests the same tenant")
		}
		key := contracts.Key(uuid.NewString())
		storagePut(t, a.Storage, a.Scope, key, "the first tenant's bytes")
		// A key is a UUID this module minted, so another tenant asking for it
		// is asking for something that does not exist in their own prefix — the
		// same answer as a key nobody ever minted, and never the first tenant's
		// bytes.
		storageMissing(t, b.Storage, b.Scope, key)
		if err := b.Storage.Delete(t.Context(), b.Scope, key); err != nil {
			t.Fatalf("deleting another tenant's key: %v", err)
		}
		if got := storageRead(t, a.Storage, a.Scope, key); got != "the first tenant's bytes" {
			t.Fatalf("another tenant's delete reached these bytes: %q", got)
		}
		// Two tenants may hold the same key, because a key is a UUID inside a
		// prefix and the prefix is not theirs to name.
		storagePut(t, b.Storage, b.Scope, key, "the second tenant's bytes")
		if got := storageRead(t, a.Storage, a.Scope, key); got != "the first tenant's bytes" {
			t.Fatal("one tenant's upload replaced another's at the same key")
		}
	})
	t.Run("a scope with no tenant is refused and writes nothing", func(t *testing.T) {
		fixture := fresh(t)
		key := contracts.Key(uuid.NewString())
		if err := fixture.Storage.Put(t.Context(), contracts.Scope{}, key, strings.NewReader("x"), 1, contracts.Meta{}); !errors.Is(err, db.ErrNoTenant) {
			t.Fatalf("Put with the zero scope: %v, want db.ErrNoTenant", err)
		}
		if _, err := fixture.Storage.Get(t.Context(), contracts.Scope{}, key); !errors.Is(err, db.ErrNoTenant) {
			t.Fatalf("Get with the zero scope: %v, want db.ErrNoTenant", err)
		}
		if err := fixture.Storage.Delete(t.Context(), contracts.Scope{}, key); !errors.Is(err, db.ErrNoTenant) {
			t.Fatalf("Delete with the zero scope: %v, want db.ErrNoTenant", err)
		}
		storageMissing(t, fixture.Storage, fixture.Scope, key)
	})
	t.Run("a key this module could not mint is refused before any byte is read", func(t *testing.T) {
		fixture := fresh(t)
		// The whole traversal argument in one list. A Key is a defined string
		// type, so Key("../../etc/passwd") compiles, and the only thing between
		// that literal and the filesystem is this check — which is why every
		// implementation repeats it and the suite feeds it to each of them.
		for _, bad := range []string{"../../etc/passwd", "..", "", "x", "UPPERCASE", "a/b", "63://e/x", strings.ToUpper(uuid.NewString())} {
			k := contracts.Key(bad)
			if err := fixture.Storage.Put(t.Context(), fixture.Scope, k, strings.NewReader("x"), 1, contracts.Meta{}); !errors.Is(err, contracts.ErrInvalidKey) {
				t.Fatalf("Put(%q): %v, want ErrInvalidKey", bad, err)
			}
			if _, err := fixture.Storage.Get(t.Context(), fixture.Scope, k); !errors.Is(err, contracts.ErrInvalidKey) {
				t.Fatalf("Get(%q): %v, want ErrInvalidKey", bad, err)
			}
			if err := fixture.Storage.Delete(t.Context(), fixture.Scope, k); !errors.Is(err, contracts.ErrInvalidKey) {
				t.Fatalf("Delete(%q): %v, want ErrInvalidKey", bad, err)
			}
		}
	})
	t.Run("non-seekable streams and empty blobs", func(t *testing.T) {
		store := fresh(t)
		for _, body := range []string{"", strings.Repeat("streamed bytes\x00\xff\n", 1024)} {
			key := contracts.Key(uuid.NewString())
			storagePut(t, store.Storage, store.Scope, key, body)
			if got := storageRead(t, store.Storage, store.Scope, key); got != body {
				t.Fatal("stored bytes differ from the supplied stream")
			}
		}
	})
	t.Run("a body that declares no length arrives, and collides", func(t *testing.T) {
		// The honest length of a stream is -1, which is what this module's upload
		// route hands every implementation there is. Asking it here rather than
		// beside one adapter is the point: an implementation that writes these
		// bytes and one that refuses them both pass a suite that never asks.
		fixture := fresh(t)
		key, body := contracts.Key(uuid.NewString()), "first chunk\nsecond chunk\n"
		stream := io.MultiReader(strings.NewReader("first chunk\n"), strings.NewReader("second chunk\n"))
		if err := fixture.Storage.Put(t.Context(), fixture.Scope, key, stream, -1, contracts.Meta{}); err != nil {
			t.Fatalf("Put with unknown length: %v", err)
		}
		if got := storageRead(t, fixture.Storage, fixture.Scope, key); got != body {
			t.Fatal("unknown-length upload changed the bytes")
		}
		// And the promise an undeclared length must not quietly cost. The key is
		// still this module's to mint once, so either direction of collision is a
		// bug rather than a replacement: a declared write onto bytes that arrived
		// undeclared, and — the one an object store cannot cover with its own
		// conditional create header, because a multipart create is not a create —
		// an undeclared write onto bytes already there.
		declared := contracts.Key(uuid.NewString())
		storagePut(t, fixture.Storage, fixture.Scope, declared, "written by a request that counted them")
		second := io.MultiReader(strings.NewReader("a second stream\n"), strings.NewReader("that declared nothing\n"))
		if err := fixture.Storage.Put(t.Context(), fixture.Scope, declared, second, -1, contracts.Meta{}); err == nil {
			t.Fatal("a body arriving with no length replaced a key that already held bytes")
		}
		if got := storageRead(t, fixture.Storage, fixture.Scope, declared); got != "written by a request that counted them" {
			t.Fatalf("an undeclared write changed the bytes at a key it should have refused: %q", got)
		}
		if err := fixture.Storage.Put(t.Context(), fixture.Scope, key, strings.NewReader("replacement"), 11, contracts.Meta{}); err == nil {
			t.Fatal("a declared write replaced a key whose bytes arrived undeclared")
		}
		if got := storageRead(t, fixture.Storage, fixture.Scope, key); got != body {
			t.Fatal("a declared write replaced the bytes of an undeclared one")
		}
	})
	t.Run("collisions preserve the original bytes", func(t *testing.T) {
		fixture, key := fresh(t), contracts.Key(uuid.NewString())
		storagePut(t, fixture.Storage, fixture.Scope, key, "original")
		if err := fixture.Storage.Put(t.Context(), fixture.Scope, key, strings.NewReader("replacement"), 11, contracts.Meta{}); err == nil {
			t.Fatal("Put overwrote an existing key")
		}
		if got := storageRead(t, fixture.Storage, fixture.Scope, key); got != "original" {
			t.Fatalf("collision changed the original bytes to %q", got)
		}
	})
	t.Run("concurrent creation has one complete winner", func(t *testing.T) {
		fixture, key := fresh(t), contracts.Key(uuid.NewString())
		const writers = 8
		start, successes := make(chan struct{}), make(chan string, writers)
		var group sync.WaitGroup
		for i := range writers {
			group.Go(func() {
				body := strings.Repeat(fmt.Sprintf("writer %d\n", i), 128)
				<-start
				if err := fixture.Storage.Put(t.Context(), fixture.Scope, key, strings.NewReader(body), int64(len(body)), contracts.Meta{}); err == nil {
					successes <- body
				}
			})
		}
		close(start)
		group.Wait()
		close(successes)
		var winners []string
		for body := range successes {
			winners = append(winners, body)
		}
		if len(winners) != 1 {
			t.Fatalf("%d successful creates, want exactly one", len(winners))
		}
		if got := storageRead(t, fixture.Storage, fixture.Scope, key); got != winners[0] {
			t.Fatal("stored bytes are not the winning writer's complete stream")
		}
	})
	t.Run("missing blobs and repeated deletion", func(t *testing.T) {
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
	})
	t.Run("reader failure cannot become upload success", func(t *testing.T) {
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
	})
	t.Run("independent stores, and a listing that honours its cutoff and its scopes", func(t *testing.T) {
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
	})
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
