// Package internal is every implementation of the file module. Nothing outside
// modules/file can import it, which is the compiler enforcing idea 3.
package internal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"

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
	// maxPixels is the frame the image pass will decode; see processImage.
	maxPixels int
}

// NewService takes the storage the bytes go to, the largest upload this
// deployment accepts, the disk one tenant may fill, and the largest frame it
// will decode. A maxPixels of 0 means contracts.DefaultMaxImagePixels.
func NewService(storage contracts.Storage, max, quota int64, maxPixels int) *Service {
	if maxPixels <= 0 {
		maxPixels = contracts.DefaultMaxImagePixels
	}
	return &Service{storage: storage, max: max, quota: quota, maxPixels: maxPixels}
}

var _ contracts.Service = (*Service)(nil)

// Upload streams the bytes into storage while hashing and counting them, then
// opens the caller's transaction and writes the row. See contracts.Service.
func (s *Service) Upload(ctx context.Context, open contracts.Tx, up contracts.Upload) (*contracts.File, error) {
	// The tenant this request resolved. It cannot come from a transaction,
	// because nothing is open yet and nothing may open while the body arrives
	// at the client's pace — which is the whole reason Scope exists beside Tx
	// rather than instead of it.
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		return nil, err
	}
	// A UUID and nothing else, which is the whole of the path-traversal
	// argument: there is no caller-supplied component in a key to escape with.
	key := contracts.Key(uuid.NewString())
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

	// The metadata goes with the bytes rather than with the response, because
	// the response a grant produces has no handler in this process to set one:
	// whatever the object carries is what the client reading it off the store
	// is served. Local ignores it and says so in its own comment.
	meta := contracts.MetaFor(&contracts.File{ContentType: up.ContentType, Visibility: up.Visibility})
	if err := s.storage.Put(ctx, scope, key, counted, up.Declared, meta); err != nil {
		return nil, fmt.Errorf("file: store %s: %w", key, err)
	}
	if counted.n > s.max {
		// Refused, so not charged for. The removal is best effort: what is left
		// behind is disk nobody references, and the answer to the caller is the
		// one that matters.
		_ = s.storage.Delete(ctx, scope, key)
		return nil, fmt.Errorf("%w: %d bytes is past the %d this deployment accepts", contracts.ErrTooLarge, counted.n, s.max)
	}
	if err := contracts.Agrees(up.ContentType, counted.head[:min(counted.n, int64(len(counted.head)))]); err != nil {
		_ = s.storage.Delete(ctx, scope, key)
		return nil, err
	}
	// The image pass (decision 0069 §4): measure the frame, turn it the way the
	// camera said, and store what the pixels re-encode to rather than what
	// arrived. It runs after the bytes are on disk and before anything is open,
	// because a decode is the one step here whose cost belongs to the caller's
	// file rather than to a database connection, and it is the step that can
	// refuse: a frame over the ceiling and a file no decoder reads are both
	// answered with the blob removed and nothing written.
	// The pass, and what the row will therefore carry: the bytes, the digest and
	// the media type of what is stored rather than of what was sent. After this
	// line the two are different objects, and every number on the row belongs to
	// the one a reader is going to fetch.
	pass, err := s.reencode(ctx, scope, up, key, counted.head[:min(counted.n, int64(len(counted.head)))])
	if err != nil {
		_ = s.storage.Delete(ctx, scope, key)
		return nil, err
	}
	size, digestHex, contentType := counted.n, hex.EncodeToString(digest.Sum(nil)), up.ContentType
	var width, height int
	if pass != nil {
		// The re-encoded frame goes under a key of its own, because Storage is
		// write-once — Put refuses a key that exists — and a second write to the
		// same name is not a thing this module asks for anywhere else. The object
		// that was streamed in is therefore referenced by no row the moment this
		// returns, and the Delete a few lines below removes it straight away; the
		// daily orphan sweep is what catches the object this process could not
		// remove itself, which is the gap between a blob write and a transaction.
		// The row below names the one key a reader is served.
		reencoded := contracts.Key(uuid.NewString())
		meta := contracts.MetaFor(&contracts.File{ContentType: pass.ContentType, Visibility: up.Visibility})
		if err := s.storage.Put(ctx, scope, reencoded, bytes.NewReader(pass.Bytes), int64(len(pass.Bytes)), meta); err != nil {
			_ = s.storage.Delete(ctx, scope, key)
			return nil, fmt.Errorf("file: store the re-encoded image: %w", err)
		}
		_ = s.storage.Delete(ctx, scope, key)
		key = reencoded
		size, digestHex, contentType = int64(len(pass.Bytes)), sha256Hex(pass.Bytes), pass.ContentType
		width, height = pass.Width, pass.Height
	}
	// Every byte is on disk, so there is finally something to open a
	// transaction for — and the quota is measured inside it, under the lock.
	tx, err := open(ctx)
	if err != nil {
		_ = s.storage.Delete(ctx, scope, key)
		return nil, err
	}
	if err := s.charge(ctx, tx, size); err != nil {
		_ = s.storage.Delete(ctx, scope, key)
		return nil, err
	}

	f := &contracts.File{
		Name: up.Name, ContentType: contentType, Visibility: up.Visibility, Kind: up.Kind,
		Size: size, SHA256: digestHex, StorageKey: key.String(), Width: width, Height: height,
	}
	if err := crud.Create(ctx, tx, f); err != nil {
		_ = s.storage.Delete(ctx, scope, key)
		return nil, err
	}
	return f, events.Publish(ctx, tx, contracts.EventUploaded, contracts.Uploaded{
		FileID: f.ID, Name: f.Name, ContentType: f.ContentType, Size: f.Size,
		SHA256: f.SHA256, Visibility: f.Visibility, At: db.Now(),
	})
}

