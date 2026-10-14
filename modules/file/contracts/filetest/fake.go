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
// carry the audience the record asserts, and each of them has its visibility
// recomputed off the whole ledger in the same write.
func (f *Fake) SetUsesShown(ctx context.Context, tx db.Tx[db.Tenant], use contracts.Use, refs []uuid.UUID, public bool) ([]uuid.UUID, error) {
	return f.setUses(ctx, tx, use, refs, public)
}

var _ contracts.ShownUses = (*Fake)(nil)

func (f *Fake) setUses(ctx context.Context, tx db.Tx[db.Tenant], use contracts.Use, refs []uuid.UUID, public bool) ([]uuid.UUID, error) {
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		return nil, err
	}
	wanted := contracts.CollapseRefs(refs)
	f.mu.Lock()
	defer f.mu.Unlock()
	// Nothing is written until every id the new set names has been found. That is
	// the whole of the refusal's promise: a body that names one gone file records
	// no uses at all, rather than the uses of the files that happen to still be
	// there. Ids this rewrite is dropping are not asked: they may be gone, and
	// the rows that name them are what the write is here to remove.
	for _, id := range wanted {
		if _, ok := f.rows[id]; !ok {
			return nil, fmt.Errorf("%w: %s is not a file of this tenant", crud.ErrNotFound, id)
		}
	}
	var from []uuid.UUID
	live := map[uuid.UUID]bool{}
	// Who may read each file this rewrite is about to change, asked before any of it
	// is written: the audience is the answer to a question about the ledger, so the
	// rewrite asks it once here and once the rows are down, and publishes the flips
	// between the two. Asked only where it can move at all — a public audience being
	// filed, or a row that asserted one being ended — which is internal.Service's
	// rule too, and the draft that renames pictures in a private record pays for
	// neither read.
	var was map[uuid.UUID]bool
	for _, row := range f.uses {
		if row.TenantID == scope.TenantID() && row.Module == use.Module && row.Entity == use.Entity &&
			row.Record == use.Record && row.Field == use.Field && row.Locale == use.Locale {
			from = append(from, row.FileID)
			live[row.FileID] = true
		}
	}
	add, remove := contracts.DiffUses(from, wanted)
	retract := contracts.CollapseRefs(remove)
	if public || len(retract) > 0 {
		was = map[uuid.UUID]bool{}
		for _, id := range append(append([]uuid.UUID{}, wanted...), remove...) {
			was[id] = f.readsPublicly(id)
		}
	}
	kept := f.uses[:0]
	for _, row := range f.uses {
		if row.TenantID == scope.TenantID() && slices.Contains(remove, row.FileID) &&
			row.Module == use.Module && row.Entity == use.Entity &&
			row.Record == use.Record && row.Field == use.Field && row.Locale == use.Locale {
			continue
		}
		kept = append(kept, row)
	}
	for _, id := range add {
		kept = append(kept, contracts.FileUse{
			Base:   crud.Base{ID: uuid.New(), TenantID: scope.TenantID(), CreatedAt: db.Now(), UpdatedAt: db.Now()},
			FileID: id, Module: use.Module, Entity: use.Entity, Record: use.Record, Field: use.Field, Locale: use.Locale,
			Public: public,
		})
	}
	slices.SortFunc(kept, func(a, b contracts.FileUse) int { return bytes.Compare(a.FileID[:], b.FileID[:]) })
	f.uses = kept
	for _, id := range add {
		// Shown again, so the grace period is over — the same arm internal.Service
		// writes with its UPDATE, run against the row this fake keeps in memory.
		if row, ok := f.rows[id]; ok {
			row.UnusedSince = nil
			f.rows[id] = row
		}
	}
	if len(remove) == 0 {
		// Nothing ended, so no clock started — but a public audience still asks who
		// may read the files it names, which is the published page that gained its
		// first picture. Nothing is held: the answer goes into the use rows this
		// rewrite just wrote, not into the file rows.
		f.settleAudience(was, contracts.Use{Module: use.Module, Entity: use.Entity, Record: use.Record}, nil)
		return nil, nil
	}
	var unused []uuid.UUID
	for _, id := range remove {
		still := false
		for _, row := range f.uses {
			if row.FileID == id && row.TenantID == scope.TenantID() {
				still = true
			}
		}
		if !still {
			unused = append(unused, id)
			if row, ok := f.rows[id]; ok && row.DeletedAt == nil && row.Width > 0 {
				// The grace period starts at the moment the last use went.
				at := db.Now()
				row.UnusedSince = &at
				f.rows[id] = row
			}
		}
	}
	// The audience of every file this rewrite named, asked again with the rows in
	// their new state and published as the flips between the two answers. Held names
	// the files whose use it ended, which internal.Service.SetUses holds FOR UPDATE
	// and may therefore write the answer into; the files it merely began a use of keep
	// their audience in the use row that asserts it. One answer either way — and no
	// file row locked by a save that only names it, which is what lets two records
	// that share two pictures be saved at all.
	f.settleAudience(was, contracts.Use{Module: use.Module, Entity: use.Entity, Record: use.Record}, remove)
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
	// Who may read each named file, asked before the rows say anything new: a
	// publish is one of the two commands allowed to write the answer back into the
	// file row, and the flip it publishes is the difference between this answer and
	// the one asked below.
	was := map[uuid.UUID]bool{}
	for _, id := range wanted {
		was[id] = f.readsPublicly(id)
	}
	for i, row := range f.uses {
		if mine(row) && (len(refs) == 0 || slices.Contains(refs, row.FileID)) {
			f.uses[i].Public = public
		}
	}
	f.settleAudience(was, owner, wanted)
	return nil
}

