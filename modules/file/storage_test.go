package file_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

func TestLocalStorageConforms(t *testing.T) {
	filetest.RunStorage(t, func(t *testing.T) filetest.StorageFixture {
		return filetest.StorageFixture{Storage: file.Local(t.TempDir())}
	})
}

func TestLocalStorageDoesNotHideReadFailuresAsMissingBlobs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "ab"), []byte("broken shard"), 0o600); err != nil {
		t.Fatal(err)
	}
	body, err := file.Local(root).Get(t.Context(), "ab000000-0000-4000-8000-000000000001")
	if body != nil {
		_ = body.Close()
	}
	if err == nil || errors.Is(err, contracts.ErrNoBlob) {
		t.Fatalf("Get through a broken storage directory = %v; want an outage", err)
	}
}

func TestLocalStorageListingExcludesCutoffAndOtherDirectories(t *testing.T) {
	root := t.TempDir()
	store := file.Local(filepath.Join(root, "owned"))
	other := file.Local(filepath.Join(root, "other"))
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