// Uses is the live list, read straight off the table the rewrite maintains.
//
// The file is read first and on purpose: RLS would answer an empty list for a
// row this tenant cannot see, and an empty list about a file the caller may not
// name is a lie — it says nobody is showing this one, which is the answer the
// release sweep acts on. Reading the row turns that answer into ErrNotFound.
func (s *Service) Uses(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID) ([]contracts.UseRow, error) {
	if _, err := crud.Get[*contracts.File](tx, fileID); err != nil {
		return nil, err
	}
	var rows []contracts.FileUse
	if err := tx.DB().WithContext(ctx).Where("file_id = ?", fileID).
		Order("created_at").Order("id").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("file: what reads %s: %w", fileID, err)
	}
	out := make([]contracts.UseRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, contracts.UseRow{
			Use:       contracts.Use{Module: row.Module, Entity: row.Entity, Record: row.Record, Field: row.Field, Locale: row.Locale},
			ID:        row.ID,
			CreatedAt: row.CreatedAt,
		})
	}
	return out, nil
}

// SetUses is the rewrite. The order is the whole of its safety: read what the
// field used, decide the difference with the same pure function the fake runs,
// lock every file either side names in ascending id order, and only then write.
//
// The lock is FOR KEY SHARE rather than FOR UPDATE because the row being locked
// is a file and the thing being protected is a set of rows pointing at it: two
// records that both reference one file may record their uses in either order,
// and neither is changing the file. What they cannot do is have the file deleted
// between this transaction's check and its write, and a key-share lock is
// exactly the lock a delete — which takes FOR UPDATE on the row it removes —
// waits for. Ascending order is what stops two records that name the same two
// files from each holding one and waiting for the other.
func (s *Service) SetUses(ctx context.Context, tx db.Tx[db.Tenant], use contracts.Use, refs []uuid.UUID) ([]uuid.UUID, error) {
	wanted := contracts.CollapseRefs(refs)
	var existing []contracts.FileUse
	if err := tx.DB().WithContext(ctx).
		Where("module = ? AND entity = ? AND record = ? AND field = ? AND locale = ?",
			use.Module, use.Entity, use.Record, use.Field, use.Locale).
		Order("file_id").Find(&existing).Error; err != nil {
		return nil, fmt.Errorf("file: what %s/%s %s shows: %w", use.Module, use.Entity, use.Record, err)
	}
	seen := make(map[uuid.UUID]contracts.FileUse, len(existing))
	from := make([]uuid.UUID, 0, len(existing))
	for _, row := range existing {
		seen[row.FileID] = row
		from = append(from, row.FileID)
	}
	add, remove := contracts.DiffUses(from, wanted)
	// One ascending pass over everything either side names, so the lock order
	// never depends on which side an id landed on. What a row that is not there
	// costs depends on that side, and only on that side.
	//
	// A file the body newly names has to exist: the refusal below is what keeps
	// a body pointing at a removed image from recording the uses of the files
	// that happen to still be there.
	//
	// A file this rewrite only ends may already be gone — its ledger row was
	// filed while the file existed, and Delete's missing ledger check (README
	// Limits) is the door a file leaves under a body still showing it. That
	// dangling row is what the caller came here to remove: refusing at it would
	// strand the record trying to stop existing, refused by a file it is giving
	// up, and leave the ledger saying a file nobody has is still being read.
	for _, id := range union(wanted, remove) {
		found, err := lockFileKeyShare(ctx, tx, id)
		if err != nil {
			return nil, err
		}
		if !found && slices.Contains(wanted, id) {
			return nil, fmt.Errorf("%w: %s is not a file of this tenant", crud.ErrNotFound, id)
		}
	}
	for _, id := range remove {
		// Hard, and for the reason the migration says out loud: this table is
		// the answer to "what reads this file", and a row that outlived the body
		// that stopped naming the file is that answer being wrong.
		if err := crud.Delete[*contracts.FileUse](tx, seen[id].ID, false); err != nil {
			return nil, err
		}
	}
	for _, id := range add {
		row := &contracts.FileUse{
			FileID: id, Module: use.Module, Entity: use.Entity,
			Record: use.Record, Field: use.Field, Locale: use.Locale,
		}
		if err := crud.Create(ctx, tx, row); err != nil {
			return nil, err
		}
	}
	if len(remove) == 0 {
		return nil, nil
	}
	// Which of the ended uses were the last their file had. This is the answer
	// the release sweep will be handed when it exists; today it is what the
	// caller that dropped the last reference learns, and the file keeps its
	// bytes either way.
	var live []uuid.UUID
	if err := tx.DB().WithContext(ctx).Model(&contracts.FileUse{}).
		Where("file_id IN ?", remove).Distinct().Pluck("file_id", &live).Error; err != nil {
		return nil, fmt.Errorf("file: whether %d files are still read: %w", len(remove), err)
	}
	// What ended and is not read by anything else. The order of the two answers
	// is "what to write, what to end", so the second one is the list here.
	_, unused := contracts.DiffUses(remove, live)
	return unused, nil
}

