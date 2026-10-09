package internal_test

// The two halves of the tenant segment that Local's app-scoped paths do not get
// from the key: who to write for, and how to remove a byte whose tenant nobody
// can name.
//
// The first case is the one a fail-open constructor survives: a scope that names
// no tenant is refused rather than answered with the nil UUID, because every
// blob of the app filed under uuid.Nil is one directory for every tenant of the
// app again — the layout the segment replaced, wearing the new name. The refusal
// is the scope's own (contracts.ScopeOf and Scope.ObjectName both make it), and
// what this case holds is that the adapter reaches it before it touches a disk.
//
// The second case is the caller that genuinely has no tenant to name. An orphan
// is the blob no row references, so no row can say whose it was, and the sweep
// that removes it runs under system access for that reason (Reconcile). Its door
// is the store's own listing, which reads each tenant out of where the bytes sit —
// and one layout, the flat directory a release before the port carried a scope,
// names no tenant at all and is listed as uuid.Nil. If either removal refused for
// want of a tenant, the sweep would report every orphan removed while deleting
// none of them.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

func TestAStoreOfOneAppWritesOnlyWhereATenantIsNamed(t *testing.T) {
	dir := t.TempDir()
	store := internal.NewLocalOf(appname.MustParse("acme"), dir)
	key := uuid.NewString()

	// The zero scope: the tenant a request resolved is missing, which is what a
	// call outside any request and outside any tenant transaction arrives as.
	err := store.Put(t.Context(), contracts.Scope{}, contracts.Key(key),
		strings.NewReader("nobody's tenant"), -1, contracts.Meta{})
	if err == nil {
		t.Errorf("a blob with no tenant on the call was written under app acme: the path it landed in is one directory for every tenant of the app")
	}
	_ = filepath.WalkDir(dir, func(at string, e os.DirEntry, walkErr error) error {
		if walkErr == nil && !e.IsDir() {
			t.Errorf("the refused write left %s behind", strings.TrimPrefix(at, dir+string(os.PathSeparator)))
		}
		return nil
	})

	// The same store still writes for a call that names its tenant, so the case
	// above is a refusal and not a broken adapter.
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme-customer"}
	ctx := tenancy.WithTenant(t.Context(), tenant)
	scope, scopeErr := contracts.ScopeOf(ctx)
	if scopeErr != nil {
		t.Fatalf("the tenant in the context mints no scope: %v", scopeErr)
	}
	if err := store.Put(ctx, scope, contracts.Key(key), strings.NewReader("one tenant's bytes"), -1, contracts.Meta{}); err != nil {
		t.Fatalf("the same store refused a call that names its tenant: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "acme", tenant.ID.String(), key)); err != nil {
		t.Errorf("the write that named its tenant did not land under it: %v", err)
	}
}

func TestTheSweepRemovesAnOrphanItCannotNameATenantFor(t *testing.T) {
	dir := t.TempDir()
	store := internal.NewLocalOf(appname.MustParse("acme"), dir)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme-customer"}
	key := uuid.NewString()
	ctx := tenancy.WithTenant(t.Context(), tenant)
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		t.Fatalf("the tenant in the context mints no scope: %v", err)
	}
	if err := store.Put(ctx, scope, contracts.Key(key), strings.NewReader("bytes no row references"), -1, contracts.Meta{}); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
	at := filepath.Join(dir, "acme", tenant.ID.String(), key)

	// The sweep's own call, and it names no tenant: the row that would have named
	// one is the row it is looking for and not finding. What it names instead is
	// the store's listing, which reads the tenant back out of where the bytes sit —
	// and one listing, the flat layout, cannot name one and says so with uuid.Nil.
	before := time.Now().Add(time.Hour)
	blobs, err := store.Blobs(t.Context(), db.Tx[db.System]{}, before)
	if err != nil {
		t.Fatalf("list what the store holds: %v", err)
	}
	want := []contracts.Blob{{TenantID: tenant.ID, Key: contracts.Key(key)}}
	if !slices.Equal(blobs, want) {
		t.Fatalf("the store lists %v, want %v: an orphan whose tenant no row can state is a blob the sweep cannot reach", blobs, want)
	}
	if err := store.RemoveBlob(t.Context(), db.Tx[db.System]{}, blobs[0]); err != nil {
		t.Fatalf("remove the orphan %s: %v", key, err)
	}
	if _, err := os.Stat(at); !os.IsNotExist(err) {
		t.Errorf("the orphan is still at %s (stat: %v): a sweep that cannot name the tenant deleted nothing and reported that it had", at, err)
	}
	// A key with nothing at it stays success, so the sweep's retry does not stop.
	if err := store.RemoveBlob(t.Context(), db.Tx[db.System]{}, blobs[0]); err != nil {
		t.Errorf("removing the orphan again returned %v, want nil: a retry that failed because the first attempt succeeded would never stop", err)
	}

	// The one position no tenant is written in: the flat directory a release
	// before the port carried a scope. Its listing carries uuid.Nil, and the
	// removal it answers to is the flat name.
	flat := filepath.Join(dir, key[:2], key)
	if err := os.MkdirAll(filepath.Dir(flat), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(flat, []byte("written before the scope"), 0o600); err != nil {
		t.Fatal(err)
	}
	blobs, err = store.Blobs(t.Context(), db.Tx[db.System]{}, before)
	if err != nil {
		t.Fatalf("list the flat layout: %v", err)
	}
	if want := []contracts.Blob{{TenantID: uuid.Nil, Key: contracts.Key(key)}}; !slices.Equal(blobs, want) {
		t.Fatalf("the store lists %v, want the flat blob under uuid.Nil: a listing that invented a tenant for it would delete from a directory that never held it", blobs)
	}
	if err := store.RemoveBlob(t.Context(), db.Tx[db.System]{}, blobs[0]); err != nil {
		t.Fatalf("remove the flat orphan %s: %v", key, err)
	}
	if _, err := os.Stat(flat); !os.IsNotExist(err) {
		t.Errorf("the flat orphan is still at %s (stat: %v)", flat, err)
	}
}
