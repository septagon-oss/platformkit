package file_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
)

// s3 is the adapter together with the client the test uses to look at the bucket
// from outside it. The adapter is the thing under test; the client is how a claim
// about what the store holds is read at the store rather than at the adapter's
// own account.
type s3 struct {
	storage  contracts.Storage
	signer   contracts.Signer
	admin    *minio.Client
	bucket   string
	endpoint string
}

// s3Store is the adapter against the S3-compatible store the environment names
// (PLATFORMKIT_TEST_S3_ENDPOINT, host:port), in a bucket of its own. Any store
// that speaks the S3 API serves: the repository's stack ships SeaweedFS because
// its image pulls without a registry login.
//
// It fails rather than skips when the endpoint is unset, exactly as the NATS
// transport's cases do: a suite that quietly skips the store it ships proves
// nothing, and an adapter nobody runs is a file that lies. `make up` starts the
// store and `make test` exports its address.
func s3Store(t *testing.T) *s3 {
	t.Helper()
	endpoint := os.Getenv("PLATFORMKIT_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Fatal("PLATFORMKIT_TEST_S3_ENDPOINT is unset; start the stack with `make up`")
	}
	access, secret := os.Getenv("PLATFORMKIT_TEST_S3_ACCESS_KEY"), os.Getenv("PLATFORMKIT_TEST_S3_SECRET_KEY")
	prefix := os.Getenv("PLATFORMKIT_TEST_S3_BUCKET_PREFIX")
	if prefix == "" {
		prefix = "platformkit-test"
	}
	bucket := prefix + "-" + uuid.NewString()[:8]
	admin, err := minio.New(endpoint, &minio.Options{Creds: credentials.NewStaticV4(access, secret, "")})
	if err != nil {
		t.Fatalf("connect to %s: %v", endpoint, err)
	}
	if err := admin.MakeBucket(t.Context(), bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatalf("make bucket %s: %v", bucket, err)
	}
	t.Cleanup(func() {
		// The bucket is this run's and the objects in it are this run's. A store
		// the tests leave dirty is a store the next run argues with.
		ctx := context.Background()
		for obj := range admin.ListObjects(ctx, bucket, minio.ListObjectsOptions{Recursive: true}) {
			if obj.Err != nil {
				break
			}
			_ = admin.RemoveObject(ctx, bucket, obj.Key, minio.RemoveObjectOptions{})
		}
		_ = admin.RemoveBucket(ctx, bucket)
	})
	store, err := file.S3(file.S3Config{Endpoint: endpoint, Bucket: bucket, AccessKey: access, SecretKey: secret})
	if err != nil {
		t.Fatalf("new S3 store: %v", err)
	}
	return &s3{storage: store, signer: store, admin: admin, bucket: bucket, endpoint: endpoint}
}

// scopeFor is the scope a request of this tenant would carry: the tenant on the
// context, which is the only door contracts.Scope has.
func scopeFor(t *testing.T, slug string) (contracts.Scope, context.Context) {
	t.Helper()
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: slug}
	ctx := tenancy.WithTenant(t.Context(), tenant)
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		t.Fatalf("scope for %s: %v", slug, err)
	}
	return scope, ctx
}

// names is what the bucket actually holds, read with a client that knows nothing
// about this module.
func (s *s3) names(t *testing.T) []string {
	t.Helper()
	var out []string
	for obj := range s.admin.ListObjects(t.Context(), s.bucket, minio.ListObjectsOptions{Recursive: true}) {
		if obj.Err != nil {
			t.Fatalf("list %s: %v", s.bucket, obj.Err)
		}
		out = append(out, obj.Key)
	}
	return out
}

// TestTheS3AdapterKeepsTheStorageContract runs everything the port promises — one
// scope's key opening nothing in another's, a scope with no tenant refused before
// a byte is read, a key that is not a UUID refused, streams, empty blobs,
// collisions, missing keys, a reader that fails — through an object store rather
// than through a double of one. It is the same suite Local and the fake run.
func TestTheS3AdapterKeepsTheStorageContract(t *testing.T) {
	store := s3Store(t)
	filetest.RunStorage(t, func(t *testing.T) filetest.StorageFixture {
		scope, _ := scopeFor(t, "t"+uuid.NewString()[:6])
		return filetest.StorageFixture{
			Storage: store.storage,
			Scope:   scope,
			// Every fixture of this run shares one bucket, which is the point:
			// the isolation under test sits between two prefixes in one bucket,
			// not between two buckets.
			TenantID: scope.TenantID(),
			// The adapter refuses a length it was not given: the conditional
			// write that refuses an existing key does not cover a multipart
			// upload, and the promise about an existing key is the one worth
			// keeping. The suite asks that question once, on this flag.
			RequiresSize: true,
		}
	})
}

