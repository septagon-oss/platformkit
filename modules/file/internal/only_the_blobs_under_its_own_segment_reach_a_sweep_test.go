package internal_test

// A listing is a list of things to delete: contracts.Reconciler's Blobs answers
// what the sweep of reconcile.go goes on to remove, so the question a store that
// shares its volume with other apps has to answer is not "can I read this byte"
// but "is this byte mine to remove". Reading and sweeping are therefore answered
// by two different rules, and this is the narrower one.
//
// The cases run without a database because the rule is the path's: no row is
// consulted in deciding whose directory a blob sits in, and the one the sweep does
// consult — which keys a row names — protects a live blob in every layout,
// including the flat directory no app's name is written in.

import (
	"errors"
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

func TestASweepReachesOnlyTheBlobsUnderItsOwnSegment(t *testing.T) {
	dir := t.TempDir()
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "one-customer"}
	ctx := tenancy.WithTenant(t.Context(), tenant)
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		t.Fatalf("the tenant in the context mints no scope: %v", err)
	}
	before := time.Now().Add(time.Hour)

	// The position a release that named no app wrote, and still writes: the
	// deployment of one app, which is every installation this repository composes.
	legacy := contracts.Key(uuid.NewString())
	if err := internal.NewLocal(dir).Put(ctx, scope, legacy,
		strings.NewReader("bytes from before the segment"), -1, contracts.Meta{}); err != nil {
		t.Fatalf("write the un-prefixed blob: %v", err)
	}
	named := internal.NewLocalOf(appname.MustParse("beta"), dir)
	blob, err := named.Get(ctx, scope, legacy)
	if err != nil {
		t.Fatalf("an app that names itself could not read the bytes its own release wrote before the segment: %v", err)
	}
	_ = blob.Close()
	// Reading them is not owning them: the un-prefixed directory is one this app
	// shares with every app mounted at the same root, and nothing in it says which
	// of them wrote a byte, so it stays out of the listing that authorises a delete.
	if blobs, err := named.Blobs(ctx, db.Tx[db.System]{}, before); err != nil || len(blobs) != 0 {
		t.Fatalf("a named app lists %v (%v), want nothing: the un-prefixed position is a list of deletions aimed at another app's directory", blobs, err)
	}

	// Two apps that both name themselves, one volume: each lists its own bytes and
	// only its own, and one app's removal leaves the other's in place.
	betaKey := contracts.Key(uuid.NewString())
	if err := named.Put(ctx, scope, betaKey, strings.NewReader("beta's bytes"), -1, contracts.Meta{}); err != nil {
		t.Fatalf("write beta's blob: %v", err)
	}
	gamma := internal.NewLocalOf(appname.MustParse("gamma"), dir)
	gammaKey := contracts.Key(uuid.NewString())
	if err := gamma.Put(ctx, scope, gammaKey, strings.NewReader("gamma's bytes"), -1, contracts.Meta{}); err != nil {
		t.Fatalf("write gamma's blob: %v", err)
	}
	blobs, err := named.Blobs(ctx, db.Tx[db.System]{}, before)
	if err != nil {
		t.Fatalf("list what beta holds: %v", err)
	}
	want := []contracts.Blob{{TenantID: tenant.ID, Key: betaKey}}
	if !slices.Equal(blobs, want) {
		t.Fatalf("beta lists %v, want %v: a listing that names another app's directory is a sweep that deletes it", blobs, want)
	}
	if err := named.RemoveBlob(ctx, db.Tx[db.System]{}, blobs[0]); err != nil {
		t.Fatalf("remove beta's own blob: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "gamma", tenant.ID.String(), gammaKey.String())); err != nil {
		t.Errorf("beta's sweep removed gamma's blob: %v", err)
	}

	// And the side the line must not take away: the deployment of one app still
	// reaches the un-prefixed position it owns, so narrowing what a named app may
	// sweep has not left a volume nobody can clean.
	oneApp := internal.NewLocal(dir)
	blobs, err = oneApp.Blobs(ctx, db.Tx[db.System]{}, before)
	if err != nil {
		t.Fatalf("list what the deployment of one app holds: %v", err)
	}
	wantOwn := []contracts.Blob{{TenantID: tenant.ID, Key: legacy}}
	if !slices.Equal(blobs, wantOwn) {
		t.Fatalf("the deployment of one app lists %v, want %v: the app segments are the other apps' directories", blobs, wantOwn)
	}
	if err := oneApp.RemoveBlob(ctx, db.Tx[db.System]{}, blobs[0]); err != nil {
		t.Fatalf("remove the un-prefixed orphan: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, tenant.ID.String(), legacy.String()[:2], legacy.String())); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the deployment of one app left the orphan it did list (stat error %v)", err)
	}
}
