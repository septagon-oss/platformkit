// Package internal is every implementation of the file module. Nothing outside
// modules/file can import it, which is the compiler enforcing idea 3.
package internal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
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
	// decode is this process's ceiling on how many frames are being decoded at
	// once, held by the upload pass and by the background encode alike. Every
	// service of this process is built on the one gate, because the ceiling is the
	// machine's and a process can build a service per runtime; a white-box test
	// that wants to see the ceiling close sets the field to a gate of its own.
	decode *decodeGate
}

// NewService takes the storage the bytes go to, the largest upload this
// deployment accepts, the disk one tenant may fill, and the largest frame it
// will decode. A maxPixels of 0 means contracts.DefaultMaxImagePixels.
func NewService(storage contracts.Storage, max, quota int64, maxPixels int) *Service {
	if maxPixels <= 0 {
		maxPixels = contracts.DefaultMaxImagePixels
	}
	return &Service{
		storage: storage, max: max, quota: quota, maxPixels: maxPixels,
		decode: processDecode(),
	}
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
	saved, err := s.storedFrame(ctx, scope, up, stored{
		key: key, size: counted.n, digest: hex.EncodeToString(digest.Sum(nil)), contentType: up.ContentType,
	}, pass)
	if err != nil {
		_ = s.storage.Delete(ctx, scope, key)
		return nil, err
	}
	if pass != nil {
		// The object that was streamed in is named by no row once its re-encoded
		// frame is stored, so it goes now; the daily orphan sweep catches what this
		// process could not remove itself, which is the gap between a blob write and
		// a transaction. This is the last thing the arrived bytes are good for: from
		// here the only object this upload still owns is saved.key, the one the row
		// below would name, so every refusal after this line removes that one and a
		// refused upload leaves no byte of either object behind.
		_ = s.storage.Delete(ctx, scope, key)
	}
	// Every byte is on disk, so there is finally something to open a
	// transaction for — and the quota is measured inside it, under the lock.
	tx, err := open(ctx)
	if err != nil {
		_ = s.storage.Delete(ctx, scope, saved.key)
		return nil, err
	}
	if err := s.charge(ctx, tx, saved.size); err != nil {
		_ = s.storage.Delete(ctx, scope, saved.key)
		return nil, err
	}

	f, err := s.record(ctx, tx, up, saved)
	if err != nil {
		_ = s.storage.Delete(ctx, scope, saved.key)
		return nil, err
	}
	return f, events.Publish(ctx, tx, contracts.EventUploaded, contracts.Uploaded{
		FileID: f.ID, Name: f.Name, ContentType: f.ContentType, Size: f.Size,
		SHA256: f.SHA256, Visibility: f.Visibility, At: db.Now(),
	})
}

// stored is what an upload left in the object store: the key a reader is served, and
// the size, digest, media type and frame the row must therefore claim — which, after
// the image pass, are the ones of what is stored rather than of what was sent.
type stored struct {
	key                 contracts.Key
	size                int64
	digest, contentType string
	width, height       int
}

// storedFrame keeps the re-encoded pixels when the image pass produced any. They go
// under a key of their own because Storage is write-once — Put refuses a key that
// exists — and a second write to the same name is not a thing this module asks for
// anywhere else. With no pass the bytes that arrived are the bytes a reader fetches.
func (s *Service) storedFrame(ctx context.Context, scope contracts.Scope, up contracts.Upload, arrived stored, pass *contracts.ImagePass) (stored, error) {
	if pass == nil {
		return arrived, nil
	}
	reencoded := contracts.Key(uuid.NewString())
	meta := contracts.MetaFor(&contracts.File{ContentType: pass.ContentType, Visibility: up.Visibility})
	if err := s.storage.Put(ctx, scope, reencoded, bytes.NewReader(pass.Bytes), int64(len(pass.Bytes)), meta); err != nil {
		return arrived, fmt.Errorf("file: store the re-encoded image: %w", err)
	}
	return stored{
		key: reencoded, size: int64(len(pass.Bytes)), digest: sha256Hex(pass.Bytes),
		contentType: pass.ContentType, width: pass.Width, height: pass.Height,
	}, nil
}

