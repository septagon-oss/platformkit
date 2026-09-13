package local_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/blob"
	"github.com/septagon-oss/platformkit/kit/blob/blobtest"
	"github.com/septagon-oss/platformkit/kit/blob/providers/local"
)

func TestLocalReaderSupportsSeekingWithoutFileMetadata(t *testing.T) {
	store := local.New(t.TempDir())
	key := "ab000000-0000-4000-8000-000000000001"
	if err := store.Put(t.Context(), key, strings.NewReader("prefix-body"), -1); err != nil {
		t.Fatal(err)
	}
	body, err := store.Get(t.Context(), key)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	seeker, ok := body.(io.Seeker)
	if !ok {
		t.Fatal("local reader lost its seek support")
	}
	if _, err := seeker.Seek(7, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(body)
	if err != nil || string(got) != "body" {
		t.Fatalf("seek read = %q, %v", got, err)
	}
}

func TestLocalStorageConforms(t *testing.T) {
	blobtest.RunStorage(t, func(t *testing.T) blobtest.StorageFixture {
		return blobtest.StorageFixture{Storage: local.New(t.TempDir())}
	})
}

func TestLocalStorageDoesNotHideReadFailuresAsMissingBlobs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ab"), []byte("broken shard"), 0o600); err != nil {
		t.Fatal(err)
	}
	body, err := local.New(root).Get(t.Context(), "ab000000-0000-4000-8000-000000000001")
	if body != nil {
		_ = body.Close()
	}
	if err == nil || errors.Is(err, blob.ErrNoBlob) {
		t.Fatalf("Get through a broken storage directory = %v; want an outage", err)
	}
}

func TestLocalStorageListingExcludesCutoffAndOtherDirectories(t *testing.T) {
	root := t.TempDir()
	store := local.New(filepath.Join(root, "owned"))
	other := local.New(filepath.Join(root, "other"))
	cutoff := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	keys := []string{
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003",
	}
	for i, key := range keys {
		if err := store.Put(t.Context(), key, strings.NewReader("body"), 4); err != nil {
			t.Fatal(err)
		}
		at := cutoff.Add(time.Duration(i-1) * time.Second)
		if err := os.Chtimes(filepath.Join(root, "owned", key[:2], key), at, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := other.Put(t.Context(), keys[1], strings.NewReader("other"), 5); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(root, "other", keys[1][:2], keys[1]), cutoff.Add(-time.Hour), cutoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "owned", "unowned-note.txt"), []byte("not a blob"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := store.Keys(t.Context(), cutoff)
	if err != nil || !slices.Equal(got, keys[:1]) {
		t.Fatalf("Keys(cutoff) = %v, %v; want only %s", got, err, keys[0])
	}
	got, err = store.Keys(t.Context(), cutoff.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(got)
	if !slices.Equal(got, keys) {
		t.Fatalf("Keys(after) = %v; want owned UUID blobs %v", got, keys)
	}
}
