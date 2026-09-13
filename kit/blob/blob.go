// Package blob describes byte storage independently of file metadata,
// authorization and database transactions. Providers own their byte effects.
package blob

import (
	"context"
	"errors"
	"io"
	"time"
)

// ErrNoBlob identifies a storage key with no bytes. Provider outages must
// remain distinguishable from this missing-value outcome.
var ErrNoBlob = errors.New("file: no bytes at this key")

// Storage owns bytes, without a transaction. The caller chooses the ordering
// of byte effects relative to any metadata commit: a rollback cannot undo a
// blob write. File supplies a lowercase UUID per upload; providers document
// their accepted keys and cancellation behavior.
type Storage interface {
	// Put writes the bytes at key. size is what the caller declared, or -1 when
	// nothing did; an implementation that has to know a length up front may
	// refuse -1, and the one here ignores it. Writing a key that already exists
	// is an error, because a key is minted per upload and a collision is a bug
	// rather than a replacement.
	Put(ctx context.Context, key string, r io.Reader, size int64) error

	// Get opens the bytes at key, or ErrNoBlob when there are none. The caller
	// closes what it is given.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes the bytes at key. A key with nothing at it is not an
	// error: the worker that calls this retries, and a retry that failed
	// because the first attempt succeeded would never stop.
	Delete(ctx context.Context, key string) error
}

// Lister is the half of Storage a reconciliation can be built on, and it is
// optional: an implementation that cannot enumerate what it holds — a signed-URL
// gateway, a store behind somebody else's API — simply does not implement it.
//
// File uses it to reconcile orphans: an upload writes the bytes before the row, so a
// transaction that then fails leaves bytes nobody references. Nothing in the
// database records them, which is why the sweep has to start from the store.
type Lister interface {
	// Keys is every key written before before. The bound is the whole safety
	// argument: an upload in flight has bytes and no row yet, and a sweep that
	// did not exclude it would delete the blob out from under a request that is
	// about to commit.
	Keys(ctx context.Context, before time.Time) ([]string, error)
}
