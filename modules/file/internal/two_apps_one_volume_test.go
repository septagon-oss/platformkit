package internal_test

// Two apps that each name themselves, mounted at one root (decision 0074): each
// writes under its own segment, neither reads the other's bytes, and neither's
// orphan sweep lists — and so can never remove — what the other wrote.

import (
	"errors"
	"io"
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

func TestTwoAppsOnOneVolumeNeitherReadsNorSweepsTheOthersBytes(t *testing.T) {
	dir := t.TempDir()
	first := internal.NewLocalOf(appname.MustParse("alpha"), dir)
	second := internal.NewLocalOf(appname.MustParse("beta"), dir)

	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "alpha-customer"}
	ctx := tenancy.WithTenant(t.Context(), tenant)
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		t.Fatalf("the tenant in the context mints no scope: %v", err)
	}
	key := contracts.Key(uuid.NewString())
	if err := first.Put(ctx, scope, key, strings.NewReader("alpha's bytes"), -1, contracts.Meta{}); err != nil {
		t.Fatalf("put under alpha: %v", err)
	}
	at := filepath.Join(dir, "alpha", tenant.ID.String(), key.String())
	if _, err := os.Stat(at); err != nil {
		t.Fatalf("alpha's write is not under its own segment: %v", err)
	}

	// The other app, handed the same tenant id and key, finds nothing.
	if r, err := second.Get(ctx, scope, key); !errors.Is(err, contracts.ErrNoBlob) {
		if err == nil {
			b, _ := io.ReadAll(r)
			r.Close()
			t.Errorf("beta read %q from alpha's segment", b)
		} else {
			t.Errorf("beta's read of a key it never wrote answered %v, want ErrNoBlob", err)
		}
	}
	if n, err := second.Prove(ctx, scope, key); err != nil || n != 0 {
		t.Errorf("beta's proof counts %d copies (err %v) of a key only alpha holds, want 0", n, err)
	}

	// Beta's sweep lists nothing; alpha's lists its own blob.
	before := time.Now().Add(time.Hour)
	listed, err := second.Blobs(t.Context(), db.Tx[db.System]{}, before)
	if err != nil {
		t.Fatalf("beta lists its store: %v", err)
	}
	if len(listed) != 0 {
		t.Errorf("beta's sweep lists %v, which sit in alpha's segment: a sweep that lists them removes another app's bytes", listed)
	}
	for _, b := range listed {
		_ = second.RemoveBlob(t.Context(), db.Tx[db.System]{}, b)
	}
	if err := second.Delete(ctx, scope, key); err != nil {
		t.Fatalf("beta's delete of a key it never wrote: %v", err)
	}
	if _, err := os.Stat(at); err != nil {
		t.Errorf("alpha's bytes are gone after beta swept and deleted: %v", err)
	}

	own, err := first.Blobs(t.Context(), db.Tx[db.System]{}, before)
	if err != nil {
		t.Fatalf("alpha lists its store: %v", err)
	}
	if want := []contracts.Blob{{TenantID: tenant.ID, Key: key}}; !slices.Equal(own, want) {
		t.Errorf("alpha's sweep lists %v, want %v", own, want)
	}
}
