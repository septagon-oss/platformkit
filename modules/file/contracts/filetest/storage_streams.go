package filetest

import (
	"io"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

func storageStreams(t *testing.T, fresh func(*testing.T) StorageFixture) {
	store := fresh(t)
	for _, body := range []string{"", strings.Repeat("streamed bytes\x00\xff\n", 1024)} {
		key := contracts.Key(uuid.NewString())
		storagePut(t, store.Storage, store.Scope, key, body)
		if got := storageRead(t, store.Storage, store.Scope, key); got != body {
			t.Fatal("stored bytes differ from the supplied stream")
		}
	}
}

func storageUnknownLength(t *testing.T, fresh func(*testing.T) StorageFixture) {
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
}

func storageCollisions(t *testing.T, fresh func(*testing.T) StorageFixture) {
	fixture, key := fresh(t), contracts.Key(uuid.NewString())
	storagePut(t, fixture.Storage, fixture.Scope, key, "original")
	if err := fixture.Storage.Put(t.Context(), fixture.Scope, key, strings.NewReader("replacement"), 11, contracts.Meta{}); err == nil {
		t.Fatal("Put overwrote an existing key")
	}
	if got := storageRead(t, fixture.Storage, fixture.Scope, key); got != "original" {
		t.Fatalf("collision changed the original bytes to %q", got)
	}
}
