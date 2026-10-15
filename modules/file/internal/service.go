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
	"gorm.io/gorm"

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
		_ = s.storage.Delete(ctx, scope, key)
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
	for _, row := range contracts.VariantPlan(f.ID, width, height, contentType) {
		if err := crud.Create(ctx, tx, row); err != nil {
			_ = s.storage.Delete(ctx, scope, key)
			return nil, err
		}
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
// Two lock strengths, chosen by what the rewrite does to the file rather than
// by which table it writes. A file whose use this rewrite ends is locked FOR
// UPDATE: what it is about to decide — whether that file is now read by nothing —
// is a count of rows another rewrite may be ending in the same instant, and two
// compatible key shares let each count the other's not-yet-committed row, so both
// would answer "still used", both would commit, and the picture would keep its
// bytes with the grace period unstarted and no sweep ever selecting it again. The
// exclusive lock makes the later rewrite wait for the earlier one to land and
// count what is left after it. A file the new set only names is locked FOR KEY
// SHARE, which is the lock that buys the one thing this rewrite needs of a file
// it is not deciding about: that it is not deleted between the check and the
// write, since a delete takes FOR UPDATE on the row it removes and so waits for
// the key share. Ascending order is what stops two records that name the same two
// files from each holding one and waiting for the other.
//
// The audience the record asserts is not a reason to lock a named file. It is a
// fact about the row this rewrite files, and the door that serves a stranger
// reads it there (see readsPublicly), so a begun use writes no file row and takes no
// lock another rewrite can be waiting behind. A save of two richtext fields is
// two calls of this one function inside one transaction, and the record's body
// decides which field names which picture: a rewrite that locked a named file
// exclusively would then hold one file row while asking for another, in an order
// nobody wrote down, and two records sharing two pictures would each be waiting
// for the file the other holds. Postgres ends that cycle by refusing one of the
// two saves, which is a person's write lost to a collision they cannot see or
// fix. What still needs the exclusive lock — the count of uses left — is a
// decision about a file this rewrite is ending, and the save-wide pass above
// (see contracts.UsesOrder) takes those locks in one ascending sweep.
func (s *Service) SetUses(ctx context.Context, tx db.Tx[db.Tenant], use contracts.Use, refs []uuid.UUID) ([]uuid.UUID, error) {
	return s.setUses(ctx, tx, use, refs, false)
}

// SetUsesShown is the same rewrite with the audience the record asserts. See
// contracts.ShownUses.
func (s *Service) SetUsesShown(ctx context.Context, tx db.Tx[db.Tenant], use contracts.Use, refs []uuid.UUID, public bool) ([]uuid.UUID, error) {
	return s.setUses(ctx, tx, use, refs, public)
}

var _ contracts.ShownUses = (*Service)(nil)

// OrderUses is the save-wide lock pass contracts.UsesOrder promises: one ascending
// FOR UPDATE walk over every file this save names and every file the record already
// shows, taking no decision and writing no row.
//
// The record's own ledger rows belong in the walk because the writes that end a use
// are the ones that most need the file locked — they are the ones that decide whether
// the file has any use left — and an edit that drops a picture names nothing at all.
// A save is allowed to name a file no row of this tenant answers for, and this pass
// says nothing about it either way: the refusal for a file that is not there is
// setUses', asked once the rows are about to be written and the answer is a fact the
// caller is owed.
//
// The rows this walk locks include every one setUses and SetShown go on to lock
// exclusively, so nothing is taken weak and upgraded later: a row locked here is
// already held when the rewrite asks for it, which Postgres answers without waiting.
func (s *Service) OrderUses(ctx context.Context, tx db.Tx[db.Tenant], owner contracts.Use, refs []uuid.UUID) error {
	var shown []uuid.UUID
	if err := tx.DB().WithContext(ctx).Model(&contracts.FileUse{}).
		Where("module = ? AND entity = ? AND record = ?", owner.Module, owner.Entity, owner.Record).
		Distinct().Pluck("file_id", &shown).Error; err != nil {
		return fmt.Errorf("file: what %s/%s %s shows: %w", owner.Module, owner.Entity, owner.Record, err)
	}
	for _, id := range union(contracts.CollapseRefs(refs), shown) {
		if _, err := lockFileForUpdate(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

var _ contracts.UsesOrder = (*Service)(nil)

// setUses is one rewrite, asked with the audience the record owning it asserts.
// public says whether the rows this pass files are shown to somebody who has not
// signed in — the record's state, never the request's claim — and it changes one
// thing: what the filed rows assert, and therefore whose audience the settlement
// below has to ask about. It changes no lock: the audience a begun use asserts lives
// in the use row, not in the file row.
func (s *Service) setUses(ctx context.Context, tx db.Tx[db.Tenant], use contracts.Use, refs []uuid.UUID, public bool) ([]uuid.UUID, error) {
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
	// Which of the ended rows were asserting a public audience. A publish granted
	// these files the anonymous read, and this rewrite is removing the row that
	// granted it, so this is the write that has to take it back — the third way back
	// the brief names, "deleting the last public user". It names the rows being ended
	// and not the files left with none: a file the ledger has never had a row for —
	// the site logo — is in neither list, and the exemption the audience rule gives
	// it (a file with no use rows keeps whatever its upload asked for) stays exactly
	// where it was. An
	// upload the media library made public and a draft only ever named privately is
	// not here either: the hand door's choice is not this rewrite's to take back,
	// only the audience a publish asserted.
	var retract []uuid.UUID
	for _, id := range remove {
		if seen[id].Public {
			retract = append(retract, id)
		}
	}
	// One ascending pass over everything either side names, so the lock order
	// never depends on which side an id landed on. What a row that is not there
	// costs depends on that side, and only on that side.
	//
	// Every file the new set names has to exist: the refusal below is what keeps
	// a body pointing at a removed image from recording the uses of the files
	// that happen to still be there.
	//
	// A file this rewrite only ends may already be gone — its ledger row was
	// filed while the file existed, and the doors that let a file leave under a body
	// still showing it are erasure and the release sweep, each of which ends every use
	// of what it removes (README Limits). That
	// dangling row is what the caller came here to remove: refusing at it would
	// strand the record trying to stop existing, refused by a file it is giving
	// up, and leave the ledger saying a file nobody has is still being read.
	for _, id := range union(wanted, remove) {
		// One ascending walk, one lock per file, taken before any of it is decided:
		// FOR UPDATE where this rewrite ends a use and so decides whether the file
		// has any left, FOR KEY SHARE where it only names the file. No lock is taken
		// weak and upgraded later, which two rewrites of one file would deadlock;
		// the order is SetShown's too, so a publish and an edit that share an image
		// serialise on it rather than each reading a snapshot the other has written.
		var (
			found bool
			err   error
		)
		if slices.Contains(remove, id) {
			found, err = lockFileForUpdate(ctx, tx, id)
		} else {
			found, err = lockFileKeyShare(ctx, tx, id)
		}
		if err != nil {
			return nil, err
		}
		if !found && slices.Contains(wanted, id) {
			return nil, fmt.Errorf("%w: %s is not a file of this tenant", crud.ErrNotFound, id)
		}
	}
	// Who may read each file this rewrite is about to change, asked before any of
	// it is written. The audience a file ends up with is the answer to a question
	// about the ledger, so the rewrite asks it twice — once here, once the rows are
	// down — and publishes the flips between the two. Without the first ask there is
	// no way to tell the published page that gained its first picture from the one
	// that already had one, and the second is the same question after the answer
	// moved. Asked only where the audience can move at all: a public audience being
	// filed, or a row that asserted one being ended.
	var given map[uuid.UUID]bool
	if public || len(retract) > 0 {
		asked, err := readsPublicly(ctx, tx, union(wanted, remove))
		if err != nil {
			return nil, err
		}
		given = asked
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
			Public: public,
		}
		if err := crud.Create(ctx, tx, row); err != nil {
			return nil, err
		}
	}
	if len(add) > 0 {
		// Something shows these again, so their grace period is over. This is the
		// other arm of the same stamp, and it is why an image can be moved from one
		// body to another inside the grace period and survive: the write that starts
		// the use is the write that ends the countdown.
		//
		// unused_since IS NOT NULL is in the WHERE because a countdown that was never
		// started has nothing to stop, and an UPDATE locks every row whose predicate it
		// weighs: written this way, the ordinary save — a picture uploaded a minute ago
		// and named for the first time — writes and locks no file row at all.
		if err := tx.DB().WithContext(ctx).Model(&contracts.File{}).
			Where("id IN ? AND unused_since IS NOT NULL", add).Update("unused_since", nil).Error; err != nil {
			return nil, fmt.Errorf("file: say %d images are shown again: %w", len(add), err)
		}
	}
	if len(remove) == 0 {
		// Nothing ended, so no grace period started and no audience was taken back.
		// A public audience is still asked about: the case is the published page that
		// gained its first picture, and the answer is the ledger's, which is where the
		// door reads it.
		if err := s.settleAudience(ctx, tx, given, use); err != nil {
			return nil, err
		}
		return nil, nil
	}
	// Which of the ended uses were the last their file had. This is the answer
	// the release sweep will be handed when it exists; today it is what the
	// caller that dropped the last reference learns, and the file keeps its
	// bytes either way.
	//
	// Counted after every lock above is held, which is what makes the count
	// trustworthy: a rewrite that ended another use of the same file was waiting
	// on that lock and, having landed, its rows are gone from this statement's
	// snapshot. Counting before the lock, or with a lock another rewrite can hold
	// at the same time, is the two-both-decide-it-is-still-used answer.
	var live []uuid.UUID
	if err := tx.DB().WithContext(ctx).Model(&contracts.FileUse{}).
		Where("file_id IN ?", remove).Distinct().Pluck("file_id", &live).Error; err != nil {
		return nil, fmt.Errorf("file: whether %d files are still read: %w", len(remove), err)
	}
	// What ended and is not read by anything else. The order of the two answers
	// is "what to write, what to end", so the second one is the list here.
	_, unused := contracts.DiffUses(remove, live)
	if len(unused) > 0 {
		// The grace period starts now, durably, in the transaction that removed the
		// last use row — the same one that wrote the body that stopped naming the
		// picture, so an edit that rolls back moves no clock. The sweep measures its
		// day from this instant; nothing else records when a file stopped being
		// shown, and deleted_at belongs to a row that is not being deleted.
		// width > 0 is in the WHERE because only a file the image pass measured is
		// ever stamped — the ledger speaks about pictures, files_unused_since_measured
		// CHECKs the stamp against the measurement, and a document upload that loses
		// the last record that named it has no grace period to start. The list
		// returned below is unchanged: it names whose last use ended, which is wider
		// than what the sweep can act on, and the sweep's own predicate asks width > 0.
		at := db.Now()
		if err := tx.DB().WithContext(ctx).Model(&contracts.File{}).
			Where("id IN ? AND deleted_at IS NULL AND width > 0", unused).Update("unused_since", &at).Error; err != nil {
			return nil, fmt.Errorf("file: stamp when %d images stopped being shown: %w", len(unused), err)
		}
	}
	// The audience either side of this rewrite named may have moved: rows that
	// asserted one went, rows that assert one arrived. Neither half of the answer is
	// written anywhere here — a begun use takes no exclusive lock on a file it only
	// names, which is the lock two records that share two pictures would collide on,
	// and an ended one leaves its row's own visibility alone because that is what the
	// file's uploader was given — so what is asked here and what the next reader asks
	// are the same two reads of the ledger.
	if err := s.settleAudience(ctx, tx, given, use); err != nil {
		return nil, err
	}
	return unused, nil
}

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

// whoReads answers "who may read these files" as the two halves that make it up:
// which of them their own row was given to the world, and which of them a record
// with a public face is showing. Either half is enough for a reader, and only the
// first is ever written onto the file: the second lives where it is asserted, in the
// use row, and the commands that decide it publish the flip and nothing else.
//
// The second half is read where it is written rather than trusted from the file row
// because writing the file row is a lock on it, held to the end of the transaction
// that wrote it: a save that wrote the row of every file its body merely names
// would hold one picture while asking for another, in an order each record's own
// field order chose and nobody wrote down, and two records that share two pictures
// then each wait for a row the other holds — which Postgres ends by refusing one of
// the two saves outright. What that leaves the door is one indexed lookup into the
// ledger for a file its own row says is private, and the lookup is the same for one
// file or a hundred asked together.
func whoReads(ctx context.Context, tx db.Tx[db.Tenant], ids []uuid.UUID) (given, shown map[uuid.UUID]bool, err error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	half := func(asking string, where string, args ...any) (map[uuid.UUID]bool, error) {
		var found []uuid.UUID
		q := tx.DB().WithContext(ctx).Model(&contracts.File{}).Where("files.id IN ?", ids)
		if err := q.Where(where, args...).Pluck("files.id", &found).Error; err != nil {
			return nil, fmt.Errorf("file: who %s %d files: %w", asking, len(ids), err)
		}
		out := make(map[uuid.UUID]bool, len(found))
		for _, id := range found {
			out[id] = true
		}
		return out, nil
	}
	// The floor is what the upload was given, and not the row's own visibility: an
	// earlier release wrote this same column with the answer a publish implied, so the
	// only column that still says what the uploader chose is the one nothing rewrites.
	if given, err = half("may read", "files.upload_visibility = ?", contracts.VisibilityPublic); err != nil {
		return nil, nil, err
	}
	if shown, err = half("is shown by", "EXISTS (SELECT 1 FROM file_uses u"+
		" WHERE u.file_id = files.id AND u.public AND u.deleted_at IS NULL)"); err != nil {
		return nil, nil, err
	}
	return given, shown, nil
}

// readsPublicly is whoReads collapsed into the one answer a reader is given: a file
// is readable by somebody who has not signed in when either half says so.
func readsPublicly(ctx context.Context, tx db.Tx[db.Tenant], ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	given, shown, err := whoReads(ctx, tx, ids)
	if err != nil {
		return nil, err
	}
	// Seeded with every id asked, so that the answer is a verdict per file and a
	// caller can tell "asked, and private" from "not asked at all": the settlement
	// below compares this map with the same one asked after its own writes, file by
	// file, and an id missing from both would look like an audience that never moved.
	out := make(map[uuid.UUID]bool, len(ids))
	for _, id := range ids {
		out[id] = false
	}
	for id := range given {
		out[id] = true
	}
	for id := range shown {
		out[id] = true
	}
	return out, nil
}

// settleAudience publishes the audience flips between was and the answer now, in the
// transaction that moved them, and puts back the one row write a revocation owes.
//
// Who may read a file is the answer to a question about the ledger — the door asks it
// there (see whoReads) — and the one fact about a file's audience that is not that
// answer is what its upload was given, which lives in files.upload_visibility and is
// written by the upload and by the migration that recovered it for older rows, and by
// nothing that shows a file. So an audience being asserted writes no file row: the
// half it adds lives in the use row that asserts it.
//
// An audience being *taken back* writes one, and only downward: a file whose recorded
// upload visibility is private and which nothing public reads any more has its own
// visibility put back to private. For a row this release wrote that is where it already
// stood, and for a row an earlier release promoted by writing the derived answer into
// files.visibility it is the row learning its own floor again. The rows this can reach
// are the ones the command already locked FOR UPDATE — a downward move needs a public
// use row to have just ended, and the rewrite that ends one is the one holding that
// lock — so the write asks for no lock its caller does not already hold.
//
// So the upload's choice is the floor. A file given public by a person holding
// file:manage keeps that read whatever a record later does with it: a page that
// showed the site logo and then dropped it takes back the audience that page
// asserted, which lives in the use row that asserted it, and nothing wider. The
// derived half being answered at the door rather than copied into a row is also what
// keeps bytes nobody is showing private: the write that ends the last row asserting a
// public audience ends the audience with it, in that transaction, not when a sweep
// gets there.
//
// A flip is one event, and an audience that did not move records nothing.
func (s *Service) settleAudience(ctx context.Context, tx db.Tx[db.Tenant], was map[uuid.UUID]bool,
	owner contracts.Use) error {
	if len(was) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, 0, len(was))
	for id := range was {
		ids = append(ids, id)
	}
	ids = ascending(ids)
	now, err := readsPublicly(ctx, tx, ids)
	if err != nil {
		return err
	}
	if err := revokeGiven(ctx, tx, was, now); err != nil {
		return err
	}
	for _, id := range ids {
		if now[id] == was[id] {
			continue
		}
		want := contracts.VisibilityPrivate
		if now[id] {
			want = contracts.VisibilityPublic
		}
		if err := events.Publish(ctx, tx, contracts.EventVisibility, contracts.Visibility{
			FileID: id, Visibility: want, Module: owner.Module, Entity: owner.Entity,
			Record: owner.Record, At: db.Now(),
		}); err != nil {
			return err
		}
	}
	return nil
}

// revokeGiven puts files.visibility back to private for every file this settlement
// found is no longer publicly readable and whose recorded upload visibility is private.
// One statement over the ids the caller already holds the locks on: a file whose
// visibility already says private matches nothing, so the ordinary revocation of an
// ordinary upload writes no row at all, and what does match is a row an earlier release
// wrote the derived answer into.
func revokeGiven(ctx context.Context, tx db.Tx[db.Tenant], was, now map[uuid.UUID]bool) error {
	var ended []uuid.UUID
	for id, before := range was {
		if before && !now[id] {
			ended = append(ended, id)
		}
	}
	if len(ended) == 0 {
		return nil
	}
	ended = ascending(ended)
	if err := tx.DB().WithContext(ctx).Model(&contracts.File{}).
		Where("id IN ? AND visibility = ? AND upload_visibility = ?",
			ended, contracts.VisibilityPublic, contracts.VisibilityPrivate).
		Update("visibility", contracts.VisibilityPrivate).Error; err != nil {
		return fmt.Errorf("file: take back the audience %d files were shown to: %w", len(ended), err)
	}
	return nil
}

// shownRead is the door's question about one row the caller already read, answered
// on that row: a file the upload gave to the world is read by anybody, and so is one
// a record that serves strangers is showing — which is what the use row says and the
// file row need not. The row handed back carries whichever half answered, because the
// bytes served, the cache header on them and whatever the caller goes on to report
// about the file are three places that must not answer the question differently.
//
// The first question is asked of what the upload was given rather than of the row's own
// visibility, which is the same rule as whoReads' and for the same reason: a release
// before this one wrote the derived answer into files.visibility, so for a private
// upload a published page had shown that column said public — and a door that believed
// it would go on serving those bytes to strangers after the page had taken the audience
// back.
func shownRead(ctx context.Context, tx db.Tx[db.Tenant], f *contracts.File) (bool, error) {
	if f.GivenPublic() {
		return true, nil
	}
	_, shown, err := whoReads(ctx, tx, []uuid.UUID{f.ID})
	if err != nil {
		return false, err
	}
	if !shown[f.ID] {
		return false, nil
	}
	f.Visibility = contracts.VisibilityPublic
	return true, nil
}

// lockFileForUpdate is lockFileKeyShare's stronger sibling, for the two commands
// that decide a file's audience — SetShown, and the rewrite that ends a use — each of
// which answers who may read the file off a read of every live use of it, and the
// lock is what makes that read repeatable against another publish of the same image.
// The rewrite that ends a use needs it for one thing more: the count of uses left is
// the grace period's start, and only an exclusive lock makes it the last word.
func lockFileForUpdate(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID) (bool, error) {
	var found uuid.UUID
	err := tx.DB().WithContext(ctx).Raw(`SELECT id FROM files WHERE id = ? FOR UPDATE`, id).Row().Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("file: lock %s to say who may read it: %w", id, err)
	}
	return true, nil
}

// missing is the first id in want that have does not name, for the refusal that
// has to say which file the caller means.
func missing(want, have []uuid.UUID) uuid.UUID {
	for _, id := range want {
		if !slices.Contains(have, id) {
			return id
		}
	}
	return uuid.Nil
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
	return slices.Compact(ascending(append(slices.Clone(a), b...)))
}

// ascending puts file ids in the one order every lock pass here takes them, so no
// two commands can arrive at two files they share from opposite ends. A caller
// names its files in the order its body holds them, which is nobody's lock order.
func ascending(ids []uuid.UUID) []uuid.UUID {
	out := slices.Clone(ids)
	slices.SortFunc(out, func(x, y uuid.UUID) int { return bytes.Compare(x[:], y[:]) })
	return out
}

// Open is the row and its bytes. See contracts.Service.
func (s *Service) Open(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, anonymous bool) (*contracts.File, io.ReadCloser, error) {
	f, err := crud.Get[*contracts.File](tx, id)
	if err != nil {
		return nil, nil, err
	}
	if anonymous {
		may, err := shownRead(ctx, tx, f)
		if err != nil {
			return nil, nil, err
		}
		if !may {
			// Not a 403: a caller who is not signed in learns nothing about what
			// this tenant has, including whether it has this.
			return nil, nil, crud.ErrNotFound
		}
	}
	return s.openKey(ctx, tx, f, contracts.Key(f.StorageKey))
}

// openKey is the one read of the store every door ends at: the row already
// checked, and the bytes at one key.
func (s *Service) openKey(ctx context.Context, tx db.Tx[db.Tenant], f *contracts.File, key contracts.Key) (*contracts.File, io.ReadCloser, error) {
	body, err := s.storage.Get(ctx, contracts.ScopeOfTx(tx), key)
	if err != nil {
		// A row that says there are bytes and a store that has none is the one
		// inconsistency the split can produce, and it is an outage rather than
		// something the caller did: it reaches huma as a 500 with this in the log.
		return nil, nil, fmt.Errorf("file: open %s for %s: %w", key, f.ID, err)
	}
	return f, body, nil
}

// OpenWidth is Open at one planned width. See contracts.WidthOpener.
//
// There is no fallback to the original, and that is the whole design of the
// door: one address with one width names one object, or every cache between this
// process and a reader becomes a lie about which of the two it is holding. A
// width with no ready variant is therefore the same 404 a private file gets at
// the public door, so an anonymous caller learns nothing about which widths this
// tenant has waiting to be built.
func (s *Service) OpenWidth(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, anonymous bool, width int) (*contracts.File, io.ReadCloser, error) {
	if width <= 0 {
		return s.Open(ctx, tx, id, anonymous)
	}
	f, err := crud.Get[*contracts.File](tx, id)
	if err != nil {
		return nil, nil, err
	}
	if anonymous {
		may, err := shownRead(ctx, tx, f)
		if err != nil {
			return nil, nil, err
		}
		if !may {
			return nil, nil, crud.ErrNotFound
		}
	}
	if width == f.Width {
		// The frame's own width asked for by number: the same object the bare
		// address serves, because a width is never planned equal to the frame's
		// own and this address has exactly one thing to answer with.
		return s.openKey(ctx, tx, f, contracts.Key(f.StorageKey))
	}
	var row contracts.Variant
	err = tx.DB().WithContext(ctx).Where("file_id = ? AND width = ? AND state = ? AND deleted_at IS NULL",
		id, width, contracts.VariantReady).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, fmt.Errorf("%w: no %d pixel width of %s is built", crud.ErrNotFound, width, id)
	}
	if err != nil {
		return nil, nil, fmt.Errorf("file: which widths %s serves: %w", id, err)
	}
	return s.openKey(ctx, tx, atWidth(f, &row), contracts.Key(row.StorageKey))
}

