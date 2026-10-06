package internal_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/appname"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestABlobOfAnAppThatNamesItselfSitsUnderTheTenantWhoseBytesTheyAre holds the
// storage row of the naming rule: the persisted key stays the caller's UUID and
// the adapter's physical path becomes <app>/<tenant>/<uuid>, so two tenants of
// one app do not share one directory and an operator can point a quota, a bucket
// policy or a `du` at one tenant's bytes.
//
// The layout is what the constructor's own doc and kit/appname/README.md state,
// and appname.StoragePath takes a tenant for the reason. A path built with the
// nil UUID in the tenant's place carries the app and nothing else: every blob of
// the app lands in one directory again, which is the layout that existed before
// the segment was added, wearing the new name.
//
// The tenant is in the context every one of these calls arrives in — a Put from
// an upload, a Get from a download and a Delete from the removal subscription all
// run inside the tenant's own request or transaction — so the segment is
// reachable where the path is formed. The port takes it as a contracts.Scope
// read off that context, which is the same tenant by the door the adapter can
// reach: contracts.ScopeOf, whose refusal is the scope's and not the adapter's.
func TestABlobOfAnAppThatNamesItselfSitsUnderTheTenantWhoseBytesTheyAre(t *testing.T) {
	dir := t.TempDir()
	store := internal.NewLocalOf(appname.MustParse("acme"), dir)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "acme-customer"}
	key := uuid.NewString()
	ctx := tenancy.WithTenant(t.Context(), tenant)
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		t.Fatalf("the tenant in the context mints no scope: %v", err)
	}

	if err := store.Put(ctx, scope, contracts.Key(key), strings.NewReader("the bytes of one tenant"), -1, contracts.Meta{}); err != nil {
		t.Fatalf("put %s: %v", key, err)
	}

	wanted := filepath.Join(dir, "acme", tenant.ID.String(), key)
	if _, err := os.Stat(wanted); err != nil {
		var found []string
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, walkErr error) error {
			if walkErr == nil && !d.IsDir() {
				found = append(found, strings.TrimPrefix(p, dir+string(os.PathSeparator)))
			}
			return nil
		})
		t.Errorf("the blob sits at %v, want it under %s: the tenant of the request that wrote it is not in the path",
			found, strings.TrimPrefix(wanted, dir+string(os.PathSeparator)))
	}

	// The same tenant's read finds those bytes, which is the half a write alone
	// cannot show: a path that names the tenant only on the way in is unreadable.
	rc, err := store.Get(ctx, scope, contracts.Key(key))
	if err != nil {
		t.Fatalf("the same tenant could not read back what it wrote: %v", err)
	}
	_ = rc.Close()
}