// lockFileKeyShare takes FOR KEY SHARE on one file's row and says whether the
// row was there for this tenant to find. It refuses for real errors only: what
// a missing row costs is the caller's decision, and SetUses decides it two ways.
//
// There is no second check of whose the row is, because there is nothing to
// check it against: the policy decides what this transaction can see, and a row
// it cannot see answers no rows. That is what makes a use of another tenant's
// file impossible rather than merely not allowed.
func lockFileKeyShare(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (bool, error) {
	var found uuid.UUID
	err := tx.DB().WithContext(ctx).Raw(`SELECT id FROM files WHERE id = ? FOR KEY SHARE`, id).Row().Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("file: lock %s to record a use of it: %w", id, err)
	}
	return true, nil
}

// union is the ascending, deduplicated id list every lock is taken in.
func union(a, b []uuid.UUID) []uuid.UUID {
	all := append(slices.Clone(a), b...)
	slices.SortFunc(all, func(x, y uuid.UUID) int { return bytes.Compare(x[:], y[:]) })
	return slices.Compact(all)
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
	body, err := s.storage.Get(ctx, contracts.ScopeOfTx(tx), contracts.Key(f.StorageKey))
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
	// A hold is the reason a removal is refused, and this door asks it before
	// it writes anything: file:manage says you may delete a file, and a hold is
	// a promise to somebody else that its bytes outlive the class it was filed
	// under. Refusing writes nothing.
	if live, why, err := held(ctx, tx, id); err != nil {
		return nil, err
	} else if live {
		return nil, fmt.Errorf("%w: %s (%s)", contracts.ErrHeld, id, why)
	}
	// Hard, not soft. A soft-deleted row is a row that still points at bytes,
	// and the bytes are about to go: keeping the row would be keeping a lie.
	if err := crud.Delete[*contracts.File](tx, id, false); err != nil {
		return nil, err
	}
	return f, events.Publish(ctx, tx, contracts.EventDeleted, contracts.Deleted{
		FileID: f.ID, StorageKey: f.StorageKey, SHA256: f.SHA256, Size: f.Size,
		Cause: contracts.EraseCaller, At: db.Now(),
	})
}

