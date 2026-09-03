// Package internal is every implementation of the file module. Nothing outside
// modules/file can import it, which is the compiler enforcing idea 3.
package internal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// Service is the file lifecycle. It has three fields, unlike the services in
// the other modules: the bytes have to go somewhere, and how large one upload
// may be and how much disk one tenant may hold are a deployment's decisions
// rather than this module's.
type Service struct {
	storage contracts.Storage
	max     int64
	quota   int64
}

// NewService takes the storage the bytes go to, the largest upload this
// deployment accepts, and the disk one tenant may fill. module.go constructs it.
func NewService(storage contracts.Storage, max, quota int64) *Service {
	return &Service{storage: storage, max: max, quota: quota}
}

var _ contracts.Service = (*Service)(nil)

// Upload streams the bytes into storage while hashing and counting them, then
// opens the caller's transaction and writes the row. See contracts.Service.
func (s *Service) Upload(ctx context.Context, open contracts.Tx, up contracts.Upload) (*contracts.File, error) {
	// A UUID and nothing else, which is the whole of the path-traversal
	// argument: there is no caller-supplied component in a key to escape with.
	key := uuid.NewString()
	digest := sha256.New()
	// One byte past the deployment's limit, so "exactly the limit" and "more
	// than it" are distinguishable; the tee hashes exactly what the copy reads.
	// head keeps the first bytes so that what the caller declared can be checked
	// against what actually arrived, without a second pass over the file.
	//
	// Nothing is open while this runs, which is the point of taking an opener:
	// the client sets the pace of a body, and a transaction that waited for one
	// is a connection a byte a second can pin.
	counted := &counter{r: io.LimitReader(io.TeeReader(up.Body, digest), s.max+1)}

	if err := s.storage.Put(ctx, key, counted, up.Declared); err != nil {
		return nil, fmt.Errorf("file: store %s: %w", key, err)
	}
	if counted.n > s.max {
		// Refused, so not charged for. The removal is best effort: what is left
		// behind is disk nobody references, and the answer to the caller is the
		// one that matters.
		_ = s.storage.Delete(ctx, key)
		return nil, fmt.Errorf("%w: %d bytes is past the %d this deployment accepts", contracts.ErrTooLarge, counted.n, s.max)
	}
	if err := contracts.Agrees(up.ContentType, counted.head[:min(counted.n, int64(len(counted.head)))]); err != nil {
		_ = s.storage.Delete(ctx, key)
		return nil, err
	}
	// Every byte is on disk, so there is finally something to open a
	// transaction for — and the quota is measured inside it, under the lock.
	tx, err := open(ctx)
	if err != nil {
		_ = s.storage.Delete(ctx, key)
		return nil, err
	}
	if err := s.charge(ctx, tx, counted.n); err != nil {
		_ = s.storage.Delete(ctx, key)
		return nil, err
	}

	f := &contracts.File{
		Name: up.Name, ContentType: up.ContentType, Visibility: up.Visibility,
		Size: counted.n, SHA256: hex.EncodeToString(digest.Sum(nil)), StorageKey: key,
	}
	if err := crud.Create(ctx, tx, f); err != nil {
		_ = s.storage.Delete(ctx, key)
		return nil, err
	}
	return f, events.Publish(ctx, tx, contracts.EventUploaded, contracts.Uploaded{
		FileID: f.ID, Name: f.Name, ContentType: f.ContentType, Size: f.Size,
		SHA256: f.SHA256, Visibility: f.Visibility, At: db.Now(),
	})
}

// Open is the row and its bytes. See contracts.Service.
func (s *Service) Open(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, anonymous bool) (*contracts.File, io.ReadCloser, error) {
	f, err := crud.Get[*contracts.File](tx, id)
	if err != nil {
		return nil, nil, err
	}
	if anonymous && !f.Public() {
		// Not a 403: a caller who is not signed in learns nothing about what
		// this tenant has, including whether it has this.
		return nil, nil, crud.ErrNotFound
	}
	body, err := s.storage.Get(ctx, f.StorageKey)
	if err != nil {
		// A row that says there are bytes and a store that has none is the one
		// inconsistency the split can produce, and it is an outage rather than
		// something the caller did: it reaches huma as a 500 with this in the log.
		return nil, nil, fmt.Errorf("file: open %s for %s: %w", f.StorageKey, f.ID, err)
	}
	return f, body, nil
}

