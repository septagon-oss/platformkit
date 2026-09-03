package filetest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// Fake is contracts.Service over a map of rows and whatever Storage it is
// given: the same rules, no database, no transaction. A consumer that wants to
// test what it does when a file is uploaded takes one of these and a Memory.
//
// It ignores the transaction it is handed, and that is the honest limit of it:
// it cannot tell a caller that a write did not commit, because nothing here
// commits. What it does keep is the order the real service keeps — bytes first,
// row second, and the bytes removed again when the row is refused — because
// that order is the whole design and a fake that got it wrong would let a
// consumer test against a module that does not exist.
type Fake struct {
	mu        sync.Mutex
	storage   contracts.Storage
	max       int64
	rows      map[uuid.UUID]contracts.File
	published []string
}

// NewFake returns a file module over storage, accepting uploads up to max.
func NewFake(storage contracts.Storage, max int64) *Fake {
	return &Fake{storage: storage, max: max, rows: map[uuid.UUID]contracts.File{}}
}

var _ contracts.Service = (*Fake)(nil)

// Published is the names of the events the fake would have emitted.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.published)
}

// Upload mirrors internal.Service.Upload.
func (f *Fake) Upload(ctx context.Context, _ db.Tx[db.Tenant], up contracts.Upload) (*contracts.File, error) {
	key := uuid.NewString()
	digest := sha256.New()
	counted := &counter{r: io.LimitReader(io.TeeReader(up.Body, digest), f.max+1)}
	if err := f.storage.Put(ctx, key, counted, up.Declared); err != nil {
		return nil, err
	}
	if counted.n > f.max {
		_ = f.storage.Delete(ctx, key)
		return nil, fmt.Errorf("%w: %d bytes is past the %d this deployment accepts", contracts.ErrTooLarge, counted.n, f.max)
	}
	if err := contracts.Agrees(up.ContentType, counted.head[:min(counted.n, int64(len(counted.head)))]); err != nil {
		_ = f.storage.Delete(ctx, key)
		return nil, err
	}
	row := contracts.File{
		Base: crud.Base{ID: uuid.New(), CreatedAt: db.Now(), UpdatedAt: db.Now()},
		Name: up.Name, ContentType: up.ContentType, Visibility: up.Visibility,
		Size: counted.n, SHA256: hex.EncodeToString(digest.Sum(nil)), StorageKey: key,
	}
	if err := row.Validate(ctx); err != nil {
		_ = f.storage.Delete(ctx, key)
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows[row.ID] = row
	f.published = append(f.published, contracts.EventUploaded)
	out := row
	return &out, nil
}

// Open mirrors internal.Service.Open.
func (f *Fake) Open(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID, anonymous bool) (*contracts.File, io.ReadCloser, error) {
	f.mu.Lock()
	row, ok := f.rows[id]
	f.mu.Unlock()
	if !ok || (anonymous && !row.Public()) {
		return nil, nil, crud.ErrNotFound
	}
	body, err := f.storage.Get(ctx, row.StorageKey)
	if err != nil {
		return nil, nil, err
	}
	return &row, body, nil
}

// Delete mirrors internal.Service.Delete: the row goes and the bytes do not.
func (f *Fake) Delete(_ context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.File, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok {
		return nil, crud.ErrNotFound
	}
	delete(f.rows, id)
	f.published = append(f.published, contracts.EventDeleted)
	return &row, nil
}

// counter counts what is read through it, the way internal.counter does.
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
