package file_test

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

// s3Store is the adapter against the S3-compatible store the test environment names
// (PLATFORMKIT_TEST_S3_ENDPOINT, host:port; the bucket is created per run). Any store
// that speaks the S3 API serves: the kernel's own stack uses SeaweedFS.
func s3Store(t *testing.T) contracts.Storage {
	t.Helper()
	endpoint := os.Getenv("PLATFORMKIT_TEST_S3_ENDPOINT")
	if endpoint == "" {
		// Fails rather than skips, as the NATS transport does: a suite that quietly skips the store it
		// ships proves nothing. `make up` starts one and `make test` exports its address.
		t.Fatal("PLATFORMKIT_TEST_S3_ENDPOINT is unset; start the stack with `make up` and export the test URLs")
	}
	access, secret := os.Getenv("PLATFORMKIT_TEST_S3_ACCESS_KEY"), os.Getenv("PLATFORMKIT_TEST_S3_SECRET_KEY")
	bucket := "pkit-test-" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
	admin, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(access, secret, "")})
	if err != nil {
		t.Fatal(err)
	}
	if err := admin.MakeBucket(t.Context(), bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("make bucket %s: %v", bucket, err)
	}
	store, err := file.S3(file.S3Config{Endpoint: endpoint, Bucket: bucket, AccessKey: access, SecretKey: secret})
	if err != nil {
		t.Fatal(err)
	}
	return store
}

// scoped is the adapter as a request of one tenant sees it: every call carries that
// tenant, which is what production contexts carry (a request's resolved host, an event
// handler's tenant transaction). A fresh tenant per fixture is a fresh, empty scope.
type scoped struct {
	inner  contracts.Storage
	tenant tenancy.Tenant
}

func (s scoped) at(ctx context.Context) context.Context { return tenancy.WithTenant(ctx, s.tenant) }
func (s scoped) Put(ctx context.Context, k string, r io.Reader, n int64) error {
	return s.inner.Put(s.at(ctx), k, r, n)
}
func (s scoped) Get(ctx context.Context, k string) (io.ReadCloser, error) {
	return s.inner.Get(s.at(ctx), k)
}
func (s scoped) Delete(ctx context.Context, k string) error { return s.inner.Delete(s.at(ctx), k) }

func tenantNamed(slug string) tenancy.Tenant { return tenancy.Tenant{ID: uuid.New(), Slug: slug} }

// TestS3StorageConforms runs the contract every Storage keeps — streams, empty blobs,
// unknown lengths, collisions, missing keys — through the S3 adapter, one tenant per scope.
func TestS3StorageConforms(t *testing.T) {
	store := s3Store(t)
	filetest.RunStorage(t, func(t *testing.T) filetest.StorageFixture {
		return filetest.StorageFixture{Storage: scoped{inner: store, tenant: tenantNamed("s" + uuid.NewString()[:6])}}
	})
}

// TestAKeyOneTenantWroteOpensNothingForAnother is the reason the adapter carries the
// tenant: in a shared bucket, a UUID copied out of one tenant's row must name nothing in
// another's prefix — not its bytes, not a collision, not a deletion.
func TestAKeyOneTenantWroteOpensNothingForAnother(t *testing.T) {
	store := s3Store(t)
	acme, globex := scoped{store, tenantNamed("acme")}, scoped{store, tenantNamed("globex")}
	k := uuid.NewString()
	if err := acme.Put(t.Context(), k, strings.NewReader("acme's contract"), -1); err != nil {
		t.Fatalf("acme put: %v", err)
	}
	if body, err := globex.Get(t.Context(), k); !errors.Is(err, contracts.ErrNoBlob) {
		if body != nil {
			_ = body.Close()
		}
		t.Fatalf("globex opened acme's key: err=%v", err)
	}
	if err := globex.Delete(t.Context(), k); err != nil {
		t.Fatalf("globex's delete of a key it does not hold: %v", err)
	}
	if err := globex.Put(t.Context(), k, strings.NewReader("globex's own"), -1); err != nil {
		t.Fatalf("the same key is free in another tenant's prefix, and was refused: %v", err)
	}
	body, err := acme.Get(t.Context(), k)
	if err != nil {
		t.Fatalf("acme's bytes after globex's delete and put: %v", err)
	}
	defer body.Close()
	if got, _ := io.ReadAll(body); string(got) != "acme's contract" {
		t.Fatalf("acme's object now reads %q", got)
	}
}

// TestAnS3CallWithNoTenantIsRefused: an object is always some tenant's, so a call from a
// context that resolved no tenant is an error and writes nothing — never a key at the root.
func TestAnS3CallWithNoTenantIsRefused(t *testing.T) {
	store := s3Store(t)
	k := uuid.NewString()
	if err := store.Put(t.Context(), k, strings.NewReader("nobody's"), -1); err == nil {
		t.Fatal("a Put with no tenant was accepted")
	}
	if _, err := store.Get(t.Context(), k); err == nil || errors.Is(err, contracts.ErrNoBlob) {
		t.Fatalf("a Get with no tenant = %v; want a refusal, not an answer about the bytes", err)
	}
	if err := store.Put(t.Context(), "../escape", strings.NewReader("x"), -1); err == nil {
		t.Fatal("a key that is not a UUID was accepted")
	}
}