// Delete removes the row and says where the bytes are. See contracts.Service.
func (s *Service) Delete(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.File, error) {
	f, err := crud.Get[*contracts.File](tx, id)
	if err != nil {
		return nil, err
	}
	// Hard, not soft. A soft-deleted row is a row that still points at bytes,
	// and the bytes are about to go: keeping the row would be keeping a lie.
	if err := crud.Delete[*contracts.File](tx, id, false); err != nil {
		return nil, err
	}
	return f, events.Publish(ctx, tx, contracts.EventDeleted, contracts.Deleted{
		FileID: f.ID, StorageKey: f.StorageKey, Size: f.Size, At: db.Now(),
	})
}

// counter counts what is read through it, which is how the size on the row is
// what actually arrived rather than what the request claimed, and keeps the
// first 512 bytes, which is what http.DetectContentType reads.
type counter struct {
	r    io.Reader
	n    int64
	head [512]byte
}

func (c *counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if c.n < int64(len(c.head)) {
		copy(c.head[c.n:], p[:n])
	}
	c.n += int64(n)
	return n, err
}

// charge refuses n bytes that would take this tenant past its quota.
//
// The usage is a sum over the tenant's own rows rather than a running total in
// a column, and the choice is deliberate. A column has to be kept correct by
// every write — an upload, a delete, the reconciliation below — and a total
// that drifts is a tenant that either cannot upload or is never stopped, with
// nothing to compare against. The sum reads one partial index on a table whose
// rows are counted in thousands, once per upload, and it cannot be wrong.
//
// The lock is what makes the sum mean anything, and it is the fix for what the
// review measured: twenty uploads that started together all read the same total
// before any of them had inserted a row, all decided there was room, and stored
// 2.9 times the quota. pg_advisory_xact_lock is held by this transaction until
// it commits or rolls back, so the read and the insert are one step; it is keyed
// on the tenant, so one customer's uploads never queue behind another's; and it
// needs no table, no row and no migration. It is taken before the sum and never
// after, which is the only ordering rule there is here.
func (s *Service) charge(ctx context.Context, tx db.Tx[db.Tenant], n int64) error {
	if s.quota <= 0 {
		return nil
	}
	lock := "file/quota/" + db.TenantOf(tx).ID.String()
	if err := tx.DB().WithContext(ctx).
		Exec(`SELECT pg_advisory_xact_lock(hashtext(?)::bigint)`, lock).Error; err != nil {
		return fmt.Errorf("file: lock this tenant's quota: %w", err)
	}
	var used int64
	err := tx.DB().Model(&contracts.File{}).Where("deleted_at IS NULL").
		Select("COALESCE(SUM(size), 0)").Scan(&used).Error
	if err != nil {
		return fmt.Errorf("file: what this tenant is holding: %w", err)
	}
	if left := s.quota - used; n > left {
		return fmt.Errorf("%w: %d bytes is past the %d this tenant has left of %d", contracts.ErrQuota, n, max(left, 0), s.quota)
	}
	return nil
}

// RemoveBlob is this module's subscription to its own file.deleted, and the
// reason that event exists.
//
// Removing the bytes cannot happen in the transaction that removed the row: a
// file delete is not something a rollback can undo, so a transaction that failed
// after it would leave the row back and the bytes gone — a download that fails
// forever. An event is the only thing in this architecture that is delivered
// exactly after a commit, so the row's removal publishes where the bytes are and
// this handler removes them.
//
// It is idempotent because kit/events claims each delivery, and idempotent again
// because a key with nothing at it is not an error: a redelivery after a
// half-finished attempt finishes it instead of failing forever.
func RemoveBlob(storage contracts.Storage) events.Subscription {
	return events.Subscription{
		Module: "file",
		Name:   contracts.EventDeleted,
		Handler: func(ctx context.Context, _ db.Tx[db.Tenant], ev events.Event) error {
			var deleted contracts.Deleted
			if err := json.Unmarshal(ev.Payload, &deleted); err != nil {
				return fmt.Errorf("file: read %s: %w", ev.Name, err)
			}
			if deleted.StorageKey == "" {
				return nil
			}
			// An error rolls the handler's transaction back, which releases the
			// claim, which is what makes the next delivery try again.
			return storage.Delete(ctx, deleted.StorageKey)
		},
	}
}
