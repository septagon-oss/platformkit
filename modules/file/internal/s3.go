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

// s3PartSize is the largest body this adapter asks of the store as one request;
// above it minio-go splits the write into a multipart upload. The number is
// named here rather than left to the client's own default because it decides
// something this port cares about: a multipart create is not a conditional
// write, so it is the line between a collision the store refuses and one nobody
// would notice.
const s3PartSize int64 = 16 << 20

// Put writes the object under the scope's own prefix, refusing a key that
// already exists there: a key is minted per upload, so a collision is a bug and
// not a replacement.
//
// The refusal is asked of the store rather than checked first, and it is asked
// differently on either side of s3PartSize. A body that fits one request carries
// the store's own conditional create header, so two concurrent writers cannot
// both win. A body that does not — because it is larger than one request, or
// because it arrived as a stream that declared no length, which is what this
// module's upload route honestly declares (contracts.Upload.Declared) — goes as a
// multipart upload, and a conditional header on that request asks nothing the
// store keeps: measured against the stack's own object store, the same
// If-None-Match: * that answers a single-part write with a refusal answered a
// multipart one with success and a replaced object. So such a body claims the
// name first, by a conditional write of nothing at all, and only the winner of
// the claim fills it.
//
// That is two requests where one would do, and it leaves a zero-byte object at a
// name no row yet names while the fill is in flight. Both are the price of taking
// an undeclared length rather than refusing it, and the refusal was the worse
// option: the length a streamed upload can declare is -1, so refusing one is
// refusing the door. A fill that fails releases the claim rather than leaving a
// key shut behind an abandoned attempt.
func (s *S3) Put(ctx context.Context, scope contracts.Scope, key contracts.Key, r io.Reader, size int64, meta contracts.Meta) error {
	name, err := scope.ObjectName(key)
	if err != nil {
		return err
	}
	if size >= 0 && size <= s3PartSize {
		if _, err := s.client.PutObject(ctx, s.bucket, name, r, size, putOptions(meta, true)); err != nil {
			return storeRefused(key, err)
		}
		return nil
	}
	if _, err := s.client.PutObject(ctx, s.bucket, name, emptyReader{}, 0, putOptions(meta, true)); err != nil {
		return storeRefused(key, err)
	}
	if _, err := s.client.PutObject(ctx, s.bucket, name, r, size, putOptions(meta, false)); err != nil {
		_ = s.client.RemoveObject(ctx, s.bucket, name, minio.RemoveObjectOptions{})
		return storeRefused(key, err)
	}
	return nil
}

// emptyReader is the claim: a body of no bytes, written to see whether the name
// is free. It is a type and not strings.NewReader("") so the claim says what it
// is where a reader of Put sees the argument.
type emptyReader struct{}

func (emptyReader) Read([]byte) (int, error) { return 0, io.EOF }

// putOptions is the object's own metadata, as contracts.MetaFor wrote it, plus
// the conditional-create header on the one write that is a create: a fill of a
// name this client already claimed must not be refused by its own claim.
func putOptions(meta contracts.Meta, create bool) minio.PutObjectOptions {
	opts := minio.PutObjectOptions{
		ContentType: orDefault(meta.ContentType, "application/octet-stream"),
		PartSize:    uint64(s3PartSize),
	}
	if meta.CacheControl != "" {
		opts.CacheControl = meta.CacheControl
	}
	if meta.ContentDisposition != "" {
		opts.ContentDisposition = meta.ContentDisposition
	}
	if create {
		opts.SetMatchETagExcept("*")
	}
	return opts
}

// storeRefused is the store's answer put into this port's words. A conditional
// create the store would not run is the collision the port calls a bug; anything
// else stays the error it was, wrapped.
func storeRefused(key contracts.Key, err error) error {
	if minio.ToErrorResponse(err).StatusCode == http.StatusPreconditionFailed {
		return fmt.Errorf("file: storage key %s already holds bytes", key)
	}
	return fmt.Errorf("file: store %s: %w", key, err)
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