// atWidth is the row a reader of one width's bytes is told about: the parent's
// name, type and visibility — a width is the same picture, and the container was
// decided once, on the frame — and the numbers of the object standing at this
// address.
//
// `size` is what makes this a cure rather than a tidying: contracts.ContentResponse
// answers every body that cannot seek with Content-Length read from the row it was
// handed, so a width served with its parent's size is a response whose own header
// contradicts the bytes it sends — true of any Storage whose Get streams (the two
// this module ships return a seekable file and an object, so only an adapter written
// outside this repository meets it today, and the port does not promise seekability).
// sha256, width, height and updated_at travel for the same reason and no more: they
// are what a caller checks or caches about the object it is being sent, and a row
// that describes the frame beside a body that is a width describes something else.
//
// The copy is not cosmetic. crud.Get handed back the row this transaction read, and
// nothing on a read path has any business rewriting it.
func atWidth(f *contracts.File, row *contracts.Variant) *contracts.File {
	out := *f
	out.Size, out.SHA256 = row.Size, row.SHA256
	out.Width, out.Height = row.Width, row.Height
	out.UpdatedAt = row.UpdatedAt
	return &out
}

// Variants is the widths one file can serve today, smallest first: the read a
// srcset is built from, and the same answer the door serves a ?w= off, so the
// two cannot disagree about which widths exist.
//
// A ready row is the whole question. A pending one is not servable and a failed
// one never will be, and a caller that wants to know what is *waiting* reads the
// rows themselves in the media library rather than through a port that lays
// images out.
func (s *Service) Variants(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID) ([]contracts.Variant, error) {
	if _, err := crud.Get[*contracts.File](tx, fileID); err != nil {
		return nil, err
	}
	var rows []contracts.Variant
	if err := tx.DB().WithContext(ctx).Where("file_id = ? AND state = ? AND deleted_at IS NULL",
		fileID, contracts.VariantReady).Order("width").Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("file: which widths %s serves: %w", fileID, err)
	}
	return rows, nil
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
