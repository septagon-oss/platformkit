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
	"gorm.io/gorm/clause"

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
	// filed while the file existed, and Delete's missing ledger check (README
	// Limits) is the door a file leaves under a body still showing it. That
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
	if len(add) > 0 {
		// Something shows these again, so their grace period is over. This is the
		// other arm of the same stamp, and it is why an image can be moved from one
		// body to another inside the grace period and survive: the write that starts
		// the use is the write that ends the countdown.
		if err := tx.DB().WithContext(ctx).Model(&contracts.File{}).
			Where("id IN ?", add).Update("unused_since", nil).Error; err != nil {
			return nil, fmt.Errorf("file: say %d images are shown again: %w", len(add), err)
		}
	}
	if len(remove) == 0 {
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
	for _, id := range retract {
		// Recomputed from the whole ledger, not from this rewrite: a second published
		// record showing the same picture keeps it readable, and a file whose last
		// public row went with this one goes private in the transaction that ended it,
		// with its event, so an anonymous read of bytes nobody is showing any more is
		// refused at the door rather than served until the release sweep gets there.
		if err := s.recomputeVisibility(ctx, tx, id, use, true); err != nil {
			return nil, err
		}
	}
	return unused, nil
}

// SetShown is the audience half of the ledger: the command that asserts an
// audience. SetUses is the one that takes one back. See contracts.Service.
//
// The order is SetUses' and one step stronger: lock every named file FOR UPDATE
// in ascending id order — this command writes the file rows, so a key share is not
// enough — then write the audience onto the rows, then recompute each file from the
// whole ledger. Every path in the kernel takes the record's row before it takes
// file rows, so record-then-file is the one order and two publishes that share an
// image serialise on that image rather than each computing from a snapshot the
// other has not written.
//
// The recompute reads the whole table and not the rows this call touched: the
// rule is "public if any live use is public", and a second record showing the same
// picture in a published page is exactly the case that has to keep it readable.
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
	for _, id := range wanted {
		if err := s.recomputeVisibility(ctx, tx, id, owner, false); err != nil {
			return err
		}
	}
	return nil
}

// recomputeVisibility asks the ledger who may read one file and writes the answer
// when it differs, publishing the flip in the same transaction as the write that
// caused it.
//
// spoke says whether the ledger has anything to say about this file at all. Where
// it has rows, or has just lost its last one, the invariant decides: public if and
// only if a live use row of it is public, so "no rows" is an answer — nobody is
// showing this and nothing ever asserted otherwise. Where the ledger has never had
// a row for it (spoke false) there is no answer to derive, and the file keeps
// whatever its upload asked for: the site logo is that case, named by a bare uuid
// with no richtext field and no lifecycle, and the media library's hand-raised
// public upload is the other.
func (s *Service) recomputeVisibility(ctx context.Context, tx db.Tx[db.Tenant], id uuid.UUID, owner contracts.Use, spoke bool) error {
	var shown int64
	if err := tx.DB().WithContext(ctx).Model(&contracts.FileUse{}).
		Where("file_id = ? AND public", id).Count(&shown).Error; err != nil {
		return fmt.Errorf("file: who may read %s: %w", id, err)
	}
	var rows int64
	if err := tx.DB().WithContext(ctx).Model(&contracts.FileUse{}).
		Where("file_id = ?", id).Count(&rows).Error; err != nil {
		return fmt.Errorf("file: who reads %s: %w", id, err)
	}
	if rows == 0 && !spoke {
		return nil
	}
	want := contracts.VisibilityPrivate
	if shown > 0 {
		want = contracts.VisibilityPublic
	}
	f, err := crud.GetForUpdate[*contracts.File](tx, id)
	if errors.Is(err, crud.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if f.Visibility == want {
		return nil
	}
	f.Visibility = want
	if err := crud.Update(ctx, tx, f, "visibility", "updated_at"); err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventVisibility, contracts.Visibility{
		FileID: id, Visibility: want, Module: owner.Module, Entity: owner.Entity,
		Record: owner.Record, At: db.Now(),
	})
}

// lockFileForUpdate is lockFileKeyShare's stronger sibling, for the one command
// that writes the file row itself: SetShown recomputes visibility off a read of
// every use of this file, and the lock is what makes that read repeatable against
// another publish of the same image.
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
	if anonymous && !f.Public() {
		// Not a 403: a caller who is not signed in learns nothing about what
		// this tenant has, including whether it has this.
		return nil, nil, crud.ErrNotFound
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
	if anonymous && !f.Public() {
		return nil, nil, crud.ErrNotFound
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
	// A record that still shows this file is a claim about *that* record, and the
	// ledger is what knows: refuse here, inside the transaction, rather than
	// trusting the control a person happened to be shown. The lock is the one
	// SetUses takes when it ends a use — FOR UPDATE on the row's own ledger — so
	// "nothing shows it" is a fact and not a race against a save in flight: a
	// publish that lands after this read waits for this transaction, and one that
	// landed before it is counted.
	// The rows and not their count: Postgres refuses a FOR UPDATE under an
	// aggregate, and the count is the least interesting part of the answer.
	var shown []uuid.UUID
	if err := tx.DB().WithContext(ctx).Model(&contracts.FileUse{}).
		Clauses(clause.Locking{Strength: "UPDATE"}).Where("file_id = ?", id).
		Pluck("id", &shown).Error; err != nil {
		return nil, fmt.Errorf("file: what reads %s: %w", id, err)
	}
	if len(shown) > 0 {
		// Refused, so nothing is written and nothing is published: the remedy is in
		// the other record, which has to stop showing this one first. The retention
		// and release sweeps remove rows through their own paths and are never
		// refused by a ledger row they are about to end, and a subject's erasure
		// outranks a record's convenience — see the module's README.
		return nil, fmt.Errorf("%w: %s is shown by %d %s", contracts.ErrInUse, id,
			len(shown), plural(int64(len(shown)), "record", "records"))
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
