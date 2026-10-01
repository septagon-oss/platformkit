package filetest

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"time"

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
	holds     map[uuid.UUID]*contracts.Hold
	published []string
}

// NewFake returns a file module over storage, accepting uploads up to max.
func NewFake(storage contracts.Storage, max int64) *Fake {
	return &Fake{
		storage: storage, max: max,
		rows: map[uuid.UUID]contracts.File{}, holds: map[uuid.UUID]*contracts.Hold{},
	}
}

var _ contracts.Service = (*Fake)(nil)

// Published is the names of the events the fake would have emitted.
func (f *Fake) Published() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.published)
}

// Upload mirrors internal.Service.Upload, including the order: the bytes are
// stored before the transaction is asked for, because a body arrives at the
// client's pace and nothing may be open while it does.
func (f *Fake) Upload(ctx context.Context, open contracts.Tx, up contracts.Upload) (*contracts.File, error) {
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		return nil, err
	}
	key := contracts.Key(uuid.NewString())
	digest := sha256.New()
	counted := &counter{r: io.LimitReader(io.TeeReader(up.Body, digest), f.max+1)}
	meta := contracts.MetaFor(&contracts.File{ContentType: up.ContentType, Visibility: up.Visibility})
	if err := f.storage.Put(ctx, scope, key, counted, up.Declared, meta); err != nil {
		return nil, err
	}
	if counted.n > f.max {
		_ = f.storage.Delete(ctx, scope, key)
		return nil, fmt.Errorf("%w: %d bytes is past the %d this deployment accepts", contracts.ErrTooLarge, counted.n, f.max)
	}
	if err := contracts.Agrees(up.ContentType, counted.head[:min(counted.n, int64(len(counted.head)))]); err != nil {
		_ = f.storage.Delete(ctx, scope, key)
		return nil, err
	}
	if _, err := open(ctx); err != nil {
		_ = f.storage.Delete(ctx, scope, key)
		return nil, err
	}
	row := contracts.File{
		Base: crud.Base{ID: uuid.New(), CreatedAt: db.Now(), UpdatedAt: db.Now()},
		Name: up.Name, ContentType: up.ContentType, Visibility: up.Visibility, Kind: up.Kind,
		Size: counted.n, SHA256: hex.EncodeToString(digest.Sum(nil)), StorageKey: key.String(),
	}
	if err := row.Validate(ctx); err != nil {
		_ = f.storage.Delete(ctx, scope, key)
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
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		return nil, nil, err
	}
	f.mu.Lock()
	row, ok := f.rows[id]
	f.mu.Unlock()
	if !ok || (anonymous && !row.Public()) {
		return nil, nil, crud.ErrNotFound
	}
	body, err := f.storage.Get(ctx, scope, contracts.Key(row.StorageKey))
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

// Grant mirrors internal.Service.Grant, including the two refusals a caller
// distinguishes: a public file needs no grant, and a store that cannot sign one
// does not pretend to.
func (f *Fake) Grant(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, expiry time.Duration) (*contracts.Grant, error) {
	switch {
	case expiry <= 0:
		expiry = contracts.DefaultGrantExpiry
	case expiry > contracts.MaxGrantExpiry:
		return nil, fmt.Errorf("%w: %s is longer than the %s this module signs", contracts.ErrInvalidExpiry, expiry, contracts.MaxGrantExpiry)
	}
	f.mu.Lock()
	row, ok := f.rows[id]
	f.mu.Unlock()
	if !ok {
		return nil, crud.ErrNotFound
	}
	if row.Public() {
		return nil, fmt.Errorf("%w: %s is public and is served at its public URL", contracts.ErrPublicFile, id)
	}
	signer, ok := f.storage.(contracts.Signer)
	if !ok {
		return nil, fmt.Errorf("%w: %T was wired as this deployment's store", contracts.ErrNotSignable, f.storage)
	}
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		return nil, err
	}
	return signer.Sign(ctx, scope, &row, expiry)
}

// Retain, Release and EraseSubject mirror internal.Service, minus the row locks
// a map does not have. This fake ignores the transaction it is handed — its own
// doc-comment says that is its honest limit — so the cases that decide whether a
// hold stops a deletion are the Postgres ones, and these exist so that a
// consumer can hold a hold and read the receipt back.
func (f *Fake) Retain(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID, until *time.Time, reason string) (*contracts.Hold, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[fileID]; !ok {
		return nil, crud.ErrNotFound
	}
	hold := &contracts.Hold{
		Base:   crud.Base{ID: uuid.New(), CreatedAt: db.Now(), UpdatedAt: db.Now()},
		FileID: fileID, Until: until, Reason: strings.TrimSpace(reason),
	}
	if err := hold.Validate(ctx); err != nil {
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	f.holds[fileID] = hold
	f.published = append(f.published, contracts.EventRetained)
	return hold, nil
}

// Release removes a hold, and no hold is not an error.
func (f *Fake) Release(_ context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[fileID]; !ok {
		return crud.ErrNotFound
	}
	if _, ok := f.holds[fileID]; !ok {
		return nil
	}
	delete(f.holds, fileID)
	f.published = append(f.published, contracts.EventReleased)
	return nil
}

// EraseSubject removes one subject's rows and refuses the whole erasure if any
// one of them is held, which is the decision the SQL command makes and the one
// a consumer's UI has to be written against.
func (f *Fake) EraseSubject(_ context.Context, tx db.Tx[db.Tenant], subject uuid.UUID, _ string) (*contracts.ErasureReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	receipt := &contracts.ErasureReceipt{Subject: subject, At: db.Now()}
	var ids []uuid.UUID
	for id, row := range f.rows {
		if row.UploaderID == subject {
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return cmp.Compare(a.String(), b.String()) })
	for _, id := range ids {
		if hold, ok := f.holds[id]; ok && hold.Live(db.Now()) {
			return nil, fmt.Errorf("%w: %s is held (%s)", contracts.ErrHeld, id, hold.Reason)
		}
	}
	for _, id := range ids {
		row := f.rows[id]
		delete(f.rows, id)
		delete(f.holds, id)
		f.published = append(f.published, contracts.EventDeleted)
		receipt.Files++
		receipt.Bytes += row.Size
	}
	return receipt, nil
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
