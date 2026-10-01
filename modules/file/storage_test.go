package file_test

import (
	"cmp"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"

	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

func TestLocalStorageConforms(t *testing.T) {
	filetest.RunStorage(t, func(t *testing.T) filetest.StorageFixture {
		tenant := uuid.New()
		return filetest.StorageFixture{
			Storage: file.Local(t.TempDir()), TenantID: tenant,
			Scope: filetest.TenantScope(t, tenant),
		}
	})
}

func TestLocalStorageDoesNotHideReadFailuresAsMissingBlobs(t *testing.T) {
	root := t.TempDir()
	tenant := uuid.New()
	store := file.Local(root)
	scope := filetest.TenantScope(t, tenant)
	// The directory a blob would live in is a file instead, so opening it is an
	// outage and not an absent object: a store that answered ErrNoBlob here
	// would turn a broken volume into a tenant's missing files.
	at := filepath.Join(root, tenant.String(), "ab")
	if err := os.MkdirAll(filepath.Dir(at), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(at, []byte("broken shard"), 0o600); err != nil {
		t.Fatal(err)
	}
	body, err := store.Get(t.Context(), scope, contracts.Key("ab000000-0000-4000-8000-000000000001"))
	if body != nil {
		_ = body.Close()
	}
	if err == nil || errors.Is(err, contracts.ErrNoBlob) {
		t.Fatalf("Get through a broken storage directory = %v; want an outage", err)
	}
}

// TestLocalStorageProvesWhatIsAtOneName is the certificate's other half. The
// erasure subscription stamps verified_at only when the store reports that it
// holds nothing at the name it was asked about, so a disk store with no answer
// would leave every erasure of an installation running on disk uncertified — and
// a disk store that answered "gone" without looking would certify one that left
// bytes behind.
//
// Two names are in scope, because Delete writes to both: the tenant's own
// directory and the flat layout an installation had before the port carried a
// scope. A leftover there is a copy that is still here.
func TestLocalStorageProvesWhatIsAtOneName(t *testing.T) {
	root := t.TempDir()
	tenant := uuid.New()
	store := file.Local(root)
	scope := filetest.TenantScope(t, tenant)
	key := contracts.Key("7c1f2f3e-1f4c-4a55-8f0a-2b3c4d5e6f70")
	if err := store.Put(t.Context(), scope, key, strings.NewReader("body"), 4, contracts.Meta{}); err != nil {
		t.Fatal(err)
	}
	if seen, err := store.Prove(t.Context(), scope, key); err != nil || seen != 1 {
		t.Fatalf("Prove over bytes still here = %d, %v; want one copy", seen, err)
	}
	if err := store.Delete(t.Context(), scope, key); err != nil {
		t.Fatal(err)
	}
	if seen, err := store.Prove(t.Context(), scope, key); err != nil || seen != 0 {
		t.Fatalf("Prove after the delete = %d, %v; want nothing left to certify", seen, err)
	}

	legacy := filepath.Join(root, key.String()[:2], key.String())
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("written before the scope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if seen, err := store.Prove(t.Context(), scope, key); err != nil || seen != 1 {
		t.Fatalf("Prove with the pre-scope copy still on the disk = %d, %v; want the one copy that is still here", seen, err)
	}
	// And the delete that clears both names is what turns that count back to
	// zero, which is the half that makes the retry safe rather than pointless.
	if err := store.Delete(t.Context(), scope, key); err != nil {
		t.Fatal(err)
	}
	if seen, err := store.Prove(t.Context(), scope, key); err != nil || seen != 0 {
		t.Errorf("Prove after the delete that clears both layouts = %d, %v; want zero", seen, err)
	}
}

// TestLocalStorageListingExcludesCutoffAndOtherDirectories is the orphan sweep's
// own safety argument at the store: a listing with a cutoff must not name a blob
// written after it, must not name anything in a directory this store was not
// pointed at, and must not name a file that is not a key. The sweep deletes what
// a listing reports, so every one of those is a file it would have removed.
func TestLocalStorageListingExcludesCutoffAndOtherDirectories(t *testing.T) {
	root := t.TempDir()
	tenant := uuid.New()
	store := file.Local(filepath.Join(root, "owned"))
	other := file.Local(filepath.Join(root, "other"))
	scope, otherScope := filetest.TenantScope(t, tenant), filetest.TenantScope(t, tenant)
	cutoff := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	keys := []contracts.Key{
		"00000000-0000-4000-8000-000000000001",
		"00000000-0000-4000-8000-000000000002",
		"00000000-0000-4000-8000-000000000003",
	}
	for i, key := range keys {
		if err := store.Put(t.Context(), scope, key, strings.NewReader("body"), 4, contracts.Meta{}); err != nil {
			t.Fatal(err)
		}
		at := cutoff.Add(time.Duration(i-1) * time.Second)
		if err := os.Chtimes(filepath.Join(root, "owned", tenant.String(), key.String()[:2], key.String()), at, at); err != nil {
			t.Fatal(err)
		}
	}
	if err := other.Put(t.Context(), otherScope, keys[1], strings.NewReader("other"), 5, contracts.Meta{}); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(root, "other", tenant.String(), keys[1].String()[:2], keys[1].String()), cutoff.Add(-time.Hour), cutoff.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "owned", tenant.String(), "unowned-note.txt"), []byte("not a blob"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := []contracts.Blob{{TenantID: tenant, Key: keys[0]}}
	if got, err := store.Blobs(t.Context(), db.Tx[db.System]{}, cutoff); err != nil || !slices.Equal(got, want) {
		t.Fatalf("Blobs(cutoff) = %v, %v; want only %v", got, err, want)
	}
	got, err := store.Blobs(t.Context(), db.Tx[db.System]{}, cutoff.Add(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	slices.SortFunc(got, func(a, b contracts.Blob) int { return cmp.Compare(a.Key, b.Key) })
	all := []contracts.Blob{{TenantID: tenant, Key: keys[0]}, {TenantID: tenant, Key: keys[1]}, {TenantID: tenant, Key: keys[2]}}
	if !slices.Equal(got, all) {
		t.Fatalf("Blobs(after) = %v; want the three blobs this store holds: %v", got, all)
	}
	// And the removal the sweep would make: one blob out, the other two still
	// listed, and the sibling store untouched.
	if err := store.RemoveBlob(t.Context(), db.Tx[db.System]{}, got[0]); err != nil {
		t.Fatalf("RemoveBlob: %v", err)
	}
	if again, err := store.Blobs(t.Context(), db.Tx[db.System]{}, cutoff.Add(2*time.Second)); err != nil || len(again) != 2 {
		t.Fatalf("after one removal the store lists %v, %v; want two", again, err)
	}
}

// TestAScopeNamesOneTenantsBytesAndNothingsAnothers is the register's name for
// this module's isolation case: the one selector a reviewer or a pillar audit
// runs to find out whether a second tenant's bytes are reachable. The assertions
// live in filetest.RunStorage, beside the port every adapter implements, so an
// adapter that gets the prefix wrong fails a test it was never told about; this
// is the same suite run under the name the documentation cites, over both stores
// this repository ships, so the selector finds the case rather than a summary of
// it. A future file.S3 joins the list here, which is where the one case all three
// stores have to answer the same way lives.
func TestAScopeNamesOneTenantsBytesAndNothingsAnothers(t *testing.T) {
	stores := []struct {
		name  string
		fresh func(*testing.T) filetest.StorageFixture
	}{
		{"Local", func(t *testing.T) filetest.StorageFixture {
			tenant := uuid.New()
			return filetest.StorageFixture{
				Storage: file.Local(t.TempDir()), TenantID: tenant,
				Scope: filetest.TenantScope(t, tenant),
			}
		}},
		{"Memory", func(t *testing.T) filetest.StorageFixture {
			tenant := uuid.New()
			return filetest.StorageFixture{
				Storage: filetest.NewMemory(), TenantID: tenant,
				Scope: filetest.TenantScope(t, tenant),
			}
		}},
	}
	for _, store := range stores {
		t.Run(store.name, func(t *testing.T) {
			filetest.RunStorage(t, store.fresh)
		})
	}
}
