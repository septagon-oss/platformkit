package internal

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/septagon-oss/platformkit/kit/tenancy"
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

// S3 is contracts.Storage on an S3-compatible object store, with the tenant in
// every object's key.
//
// The contract's keys are UUIDs this module mints, and Local keeps them in one
// directory for the whole installation. An object store shared by every tenant
// of a shared-instance deployment (decision 0028) cannot work that way: a key a
// tenant learns must not open another tenant's bytes. So the adapter never uses
// a key on its own. Every object is `<tenant id>/<key>`, the tenant is read from
// the request's own context — the tenant the host resolved to, or the one an
// event handler's transaction was opened in — and a call that carries no tenant
// is refused rather than written somewhere nobody owns. A UUID copied from one
// tenant's row therefore names nothing in another tenant's prefix.
//
// It does not implement contracts.Lister: the reconciliation sweep compares the
// store with rows and runs for the installation, not for a tenant, and an
// installation-wide listing of a shared bucket is exactly the reach this adapter
// exists to refuse. Orphans left by a failed upload are reclaimed by a bucket
// lifecycle rule instead (see modules/file/README.md).
type S3 struct {
	client *minio.Client
	bucket string
}

// NewS3 connects to the store. It asks nothing of the network; the first Put
// is where an unreachable endpoint or a missing bucket becomes an error.
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

var _ contracts.Storage = (*S3)(nil)

// object is the key's place in the bucket: under the tenant of ctx, and only a
// key this module could have minted.
func (s *S3) object(ctx context.Context, k string) (string, error) {
	if !key.MatchString(k) {
		return "", fmt.Errorf("file: %q is not a storage key; a key is a UUID", k)
	}
	t, ok := tenancy.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("file: storage key %s was used with no tenant; an object is always some tenant's", k)
	}
	return t.ID.String() + "/" + k, nil
}

// Put writes the object, refusing a key that already exists in this tenant's
// prefix: a key is minted per upload, so a collision is a bug and not a
// replacement. The refusal is the store's own conditional write, so two
// concurrent writers cannot both win.
func (s *S3) Put(ctx context.Context, k string, r io.Reader, size int64) error {
	name, err := s.object(ctx, k)
	if err != nil {
		return err
	}
	opts := minio.PutObjectOptions{ContentType: "application/octet-stream"}
	opts.SetMatchETagExcept("*")
	if _, err := s.client.PutObject(ctx, s.bucket, name, r, size, opts); err != nil {
		if resp := minio.ToErrorResponse(err); resp.StatusCode == http.StatusPreconditionFailed {
			return fmt.Errorf("file: storage key %s already holds bytes", k)
		}
		return fmt.Errorf("file: store %s: %w", k, err)
	}
	return nil
}

// Get opens the object in this tenant's prefix, or ErrNoBlob when there is none.
func (s *S3) Get(ctx context.Context, k string) (io.ReadCloser, error) {
	name, err := s.object(ctx, k)
	if err != nil {
		return nil, err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, name, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("file: open %s: %w", k, err)
	}
	// GetObject is lazy; Stat is the request that finds out whether it is there.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil, contracts.ErrNoBlob
		}
		return nil, fmt.Errorf("file: open %s: %w", k, err)
	}
	return obj, nil
}

// Delete removes the object in this tenant's prefix. A key with nothing at it is
// not an error, because the worker that calls this retries.
func (s *S3) Delete(ctx context.Context, k string) error {
	name, err := s.object(ctx, k)
	if err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, name, minio.RemoveObjectOptions{}); err != nil {
		if minio.ToErrorResponse(err).Code == "NoSuchKey" {
			return nil
		}
		return fmt.Errorf("file: remove %s: %w", k, err)
	}
	return nil
}
