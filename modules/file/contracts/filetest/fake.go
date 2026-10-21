package filetest

import (
	"bytes"
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
	quota     int64
	rows      map[uuid.UUID]contracts.File
	uses      []contracts.FileUse
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

// WithQuota gives this fake the disk one tenant may fill, which is the other
// half of what a deployment decides: one ceiling answers two questions — the
// meter's number and the upload's refusal — and they are the same sum, computed
// by Quota and charged by Upload here exactly as internal.Service does it. A
// ceiling of zero, the default, means no ceiling.
//
// What the fake still cannot do is charge for other widths: it encodes no
// variants, so its Used is the frames' own bytes. That half of the sum is
// internal's to answer, and the case that answers it lives there.
func (f *Fake) WithQuota(bytes int64) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.quota = bytes
	return f
}

var _ contracts.Service = (*Fake)(nil)

// Uses and SetUses are the same two decisions the SQL service makes, made over
// a slice: contracts.CollapseRefs and contracts.DiffUses decide what a body's
// references mean and which rows that changes, and what differs between this
// file and internal/service.go is how a row is kept, never what is kept.
//
// What the fake cannot do is pretend at a second tenant: it stores each use
// under the scope its context resolved, and reads them back through the same
// scope, so a use written for one tenant is invisible to another. It is the
// file rows themselves that carry no tenant here, because the map has never
// keyed one — which is the honest limit already stated at the top of this file,
// and why the case that refuses a second tenant's file lives in
// internal, against the row-level policy that decides it for real.

