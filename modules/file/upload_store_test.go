package file_test

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

// mountedWithStorage is the module as main mounts it, over a real Postgres and the
// store the composition chose. It is `mountedOn` with the store asked for rather than
// assumed, because the question this case asks — did a byte the door accepted arrive
// in the store the deployment named — cannot be asked of a mount whose store the test
// helper fixes.
func mountedWithStorage(t *testing.T, storage contracts.Storage) http.Handler {
	t.Helper()
	_, conn := dbtest.Schema(t, file.Migrations)
	hosts := map[string]tenancy.Tenant{host: acme}
	api, router := httpx.New(httpx.Options{
		PublicHost: host, Tenants: caller{hosts}, Conn: conn, Authorize: caller{hosts},
		Authenticate: func(context.Context, db.Tx[db.Tenant], *http.Request) (tenancy.Principal, bool, error) {
			return tenancy.Principal{UserID: uuid.New()}, true, nil
		},
		Log: slog.New(slog.DiscardHandler),
	})
	_, m := file.Module(file.Deps{Storage: storage, MaxBytes: filetest.Limit})
	m.Routes(surfacesOf(api))
	if err := api.ValidateDeclarations(); err != nil {
		t.Fatalf("the mounted routes do not declare themselves: %v", err)
	}
	return router
}

// scopeOf is the scope of one named tenant — the acme the mount resolves this
// origin to — so that a read at the store is asked with the same prefix the route
// wrote under.
func scopeOf(t *testing.T, tenant tenancy.Tenant) (contracts.Scope, context.Context) {
	t.Helper()
	ctx := tenancy.WithTenant(t.Context(), tenant)
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		t.Fatalf("scope for %s: %v", tenant.Slug, err)
	}
	return scope, ctx
}

// TestAnUploadArrivesInTheObjectStoreTheCompositionChose asks the one question a
// deployment asks of a store: the composition names your object store, a person
// posts a file, did the file arrive?
//
// The route reads the request as a stream (internal/handler.go's `arriving`), so the
// length it hands Storage.Put is the one a stream can honestly declare, and the port
// says in its own words what that means for an implementation: "a part declares no
// length of its own. It is the honest answer, and it is why Storage.Put has to accept
// one". contracts.Upload.Declared reads the same way — "a hint for a Storage that has
// to know one up front", counted bytes being what is stored and limited either way. A
// store may prefer a length; what it cannot do is refuse the door's honest answer and
// leave the module holding a write route that writes nothing.
//
// filetest.RunStorage does let an adapter declare that it needs a length up front
// (StorageFixture.RequiresSize), and that is fair of the store's own callers. This
// case is about the module's caller, which is HTTP, and which declares nothing ever:
// the same flag that makes a store's own suite honest is what makes the pair unusable
// unless somebody asks the question at the pair.
func TestAnUploadArrivesInTheObjectStoreTheCompositionChose(t *testing.T) {
	store := s3Store(t)
	router := mountedWithStorage(t, store.storage)

	code, out := upload(t, router, files, "contract.txt", "text/plain", "one private byte")
	if code != http.StatusCreated {
		t.Fatalf("POST /files over the S3-backed composition = %d %s, want 201: the module's upload route "+
			"streams the body and declares no length, which is the answer contracts.Upload.Declared calls "+
			"honest and contracts.Storage.Put is documented to have to accept", code, out)
	}

	scope, ctx := scopeOf(t, acme)
	prefix := acme.ID.String() + "/"
	var found []string
	for _, n := range store.names(t) {
		if strings.HasPrefix(n, prefix) {
			found = append(found, n)
		}
	}
	if len(found) != 1 {
		t.Fatalf("the object store holds %v, want one object under %s: the row the route wrote names bytes "+
			"that are not here", found, prefix)
	}
	if body := s3Read(t, store, scope, ctx, contracts.Key(strings.TrimPrefix(found[0], prefix))); body != "one private byte" {
		t.Errorf("the object at %s reads %q, want the byte the person posted", found[0], body)
	}
}