// Grant mints a time-limited door onto one row's bytes. See contracts.Service.
func (s *Service) Grant(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, expiry time.Duration) (*contracts.Grant, error) {
	switch {
	case expiry <= 0:
		expiry = contracts.DefaultGrantExpiry
	case expiry > contracts.MaxGrantExpiry:
		// Refused and not clamped, in both directions of the argument: a link
		// that quietly lives a day is a leak the caller did not ask for, and one
		// that quietly dies at fifteen minutes breaks a download the caller
		// designed for a day.
		return nil, fmt.Errorf("%w: %s is longer than the %s this module signs", contracts.ErrInvalidExpiry, expiry, contracts.MaxGrantExpiry)
	}
	f, err := crud.Get[*contracts.File](tx, id)
	if err != nil {
		return nil, err
	}
	if f.Public() {
		return nil, fmt.Errorf("%w: %s is public and is served at its public URL", contracts.ErrPublicFile, id)
	}
	signer, ok := s.storage.(contracts.Signer)
	if !ok {
		return nil, fmt.Errorf("%w: %T was wired as this deployment's store", contracts.ErrNotSignable, s.storage)
	}
	grant, err := signer.Sign(ctx, contracts.ScopeOfTx(tx), f, expiry)
	if err != nil {
		return nil, err
	}
	// Not an audit row: minting a link changes nothing, and a trail of every
	// link ever minted is noise where the thing worth keeping is a log line that
	// joins the byte that was fetched back to the request that minted it. trace
	// rides the context, so it is the same id the rest of the request carries.
	slog.InfoContext(ctx, "file: granted a private file",
		"file", f.ID, "tenant", db.TenantOf(tx).Slug, "expiresAt", grant.ExpiresAt)
	return grant, nil
}

// imagePass is the upload path's call into image.go, and all it decides is
// which objects are read as images and what happens when the reading fails.
//
// A nil pass with a nil error means "these bytes stay exactly as they arrived",
// which is the answer for everything the pass does not run over: a document
// whose header no decoder reads, because refusing it here would change what a
// document door accepts, and 0069 §4 asks what an image door refuses.
//
// It is called with the object already stored and nothing open, so every
// refusal below removes the blob: the caller cannot roll a write to disk back,
// and it does not try to.
func (s *Service) reencode(ctx context.Context, scope contracts.Scope, up contracts.Upload, key contracts.Key, head []byte) (*contracts.ImagePass, error) {
	if !contracts.ReadsAsImage(up, head) {
		return nil, nil
	}
	body, err := s.storage.Get(ctx, scope, key)
	if err != nil {
		return nil, s.refuseOrKeep(ctx, up, key, fmt.Errorf("file: read back %s to measure it: %w", key, err))
	}
	// The decode reads straight out of the store: the only bytes held in memory
	// before the frame is allocated are the header processImage keeps for the
	// orientation tag, and the frame itself is allocated once the ceiling has
	// been agreed to. The object past that header is streamed, not buffered — a
	// photograph is several megabytes and the probe is one.
	pass, err := contracts.ProcessImage(body, s.maxPixels)
	_ = body.Close()
	if err != nil {
		return nil, s.refuseOrKeep(ctx, up, key, err)
	}
	if int64(len(pass.Bytes)) > s.max {
		return nil, fmt.Errorf("%w: the frame re-encodes to %d bytes, past the %d this deployment accepts", contracts.ErrTooLarge, len(pass.Bytes), s.max)
	}
	return &pass, nil
}

// refuseOrKeep is the one place that decides whether a failed pass is a refusal
// or a shrug, and the rule is the caller's own declaration: somebody who said
// "image" gets the reason, and a file that was never claimed to be one is kept
// as it arrived — with the failure logged, because an image door that quietly
// stopped running over everything is the failure this would otherwise hide.
func (s *Service) refuseOrKeep(ctx context.Context, up contracts.Upload, key contracts.Key, err error) error {
	if contracts.RefusesPass(err, up.Image) {
		return err
	}
	slog.WarnContext(ctx, "file: not read as an image", "key", key.String(), "reason", err)
	return nil
}

// sha256Hex is the digest of bytes this module wrote itself, which is the same
// account as the digest of the bytes that arrived and has to be true of the
// object a reader is served.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
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