// record writes the row and the plan for the other widths in one transaction, so a
// plan never exists without its picture and an upload that rolled back files nothing.
func (s *Service) record(ctx context.Context, tx db.Tx[db.Tenant], up contracts.Upload, saved stored) (*contracts.File, error) {
	f := &contracts.File{
		Name: up.Name, ContentType: saved.contentType, Visibility: up.Visibility, Kind: up.Kind, Sealed: up.Sealed,
		Size: saved.size, SHA256: saved.digest, StorageKey: saved.key.String(), Width: saved.width, Height: saved.height,
		// The same word in both columns, and this one is where it stays: what the
		// upload was given is the floor of who may read the file, and the only
		// writes it ever sees after here are this one and the migration that
		// recovered it for rows an earlier release overwrote.
		UploadVisibility: up.Visibility,
	}
	// No grace period starts here. The release rule the sweep runs is "a file whose
	// last use ended" (this file's own contracts.File.UnusedSince names it), and an
	// upload has no use that has ended — the ledger has not spoken about this file at
	// all. Stamping the arrival reaped every image whose only name lives outside the
	// ledger: a day after a tenant configured its site logo, site_settings names it
	// by a bare uuid (modules/site/contracts/site.go, LogoFileID), files no use row
	// and places no hold, so the sweep's predicate matched it exactly and the bytes
	// the public frame renders were destroyed. The abandoned upload — the person who
	// picked a file and never saved the record — is the cost of that boundary, and it
	// is charged to the quota and the retention sweep rather than to a rule that
	// cannot tell an unsaved picture from a configured one; see this module's README
	// and the round's report.
	if err := crud.Create(ctx, tx, f); err != nil {
		return nil, err
	}
	// The plan for the other widths, filed in the same transaction as the row it
	// belongs to, so a plan never exists without its picture and an upload that
	// rolled back files nothing. Pending rows carry no size, so this costs no
	// quota: the bytes charge for themselves when they are written, in the tick
	// that writes them, under the same lock every other charge takes.
	//
	// Nothing here encodes anything: the caller's request has already done the
	// one thing that had to happen in it, which is to store and serve the frame.
	// internal/variants.go builds the rest on the clock, and until it has been
	// round, the srcset names this row alone.
	for _, row := range contracts.VariantPlan(f.ID, saved.width, saved.height, saved.contentType) {
		if err := crud.Create(ctx, tx, row); err != nil {
			return nil, err
		}
	}
	return f, nil
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

var _ contracts.ShownUses = (*Service)(nil)

var _ contracts.UsesOrder = (*Service)(nil)

// SetShown is the audience half of the ledger: the command that asserts an
// audience. SetUses is the one that takes one back. See contracts.Service.
//
// The order is SetUses' and one step stronger: lock every named file FOR UPDATE
// in ascending id order — this command writes the file rows — then write the
// audience onto the rows, then ask the ledger who may read each file now. Every
// path in the kernel takes the record's row before it takes file rows, so
// record-then-file is the one order and two publishes that share an image serialise
// on that image rather than each announcing a flip the other has already announced.
//
// The audience is read off the whole table and not off the rows this call touched:
// the rule is "public if any live use is public", and a second record showing the
// same picture in a published page is exactly the case that has to keep it
// readable. What this command holds every file it names for is that read: the ledger
// answer of one file has to be settled while no other command is moving one of its
// uses, or two publishes of a shared picture each announce the flip the other has
// already announced. The answer itself is never written into the file row — that row
// holds the audience the file was given, which is the floor the answer is read from
// the ledger beside (see settleAudience).
func (s *Service) SetShown(ctx context.Context, tx db.Tx[db.Tenant], owner contracts.Use, refs []uuid.UUID, public bool) error {
	wanted := contracts.CollapseRefs(refs)
	if len(wanted) == 0 {
		// With nothing named, the record's own rows are the set: an unpublish ends
		// the audience of every file this record shows, whatever its body now says,
		// and it must never refuse, because refusing an unpublish would leave bytes
		// public that nothing is showing.
		var have []uuid.UUID
		if err := tx.DB().WithContext(ctx).Model(&contracts.FileUse{}).
			Where("module = ? AND entity = ? AND record = ?", owner.Module, owner.Entity, owner.Record).
			Distinct().Pluck("file_id", &have).Error; err != nil {
			return fmt.Errorf("file: what %s/%s %s shows: %w", owner.Module, owner.Entity, owner.Record, err)
		}
		wanted = have
	}
	for _, id := range ascending(wanted) {
		found, err := lockFileForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("%w: %s is not a file of this tenant", crud.ErrNotFound, id)
		}
	}
	was, err := readsPublicly(ctx, tx, wanted)
	if err != nil {
		return err
	}
	rows := tx.DB().WithContext(ctx).Model(&contracts.FileUse{}).
		Where("module = ? AND entity = ? AND record = ?", owner.Module, owner.Entity, owner.Record)
	if len(refs) > 0 {
		rows = rows.Where("file_id IN ?", refs)
	}
	if err := rows.Update("public", public).Error; err != nil {
		return fmt.Errorf("file: say who %s/%s %s shows: %w", owner.Module, owner.Entity, owner.Record, err)
	}
	if public {
		// A ref with no use row for this record is a publish that would expose bytes
		// the ledger says nothing about, and so could never take back. Refusing is
		// the safe direction, and the remedy is the door that files uses: save the
		// body, then publish.
		var shown []uuid.UUID
		if err := tx.DB().WithContext(ctx).Model(&contracts.FileUse{}).
			Where("module = ? AND entity = ? AND record = ? AND file_id IN ?",
				owner.Module, owner.Entity, owner.Record, wanted).
			Distinct().Pluck("file_id", &shown).Error; err != nil {
			return fmt.Errorf("file: whether %s/%s %s shows these files: %w", owner.Module, owner.Entity, owner.Record, err)
		}
		if len(shown) != len(wanted) {
			return fmt.Errorf("%w: %s — save the body that names it, then publish",
				contracts.ErrNotShown, missing(wanted, shown))
		}
	}
	return s.settleAudience(ctx, tx, was, owner)
}