// TestAnObjectOneTenantWroteOpensNothingForAnother is the reason the adapter takes
// the scope in every call: in one bucket, a UUID copied out of one tenant's row
// must name nothing in another tenant's prefix — not its bytes, not a collision,
// not a deletion. The suite asks the same question; this asks it of a real store,
// and reads the object names at the store.
func TestAnObjectOneTenantWroteOpensNothingForAnother(t *testing.T) {
	store := s3Store(t)
	acme, acmeCtx := scopeFor(t, "acme")
	globex, globexCtx := scopeFor(t, "globex")
	key := contracts.Key(uuid.NewString())
	if err := store.storage.Put(acmeCtx, acme, key, strings.NewReader("acme's contract"), 15,
		contracts.Meta{ContentType: "text/plain"}); err != nil {
		t.Fatalf("acme put: %v", err)
	}
	if body, err := store.storage.Get(globexCtx, globex, key); !errors.Is(err, contracts.ErrNoBlob) {
		if body != nil {
			_ = body.Close()
		}
		t.Fatalf("globex opened acme's key: err=%v", err)
	}
	if err := store.storage.Delete(globexCtx, globex, key); err != nil {
		t.Fatalf("globex deleted a key it holds nothing at: %v", err)
	}
	if err := store.storage.Put(globexCtx, globex, key, strings.NewReader("globex's own"), 12,
		contracts.Meta{ContentType: "text/plain"}); err != nil {
		t.Fatalf("the same key is free in another tenant's prefix and was refused: %v", err)
	}
	if got := s3Read(t, store, acme, acmeCtx, key); got != "acme's contract" {
		t.Fatalf("acme's bytes now read %q", got)
	}
	// And the names the bucket holds carry each scope's own prefix, which is the
	// one place a reviewer can check the composition rather than trusting it.
	want := []string{acme.String() + "/" + key.String(), globex.String() + "/" + key.String()}
	seen := strings.Join(store.names(t), " ")
	for _, name := range want {
		if !strings.Contains(seen, name) {
			t.Errorf("the store holds no object named %s; the bucket reads %s", name, seen)
		}
	}
}

// TestAGrantIsAPresignedReadOntoOneTenantsObject is the brief's own sentence — a
// private byte served by a signed URL with no Go request in the path — asked of
// the store rather than of a mock: the URL is minted here and read by an ordinary
// HTTP GET that this process answers with nothing but the response it got.
func TestAGrantIsAPresignedReadOntoOneTenantsObject(t *testing.T) {
	store := s3Store(t)
	scope, ctx := scopeFor(t, "private")
	f := &contracts.File{
		Base:        entity.Base{ID: uuid.New()},
		Name:        "contract.txt",
		ContentType: "text/plain",
		Size:        14,
		SHA256:      strings.Repeat("0", 64),
		StorageKey:  uuid.NewString(),
		Visibility:  contracts.VisibilityPrivate,
	}
	if err := store.storage.Put(ctx, scope, contracts.Key(f.StorageKey), strings.NewReader("a private byte"), 14,
		contracts.MetaFor(f)); err != nil {
		t.Fatalf("put: %v", err)
	}
	grant, err := store.signer.Sign(ctx, scope, f, time.Hour)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if grant.ExpiresAt.Before(time.Now().UTC().Add(time.Hour - time.Minute)) {
		t.Errorf("the grant expires at %s, which is not an hour out", grant.ExpiresAt)
	}
	res, body := s3Fetch(t, grant.URL)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("the presigned read answered %d: %s", res.StatusCode, body)
	}
	if string(body) != "a private byte" {
		t.Errorf("the grant served %q, want the bytes the row names", body)
	}
	if got := res.Header.Get("Content-Type"); got != "text/plain" {
		t.Errorf("the grant served Content-Type %q: the object carries the type contracts.MetaFor wrote at Put, and a client reading the bucket straight is served whatever the object says", got)
	}
	// A grant minted in another tenant's scope is a signature over a different
	// name and serves nothing: the reach comes from the store's own authorization,
	// not from a token this module invented.
	otherScope, otherCtx := scopeFor(t, "other")
	otherGrant, err := store.signer.Sign(otherCtx, otherScope, f, time.Hour)
	if err != nil {
		t.Fatalf("sign in another tenant's scope: %v", err)
	}
	if otherGrant.URL == grant.URL {
		t.Fatal("two tenants' scopes signed the same object name")
	}
	otherRes, otherBody := s3Fetch(t, otherGrant.URL)
	if otherRes.StatusCode == http.StatusOK {
		t.Errorf("a grant signed under another tenant's scope served the object (%s): the prefix is not in the signature", otherBody)
	}
}

// TestAnS3CallWithNoScopeIsRefused: an object is always some tenant's, so a scope
// nobody resolved is an error that writes nothing rather than an object at the
// root of the bucket — and a key this module could not have minted never reaches
// the store at all.
func TestAnS3CallWithNoScopeIsRefused(t *testing.T) {
	store := s3Store(t)
	none := contracts.Scope{}
	key := contracts.Key(uuid.NewString())
	if err := store.storage.Put(t.Context(), none, key, strings.NewReader("x"), 1, contracts.Meta{}); err == nil {
		t.Error("a Put with no tenant was accepted")
	}
	if _, err := store.storage.Get(t.Context(), none, key); err == nil {
		t.Error("a Get with no tenant answered")
	}
	if err := store.storage.Delete(t.Context(), none, key); err == nil {
		t.Error("a Delete with no tenant answered")
	}
	scope, scoped := scopeFor(t, "shapes")
	for _, shape := range []contracts.Key{"../../etc/passwd", "NOT-A-UUID", "", contracts.Key(strings.ToUpper(uuid.NewString()))} {
		if err := store.storage.Put(scoped, scope, shape, strings.NewReader("x"), 1, contracts.Meta{}); err == nil {
			t.Errorf("a key of %q was accepted", shape)
		}
	}
	if got := store.names(t); len(got) != 0 {
		t.Errorf("the refused writes left %v in the bucket", got)
	}
}

func s3Read(t *testing.T, store *s3, scope contracts.Scope, ctx context.Context, key contracts.Key) string {
	t.Helper()
	body, err := store.storage.Get(ctx, scope, key)
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	defer body.Close()
	raw, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return string(raw)
}

// s3Fetch reads a minted URL the way any client would, and returns the response
// and its body.
func s3Fetch(t *testing.T, url string) (*http.Response, []byte) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("the minted URL is not one an HTTP client can read: %v", err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatalf("read the response body: %v", err)
	}
	return res, body
}
