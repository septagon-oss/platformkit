package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// S3Config is where an S3-compatible object store is and how to reach it: any
// store that speaks the S3 API — AWS S3, Garage, SeaweedFS, Ceph RGW — through
// the minio-go client.
type S3Config struct {
	Endpoint  string // host:port, no scheme
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
	Secure    bool // https to the endpoint
}

// S3 is contracts.Storage and contracts.Signer on an S3-compatible object
// store, with the tenant in every object's name.
//
// A bucket is shared by every tenant of a shared-instance deployment (decision
// 0028), so the adapter never uses a key on its own: it asks the scope for the
// name with contracts.Scope.ObjectName, which is the one place a prefix and a
// key are joined, and a key has no way to name a prefix that is not its own
// tenant's. A UUID copied out of one tenant's row therefore names nothing in
// another tenant's prefix — not its bytes, not a collision, not a deletion.
//
// Two of the port's optional doors are not implemented here, and both absences
// are the module's declared readings rather than silent ones.
//
//   - contracts.Reconciler: enumerating a shared bucket is an installation-wide
//     read, and the sweep that would do it runs for the installation rather than
//     a tenant. Refusing it costs the orphan sweep, so an object left by an
//     upload whose transaction then failed is reclaimed by a bucket lifecycle
//     rule instead (modules/file/README.md), which is the same answer a store
//     behind somebody else's API gets.
//   - contracts.Prover: an erasure's proof row is still written and its
//     verified_at stays NULL, which is the difference this module draws between
//     "we checked" and "we assume". A deployment that wants the check counts
//     versions, delete markers and abandoned multipart parts under the tenant's
//     prefix; the port has the shape for it and this adapter does not yet.
//
// What it does add over Local is the door Local cannot: Sign, which hands out a
// time-limited read onto one object with no request of this process in the path.
type S3 struct {
	client *minio.Client
	bucket string
}

var (
	_ contracts.Storage = (*S3)(nil)
	_ contracts.Signer  = (*S3)(nil)
)

// NewS3 connects to the store. It asks nothing of the network; the first Put is
// where an unreachable endpoint or a missing bucket becomes an error.
func NewS3(cfg S3Config) (*S3, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, errors.New("file: an S3 store needs an endpoint and a bucket")
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.Secure,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("file: connect to %s: %w", cfg.Endpoint, err)
	}
	return &S3{client: client, bucket: cfg.Bucket}, nil
}

// Put writes the object under the scope's own prefix, refusing a key that
// already exists there: a key is minted per upload, so a collision is a bug and
// not a replacement. The refusal is the store's own conditional write, so two
// concurrent writers cannot both win — which is why the length has to be known
// up front. An unknown length goes multipart, a conditional header does not
// cover a multipart upload as a whole, and the promise this port's Put makes is
// the one an S3 conditional Put keeps.
func (s *S3) Put(ctx context.Context, scope contracts.Scope, key contracts.Key, r io.Reader, size int64, meta contracts.Meta) error {
	name, err := scope.ObjectName(key)
	if err != nil {
		return err
	}
	if size < 0 {
		return fmt.Errorf("file: %s arrived with no length: this store refuses an object it cannot count before it writes it, because %s",
			key, "a conditional Put — the write that refuses an existing key — does not cover a multipart upload")
	}
	opts := minio.PutObjectOptions{ContentType: orDefault(meta.ContentType, "application/octet-stream")}
	if meta.CacheControl != "" {
		opts.CacheControl = meta.CacheControl
	}
	if meta.ContentDisposition != "" {
		opts.ContentDisposition = meta.ContentDisposition
	}
	opts.SetMatchETagExcept("*")
	if _, err := s.client.PutObject(ctx, s.bucket, name, r, size, opts); err != nil {
		if resp := minio.ToErrorResponse(err); resp.StatusCode == http.StatusPreconditionFailed {
			return fmt.Errorf("file: storage key %s already holds bytes", key)
		}
		return fmt.Errorf("file: store %s: %w", key, err)
	}
	return nil
}

// Get opens the object in the scope's own prefix, or ErrNoBlob when there is
// none.
func (s *S3) Get(ctx context.Context, scope contracts.Scope, key contracts.Key) (io.ReadCloser, error) {
	name, err := scope.ObjectName(key)
	if err != nil {
		return nil, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, name, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("file: open %s: %w", key, err)
	}
	// GetObject is lazy; Stat is the request that finds out whether it is there.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, contracts.ErrNoBlob
		}
		return nil, fmt.Errorf("file: open %s: %w", key, err)
	}
	return obj, nil
}

// Delete removes the object in the scope's own prefix. A key with nothing at it
// is not an error, because the worker that calls this retries.
func (s *S3) Delete(ctx context.Context, scope contracts.Scope, key contracts.Key) error {
	name, err := scope.ObjectName(key)
	if err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, name, minio.RemoveObjectOptions{}); err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil
		}
		return fmt.Errorf("file: remove %s: %w", key, err)
	}
	return nil
}

// Sign is the store's own presigned GET: an S3 SigV4 query over the object name
// the scope implies, valid until expiry. Nothing is written and nothing is
// revoked by removing the bytes — a grant is a bearer capability until its
// expiry, which is why the module caps how long one may be.
//
// The metadata travels on the object rather than on the query, because
// contracts.MetaFor wrote it there at Put: a client that reads the URL straight
// off the bucket gets the same Content-Type, Cache-Control and
// Content-Disposition contracts/response.go would have set on a response this
// process served. A store that ignores the object's own metadata therefore serves
// a grant with the wrong headers on it, and that is the store's conformance to
// fail, not this adapter's to paper over.
func (s *S3) Sign(ctx context.Context, scope contracts.Scope, f *contracts.File, expiry time.Duration) (*contracts.Grant, error) {
	name, err := scope.ObjectName(contracts.Key(f.StorageKey))
	if err != nil {
		return nil, err
	}
	if expiry <= 0 {
		return nil, fmt.Errorf("%w: a grant signed for %s is already expired", contracts.ErrInvalidExpiry, expiry)
	}
	url, err := s.client.PresignedGetObject(ctx, s.bucket, name, expiry, nil)
	if err != nil {
		return nil, fmt.Errorf("file: sign a grant for %s: %w", f.ID, err)
	}
	return &contracts.Grant{URL: url.String(), ExpiresAt: time.Now().UTC().Add(expiry)}, nil
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}