// Uses is the live list of one file.
func (f *Fake) Uses(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID) ([]contracts.UseRow, error) {
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.rows[fileID]; !ok {
		return nil, fmt.Errorf("%w: %s", crud.ErrNotFound, fileID)
	}
	out := []contracts.UseRow{}
	for _, row := range f.uses {
		if row.FileID != fileID || row.TenantID != scope.TenantID() {
			continue
		}
		out = append(out, contracts.UseRow{
			Use:       contracts.Use{Module: row.Module, Entity: row.Entity, Record: row.Record, Field: row.Field, Locale: row.Locale},
			ID:        row.ID,
			CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

// SetUses rewrites one field's uses, in the order the SQL service uses them:
// decide, refuse a file the new set names that is not there, then write. A
// file this rewrite only ends is allowed to be gone already — see the case in
// conformance.go that holds both implementations to that.
func (f *Fake) SetUses(ctx context.Context, tx db.Tx[db.Tenant], use contracts.Use, refs []uuid.UUID) ([]uuid.UUID, error) {
	return f.setUses(ctx, tx, use, refs, false)
}

// SetUsesShown mirrors internal.Service.SetUsesShown: the rows this call files
// carry the audience the record asserts, and the audience a stranger may read each
// of those files with is the ledger's answer, asked again beside the rows.
func (f *Fake) SetUsesShown(ctx context.Context, tx db.Tx[db.Tenant], use contracts.Use, refs []uuid.UUID, public bool) ([]uuid.UUID, error) {
	return f.setUses(ctx, tx, use, refs, public)
}

var _ contracts.ShownUses = (*Fake)(nil)

// OrderUses answers contracts.UsesOrder so that a caller wired to this fake is told
// the same save-wide pass the SQL service is asked for. The fake keeps no rows and so
// takes no locks: it holds one mutex for the whole service, which is the serialisation
// the ledger buys with its ascending FOR UPDATE sweep. What the pass is for — that the
// whole save arrives in one call before any field of it is filed — is asserted where
// the pass is called, in kit/rest.
func (f *Fake) OrderUses(_ context.Context, _ db.Tx[db.Tenant], _ contracts.Use, _ []uuid.UUID) error {
	return nil
}

var _ contracts.UsesOrder = (*Fake)(nil)

// setUses is both of the ledger's doors in one: it files the set of files a body
// references and, when the last public use of a file ends, hands back the files whose
// grace period starts now.
//
// Who may read each file the rewrite touches is asked before any of it is written and
// again with the rows down, and the flips between the two answers are published between
// the two reads. Asked only where it can move at all — a public audience being filed, or
// a row that asserted one being ended — which is internal.Service's rule too, and the
// draft that renames pictures in a private record pays for neither read. Neither half is
// written onto a file row: what a publish asserted stays in the use row that asserts it,
// and what the file was given stays where its upload put it. That is what lets two
// records that share two pictures be saved at all: no file row is locked by a save that
// only names it. The whole rewrite holds f.mu.
func (f *Fake) setUses(ctx context.Context, tx db.Tx[db.Tenant], use contracts.Use, refs []uuid.UUID, public bool) ([]uuid.UUID, error) {
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		return nil, err
	}
	wanted := contracts.CollapseRefs(refs)
	f.mu.Lock()
	defer f.mu.Unlock()
	// Validate the entire desired set before changing any use.
	if err := f.validateUseFiles(wanted); err != nil {
		return nil, err
	}
	from := f.usedFiles(scope, use)
	add, remove := contracts.DiffUses(from, wanted)
	was := f.useAudiences(wanted, remove, public)
	f.rewriteUses(scope, use, add, remove, public)
	if len(remove) == 0 {
		// Nothing ended, so no clock started — but a public audience still asks who may
		// read the files it names, which is the published page that gained its first
		// picture. The answer lives in the use rows this rewrite just wrote.
		f.settleAudience(was)
		return nil, nil
	}
	unused := f.startUnused(scope, remove)
	f.settleAudience(was)
	return unused, nil
}

// SetShown mirrors internal.Service: the audience lands on this record's rows,
// a publish that names a file the record shows no use of is refused, and each
// file's visibility is recomputed off the whole ledger rather than off the rows
// this call touched. A file with no use rows keeps whatever its upload asked for.
func (f *Fake) SetShown(ctx context.Context, tx db.Tx[db.Tenant], owner contracts.Use, refs []uuid.UUID, public bool) error {
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		return err
	}
	wanted := contracts.CollapseRefs(refs)
	f.mu.Lock()
	defer f.mu.Unlock()
	mine := func(row contracts.FileUse) bool {
		return row.TenantID == scope.TenantID() && row.Module == owner.Module &&
			row.Entity == owner.Entity && row.Record == owner.Record
	}
	if len(wanted) == 0 {
		for _, row := range f.uses {
			if mine(row) && !slices.Contains(wanted, row.FileID) {
				wanted = append(wanted, row.FileID)
			}
		}
	}
	for _, id := range wanted {
		if _, ok := f.rows[id]; !ok {
			return fmt.Errorf("%w: %s is not a file of this tenant", crud.ErrNotFound, id)
		}
	}
	shown := map[uuid.UUID]bool{}
	for _, row := range f.uses {
		if mine(row) && slices.Contains(wanted, row.FileID) {
			shown[row.FileID] = true
		}
	}
	if public {
		for _, id := range wanted {
			if !shown[id] {
				return fmt.Errorf("%w: %s — save the body that names it, then publish", contracts.ErrNotShown, id)
			}
		}
	}
	// Who may read each named file, asked before the rows say anything new: the flip
	// this command publishes is the difference between this answer and the one asked
	// below.
	was := map[uuid.UUID]bool{}
	for _, id := range wanted {
		was[id] = f.readsPublicly(id)
	}
	for i, row := range f.uses {
		if mine(row) && (len(refs) == 0 || slices.Contains(refs, row.FileID)) {
			f.uses[i].Public = public
		}
	}
	f.settleAudience(was)
	return nil
}

// settleAudience publishes the audience flips between was and the answer now. It is
// internal.Service's rule on this fake's memory: who may read a file is what its upload
// gave it widened by the records showing it to a stranger, the widened half lives only
// in the use row that asserts it, the visibility a file was given is the floor no
// command here takes back, a revocation puts the row's own visibility back to private
// beside the event that says so, a flip is one event, and an audience that did not move
// records nothing. The caller holds f.mu.
func (f *Fake) settleAudience(was map[uuid.UUID]bool) {
	ids := make([]uuid.UUID, 0, len(was))
	for id := range was {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for _, id := range ids {
		now := f.readsPublicly(id)
		if was[id] && !now {
			// The audience a record asserted went with the record. Only ever
			// downward, and only to the floor: internal.Service writes the same row
			// in the same transaction as the event below.
			if row, ok := f.rows[id]; ok && !row.GivenPublic() && row.Public() {
				row.Visibility = contracts.VisibilityPrivate
				f.rows[id] = row
			}
		}
		if now == was[id] {
			continue
		}
		f.published = append(f.published, contracts.EventVisibility)
	}
}

// readsPublicly is the answer the door gives: the visibility a file was given, or the
// audience a record with a public face is showing it to. internal.Service asks the
// same question of the two tables it reads it from; see its whoReads. The caller
// holds f.mu.
func (f *Fake) readsPublicly(id uuid.UUID) bool {
	return f.givenPublicly(id) || f.shownPublicly(id)
}

func (f *Fake) givenPublicly(id uuid.UUID) bool {
	row, ok := f.rows[id]
	return ok && row.GivenPublic()
}

func (f *Fake) shownPublicly(id uuid.UUID) bool {
	for _, u := range f.uses {
		if u.FileID == id && u.Public {
			return true
		}
	}
	return false
}

// Forget drops one file row past every rule Delete keeps. It stands in for the
// two doors that remove a file without consulting the ledger — the erasure door
// and the retention sweep — which is how a case gets at the use whose file is
// already gone without Delete refusing to create that state.
func (f *Fake) Forget(id uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rows, id)
}

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
	pass, err := f.uploadImage(ctx, scope, key, up, counted.head[:min(counted.n, int64(len(counted.head)))])
	if err != nil {
		return nil, err
	}
	size, digestHex, contentType := counted.n, hex.EncodeToString(digest.Sum(nil)), up.ContentType
	var width, height int
	if pass != nil {
		sum := sha256.Sum256(pass.Bytes)
		size, digestHex, contentType = int64(len(pass.Bytes)), hex.EncodeToString(sum[:]), pass.ContentType
		width, height = pass.Width, pass.Height
		// A key of its own, exactly as the SQL service does it: Storage is
		// write-once, and the streamed object is left for the orphan sweep.
		reencoded := contracts.Key(uuid.NewString())
		meta := contracts.MetaFor(&contracts.File{ContentType: contentType, Visibility: up.Visibility})
		if err := f.storage.Put(ctx, scope, reencoded, bytes.NewReader(pass.Bytes), size, meta); err != nil {
			_ = f.storage.Delete(ctx, scope, key)
			return nil, err
		}
		_ = f.storage.Delete(ctx, scope, key)
		key = reencoded
	}
	if _, err := open(ctx); err != nil {
		_ = f.storage.Delete(ctx, scope, key)
		return nil, err
	}
	row := contracts.File{
		Base: crud.Base{ID: uuid.New(), CreatedAt: db.Now(), UpdatedAt: db.Now()},
		Name: up.Name, ContentType: contentType, Visibility: up.Visibility, Kind: up.Kind,
		Size: size, SHA256: digestHex, StorageKey: key.String(), Width: width, Height: height,
		// Both columns, the same word: what the upload was given is the floor no
		// later audience takes back, and it is recorded here rather than derived.
		UploadVisibility: up.Visibility,
	}
	if err := row.Validate(ctx); err != nil {
		_ = f.storage.Delete(ctx, scope, key)
		return nil, fmt.Errorf("%w: %s", crud.ErrInvalid, err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// The ceiling is asked of the same sum Quota answers with, under this fake's
	// own mutex, which stands in for the advisory lock the real service takes on
	// the tenant: the read and the insert are one step, and twenty uploads that
	// start together cannot each read the same total and each decide there is
	// room. Refused, the bytes go back the way they came.
	if f.quota > 0 {
		var used int64
		for _, other := range f.rows {
			used += other.Size
		}
		if size > f.quota-used {
			_ = f.storage.Delete(ctx, scope, key)
			return nil, fmt.Errorf("%w: %d bytes is past the %d this tenant has left of %d",
				contracts.ErrQuota, size, max(f.quota-used, 0), f.quota)
		}
	}
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
	if ok && anonymous && !row.GivenPublic() && f.readsPublicly(id) {
		// The ledger answered for a row its upload was not given to the world, so the
		// row the caller is handed says so too: what is served, what the response
		// caches and what the caller reports about the file are one answer.
		// internal.Service does the same to the row it read.
		given := row
		given.Visibility = contracts.VisibilityPublic
		row = given
	}
	may := ok && (!anonymous || f.readsPublicly(id))
	f.mu.Unlock()
	if !ok || !may {
		return nil, nil, crud.ErrNotFound
	}
	body, err := f.storage.Get(ctx, scope, contracts.Key(row.StorageKey))
	if err != nil {
		return nil, nil, err
	}
	return &row, body, nil
}

// Delete mirrors internal.Service.Delete: the row goes, the bytes do not, and a
// file some record still shows is refused with the same count the SQL service
// counts — contracts.DistinctRecords over the same rows the details panel lists.
func (f *Fake) Delete(ctx context.Context, _ db.Tx[db.Tenant], id uuid.UUID) (*contracts.File, error) {
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	row, ok := f.rows[id]
	if !ok {
		return nil, crud.ErrNotFound
	}
	var uses []contracts.UseRow
	for _, use := range f.uses {
		if use.TenantID == scope.TenantID() && use.FileID == id {
			uses = append(uses, contracts.UseRow{
				Use:       contracts.Use{Module: use.Module, Entity: use.Entity, Record: use.Record, Field: use.Field, Locale: use.Locale},
				ID:        use.ID,
				CreatedAt: use.CreatedAt,
			})
		}
	}
	if records := contracts.DistinctRecords(uses); records > 0 {
		return nil, &contracts.InUse{File: id, Records: records}
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
	// The same question the public door answers, so a link is never minted onto
	// bytes anybody can already have: a file a published page shows is served at its
	// public address whether or not its own row says so.
	if f.readsPublicly(id) {
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