// settleAudience publishes the audience flips between was and the answer now, and
// writes that answer into the row of every file held. It is internal.Service's rule
// on this fake's memory: who may read a file is its own visibility widened by the
// records showing it to a stranger, a held file is answered from the ledger alone, a
// flip is one event, an audience that did not move records nothing, and a file this
// command did not hold keeps its answer in the use row that asserts it. The caller
// holds f.mu.
func (f *Fake) settleAudience(was map[uuid.UUID]bool, owner contracts.Use, held []uuid.UUID) {
	ids := make([]uuid.UUID, 0, len(was))
	for id := range was {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	for _, id := range ids {
		keep := slices.Contains(held, id)
		read := f.readsPublicly(id)
		if keep {
			read = f.shownPublicly(id)
		}
		if read == was[id] {
			continue
		}
		want := contracts.VisibilityPrivate
		if read {
			want = contracts.VisibilityPublic
		}
		if keep {
			// Written back: rows holds values, so a flip left on the copy this read
			// handed out would publish an event for a visibility no read of this fake
			// ever shows.
			if row, ok := f.rows[id]; ok {
				row.Visibility = want
				f.rows[id] = row
			}
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
	return ok && row.Public()
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
	// The same image pass the SQL service runs — the same function, not a
	// second implementation of it — at the kernel's default ceiling. A
	// deployment's own ceiling is a Deps value, and the case that exercises a
	// deployment that changed one lives in internal, beside the Deps that sets
	// it; what this suite pins is that both implementations of Upload agree
	// about what an image becomes.
	body, err := f.storage.Get(ctx, scope, key)
	if err != nil {
		_ = f.storage.Delete(ctx, scope, key)
		return nil, err
	}
	var pass *contracts.ImagePass
	if contracts.ReadsAsImage(up, counted.head[:min(counted.n, int64(len(counted.head)))]) {
		got, perr := contracts.ProcessImage(body, contracts.DefaultMaxImagePixels)
		if perr != nil {
			err = perr
		} else {
			pass = &got
		}
	}
	_ = body.Close()
	if err != nil {
		// The same rule as the service: a pass that failed refuses and removes
		// the object only when the caller called it an image or asked for more
		// pixels than this deployment decodes.
		if contracts.RefusesPass(err, up.Image) {
			_ = f.storage.Delete(ctx, scope, key)
			return nil, err
		}
		err = nil
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
	if ok && anonymous && !row.Public() && f.readsPublicly(id) {
		// The ledger answered for a row its own visibility says is private, so the row
		// the caller is handed says so too: what is served, what the response caches
		// and what the caller reports about the file are one answer. internal.Service
		// does the same to the row it read.
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
