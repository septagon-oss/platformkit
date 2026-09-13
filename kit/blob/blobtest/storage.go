// Package blobtest checks byte-storage providers without File or a database.
package blobtest

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/blob"
)

// StorageFixture is a fresh, empty storage scope. The factory owns its cleanup.
// RequiresSize records a provider capability for tests only: Storage permits
// providers to refuse an unknown length. It adds no runtime interface.
type StorageFixture struct {
	Storage      blob.Storage
	RequiresSize bool
}

// RunStorage checks the byte-storage contract without a database or service.
// Every factory call must return an independent scope, including calls made by
// the same subtest. Lister checks run only when that optional contract exists.
func RunStorage(t *testing.T, fresh func(*testing.T) StorageFixture) {
	t.Helper()
	t.Run("non-seekable streams and empty blobs", func(t *testing.T) {
		store := fresh(t).Storage
		for _, body := range []string{"", strings.Repeat("streamed bytes\x00\xff\n", 1024)} {
			key := uuid.NewString()
			storagePut(t, store, key, body)
			if got := storageRead(t, store, key); got != body {
				t.Fatal("stored bytes differ from the supplied stream")
			}
		}
	})
	t.Run("unknown length follows the declared capability", func(t *testing.T) {
		fixture := fresh(t)
		key, body := uuid.NewString(), "first chunk\nsecond chunk\n"
		stream := io.MultiReader(strings.NewReader("first chunk\n"), strings.NewReader("second chunk\n"))
		err := fixture.Storage.Put(t.Context(), key, stream, -1)
		if fixture.RequiresSize {
			if err == nil {
				t.Fatal("provider declared that it requires a length but accepted -1")
			}
			storageMissing(t, fixture.Storage, key)
			return
		}
		if err != nil {
			t.Fatalf("Put with unknown length: %v", err)
		}
		if got := storageRead(t, fixture.Storage, key); got != body {
			t.Fatal("unknown-length upload changed the bytes")
		}
	})
	t.Run("collisions preserve the original bytes", func(t *testing.T) {
		store, key := fresh(t).Storage, uuid.NewString()
		storagePut(t, store, key, "original")
		if err := store.Put(t.Context(), key, strings.NewReader("replacement"), 11); err == nil {
			t.Fatal("Put overwrote an existing key")
		}
		if got := storageRead(t, store, key); got != "original" {
			t.Fatalf("collision changed the original bytes to %q", got)
		}
	})
	t.Run("concurrent creation has one complete winner", func(t *testing.T) {
		store, key := fresh(t).Storage, uuid.NewString()
		const writers = 8
		start, successes := make(chan struct{}), make(chan string, writers)
		var group sync.WaitGroup
		for i := range writers {
			group.Go(func() {
				body := strings.Repeat(fmt.Sprintf("writer %d\n", i), 128)
				<-start
				if err := store.Put(t.Context(), key, strings.NewReader(body), int64(len(body))); err == nil {
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
		if got := storageRead(t, store, key); got != winners[0] {
			t.Fatal("stored bytes are not the winning writer's complete stream")
		}
	})
	t.Run("missing blobs and repeated deletion", func(t *testing.T) {
		store, key := fresh(t).Storage, uuid.NewString()
		storageMissing(t, store, key)
		for range 2 {
			if err := store.Delete(t.Context(), key); err != nil {
				t.Fatalf("Delete missing key: %v", err)
			}
		}
		storagePut(t, store, key, "remove me")
		for range 2 {
			if err := store.Delete(t.Context(), key); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			storageMissing(t, store, key)
		}
	})
	t.Run("reader failure cannot become upload success", func(t *testing.T) {
		store, key := fresh(t).Storage, uuid.NewString()
		stream := &storageFailureReader{}
		if err := store.Put(t.Context(), key, stream, 128); err == nil {
			t.Fatal("Put reported success after the caller's stream failed")
		}
		if !stream.read {
			t.Fatal("Put failed before exercising the caller's stream")
		}
		// Storage does not promise atomic cleanup on every provider error. Its
		// retry-safe Delete must still allow the owner to remove a failed write.
		if err := store.Delete(t.Context(), key); err != nil {
			t.Fatalf("clean failed upload: %v", err)
		}
		storageMissing(t, store, key)
	})
	t.Run("independent scopes and optional listing cutoff", func(t *testing.T) {
		first, second := fresh(t).Storage, fresh(t).Storage
		key := uuid.NewString()
		storagePut(t, first, key, "first scope")
		storageMissing(t, second, key)
		storagePut(t, second, key, "second scope")
		if got := storageRead(t, first, key); got != "first scope" {
			t.Fatal("another storage scope changed the bytes")
		}
		if err := second.Delete(t.Context(), key); err != nil {
			t.Fatal(err)
		}
		if got := storageRead(t, first, key); got != "first scope" {
			t.Fatal("deleting in another scope removed the bytes")
		}
		for i, store := range []blob.Storage{first, second} {
			if lister, ok := store.(blob.Lister); ok {
				keys, err := lister.Keys(t.Context(), time.Unix(0, 0))
				if err != nil || len(keys) != 0 {
					t.Fatalf("new blobs listed before historical cutoff: %v, %v", keys, err)
				}
				keys, err = lister.Keys(t.Context(), time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC))
				want := []string{}
				if i == 0 {
					want = []string{key}
				}
				if err != nil || !slices.Equal(keys, want) {
					t.Fatalf("listing crossed its scope or lost a blob: %v, %v; want %v", keys, err, want)
				}
			}
		}
	})
}

func storagePut(t *testing.T, store blob.Storage, key, body string) {
	t.Helper()
	stream := struct{ io.Reader }{strings.NewReader(body)}
	if err := store.Put(t.Context(), key, stream, int64(len(body))); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func storageRead(t *testing.T, store blob.Storage, key string) string {
	t.Helper()
	body, err := store.Get(t.Context(), key)
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

func storageMissing(t *testing.T, store blob.Storage, key string) {
	t.Helper()
	body, err := store.Get(t.Context(), key)
	if body != nil {
		_ = body.Close()
	}
	if !errors.Is(err, blob.ErrNoBlob) {
		t.Fatalf("Get missing blob: %v, want ErrNoBlob", err)
	}
}

type storageFailureReader struct{ read bool }

func (r *storageFailureReader) Read(p []byte) (int, error) {
	r.read = true
	return copy(p, "incomplete"), errors.New("fixture source stream failed")
}