var _ contracts.WidthOpener = (*Service)(nil)
var _ contracts.VariantServable = (*Service)(nil)

// Delete removes the row and says where the bytes are. See contracts.Service.
//
// The row is read under FOR UPDATE, before anything about it is decided. A use
// takes at least FOR KEY SHARE on the file — SetUses does, on both sides of its
// rewrite — so this lock is what makes "nothing shows it" a fact rather than a
// reading: a filing in flight holds the key share until it commits, this delete
// waits for it, and the ledger read below then runs in a snapshot that already
// contains the row that filing wrote. Without the lock the ledger read sees only
// uses that were committed *before* it, a use filed during this transaction lands
// after it, and the deletion strands a use on bytes its own event orders removed.
func (s *Service) Delete(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (*contracts.File, error) {
	// FOR UPDATE, and it is the lock that makes the question below answerable: the
	// strength a save takes on a file its new body names is FOR KEY SHARE, which
	// conflicts with this one, so the two orders settle without a second check
	// anywhere. A save that waits here finds no row when it wakes and is refused
	// whole; a save that landed first is counted by the read below.
	f, err := crud.GetForUpdate[*contracts.File](tx, id)
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
	// What still shows this. Distinct on the record and not on the row, because the
	// number a person can act on is the number of pages to go and edit, and one
	// page can name one picture in two fields in two locales. Served by the
	// file_uses (tenant_id, file_id) index, and scoped by the table's own policy —
	// a use of another tenant's file is not visible here and is not this tenant's
	// answer either.
	var uses []contracts.UseRow
	if err := tx.DB().WithContext(ctx).Model(&contracts.FileUse{}).
		Where("file_id = ?", id).Select("DISTINCT module, entity, record").Find(&uses).Error; err != nil {
		return nil, fmt.Errorf("file: what shows %s: %w", id, err)
	}
	// Refused after the hold check and before any write: a hold is a reason nobody
	// can lift by editing a page, and a refusal has to leave the row, the bytes and
	// the ledger exactly where they were, with nothing in the outbox. The removals
	// that outrank a record's convenience — a subject's erasure and the two sweeps —
	// go through their own paths and are never refused by a ledger row they are
	// about to end.
	if records := contracts.DistinctRecords(uses); records > 0 {
		return nil, &contracts.InUse{File: id, Records: records}
	}
	// Hard, not soft. A soft-deleted row is a row that still points at bytes,
	// and the bytes are about to go: keeping the row would be keeping a lie.
	if err := crud.Delete[*contracts.File](tx, id, false); err != nil {
		return nil, err
	}
	widths, err := removeWidths(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	return f, events.Publish(ctx, tx, contracts.EventDeleted, contracts.Deleted{
		FileID: f.ID, StorageKey: f.StorageKey, SHA256: f.SHA256, Size: f.Size,
		VariantKeys: widths, Cause: contracts.EraseCaller, At: db.Now(),
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
	// The same question the public door answers, so a link is never minted onto
	// bytes anybody can already have: a file a published page shows is served at
	// its public address whether or not its own row says so.
	if may, err := shownRead(ctx, tx, f); err != nil {
		return nil, err
	} else if may {
		return nil, fmt.Errorf("%w: %s is public and is served at its public URL", contracts.ErrPublicFile, id)
	}
	if f.Sealed {
		// A presigned URL is the content route with the signature done by the store:
		// handing one out for a sealed file would be this module opening the door it
		// just refused to open, and for longer than any request lives. The module
		// that sealed the bytes mints its own door.
		return nil, crud.ErrNotFound
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
	// Opened here, closed on the way out of this function whichever way that is.
	// The ceiling below is asked for after this round-trip and can answer no, and a
	// refusal that left the reader open would charge every refused upload a file
	// descriptor or a provider stream — a bound that leaks what it refuses is a
	// slower way to run out. The background encode closes its reader the same way.
	defer body.Close()
	// The decode reads straight out of the store: the only bytes held in memory
	// before the frame is allocated are the header processImage keeps for the
	// orientation tag, and the frame itself is allocated once the ceiling has
	// been agreed to. The object past that header is streamed, not buffered — a
	// photograph is several megabytes and the probe is one.
	// Under the process's ceiling: the pixel limit bounds one picture, this bounds
	// how many there are at once. The round-trip that opens the object is asked for
	// outside a slot, so a store that is slow to answer costs no decode capacity;
	// the streaming of the bytes happens inside one, which is what holds it for the
	// length of a decode rather than the length of the CPU alone.
	release, ok := s.decode.wait(ctx, decodeUploadWait)
	if !ok {
		return nil, s.refuseOrKeep(ctx, up, key, contracts.ErrBusyDecoding)
	}
	pass, err := contracts.ProcessImage(body, s.maxPixels)
	release()
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

// removeWidths removes one file's variant rows and answers which objects they
// named, so the caller can put those names on the work order that removes bytes
// once this transaction commits.
//
// Hard, with the parent, and read before the parent's row went: by the time a
// worker runs there is no row left to read the names from, which is the same
// reason the parent's own key travels inside file.deleted. A width counted in the
// tenant's quota sum, so removing its row is what gives the space back — leaving
// it behind would be a quota charged for bytes nothing can point at any more.
func removeWidths(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID) ([]string, error) {
	gorm := tx.DB().WithContext(ctx)
	var keys []string
	if err := gorm.Model(&contracts.Variant{}).Where("file_id = ?", fileID).
		Pluck("storage_key", &keys).Error; err != nil {
		return nil, fmt.Errorf("file: which widths %s has: %w", fileID, err)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	if err := gorm.Unscoped().Where("file_id = ?", fileID).
		Delete(&contracts.Variant{}).Error; err != nil {
		return nil, fmt.Errorf("file: remove the widths of %s: %w", fileID, err)
	}
	return keys, nil
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
//
// What it asks is usage — the same sum the quota meter answers with, so the
// number a person is shown and the number that refuses them are one number.
func (s *Service) charge(ctx context.Context, tx db.Tx[db.Tenant], n int64) error {
	if s.quota <= 0 {
		return nil
	}
	lock := "file/quota/" + db.TenantOf(tx).ID.String()
	if err := tx.DB().WithContext(ctx).
		Exec(`SELECT pg_advisory_xact_lock(hashtext(?)::bigint)`, lock).Error; err != nil {
		return fmt.Errorf("file: lock this tenant's quota: %w", err)
	}
	used, _, err := usage(ctx, tx)
	if err != nil {
		return err
	}
	if left := s.quota - used; n > left {
		return fmt.Errorf("%w: %d bytes is past the %d this tenant has left of %d", contracts.ErrQuota, n, max(left, 0), s.quota)
	}
	return nil
}

// usage is one tenant's disk: the bytes of its live rows and the bytes of the
// other widths of those same pictures, as one number.
//
// It is one function with two callers — the refusal above, which takes the
// tenant's lock before asking, and the meter, which deliberately does not — for
// the reason that matters: a meter that summed a different way from the refusal
// would promise space the upload then refused to give. Both ask the same
// question of the same two tables and get the same answer.
func usage(ctx context.Context, tx db.Tx[db.Tenant]) (used, widths int64, err error) {
	if err := tx.DB().Model(&contracts.File{}).Where("deleted_at IS NULL").
		Select("COALESCE(SUM(size), 0)").Scan(&used).Error; err != nil {
		return 0, 0, fmt.Errorf("file: what this tenant is holding: %w", err)
	}
	// A variant is the same picture at another width, and the disk it takes is
	// the same disk, so it is the same sum. It enters here and nowhere else: a
	// second counter would be a second number to be wrong about, and a width that
	// counted as nothing would make the quota a lie the day widths shipped. A
	// pending row carries no size, so filing a plan charges for nothing.
	if err := tx.DB().Model(&contracts.Variant{}).Where("deleted_at IS NULL").
		Select("COALESCE(SUM(size), 0)").Scan(&widths).Error; err != nil {
		return 0, 0, fmt.Errorf("file: what this tenant is holding in other widths: %w", err)
	}
	return used + widths, widths, nil
}

// plural is the one word a refusal gets to agree with its own number.
func plural(n int64, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
