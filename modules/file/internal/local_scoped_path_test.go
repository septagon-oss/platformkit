package internal_test

// The two halves of the tenant segment that Local's app-scoped paths do not get
// from the key: who to write for, and how to remove a byte whose tenant nobody
// can name.
//
// The first case is the one a fail-open constructor survives: reading the tenant
// off the call and writing the nil UUID when the call names none keeps
// TestABlobOfAnAppThatNamesItselfSitsUnderTheTenantWhoseBytesTheyAre green,
// because that case hands over a tenant, and puts every *other* blob of the app —
// the ones whose caller lost its context somewhere — into one shared directory
// again. Refusing is the difference between a layout and a habit.
//
// The second case is the caller that genuinely has no tenant to name. An orphan
// is the blob no row references, so no row can say whose it was, and the sweep
// that removes it runs under system access for that reason (Reconcile). If that
// call refused for want of a tenant, the sweep would report every orphan removed
// while deleting none of them.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

func TestAStoreOfOneAppWritesOnlyWhereATenantIsNamed(t *testing.T) {
	dir := t.TempDir()
	store := internal.NewLocalOf(appname.MustParse("acme"), dir)
	key := uuid.NewString()

	err := store.Put(t.Context(), key, strings.NewReader("nobody's tenant"), -1)
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
	if err := store.Put(ctx, key, strings.NewReader("one tenant's bytes"), -1); err != nil {
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

	if err := store.Put(ctx, key, strings.NewReader("bytes no row references"), -1); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}
	at := filepath.Join(dir, "acme", tenant.ID.String(), key)

	// The sweep's own call: the same key, no tenant on the context, because the
	// row that would have named one is the row it is looking for and not finding.
	if err := store.Delete(t.Context(), key); err != nil {
		t.Fatalf("remove the orphan %s: %v", key, err)
	}
	if _, err := os.Stat(at); !os.IsNotExist(err) {
		t.Errorf("the orphan is still at %s (stat: %v): a sweep that cannot name the tenant deleted nothing and reported that it had", at, err)
	}
	// A key with nothing at it stays success, so the sweep's retry does not stop.
	if err := store.Delete(t.Context(), key); err != nil {
		t.Errorf("removing the orphan again returned %v, want nil: a retry that failed because the first attempt succeeded would never stop", err)
	}
}
