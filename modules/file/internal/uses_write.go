package internal

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

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

// setUses is one rewrite, asked with the audience the record owning it asserts.
// public says whether the rows this pass files are shown to somebody who has not
// signed in — the record's state, never the request's claim — and it changes one
// thing: what the filed rows assert, and therefore whose audience the settlement
// below has to ask about. It changes no lock: the audience a begun use asserts lives
// in the use row, not in the file row.
func (s *Service) setUses(ctx context.Context, tx db.Tx[db.Tenant], use contracts.Use, refs []uuid.UUID, public bool) ([]uuid.UUID, error) {
	wanted := contracts.CollapseRefs(refs)
	seen, from, err := s.shownUses(ctx, tx, use)
	if err != nil {
		return nil, err
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
	if err := s.lockNamed(ctx, tx, wanted, remove); err != nil {
		return nil, err
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
	if err := s.fileUseRows(ctx, tx, use, add, remove, seen, public); err != nil {
		return nil, err
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
	unused, err := s.stopCounting(ctx, tx, remove)
	if err != nil {
		return nil, err
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

// shownUses reads the ledger rows this record's field names today, as the map this
// pass compares against and the ascending list the diff is made from.
func (s *Service) shownUses(ctx context.Context, tx db.Tx[db.Tenant], use contracts.Use) (map[uuid.UUID]contracts.FileUse, []uuid.UUID, error) {
	var existing []contracts.FileUse
	if err := tx.DB().WithContext(ctx).
		Where("module = ? AND entity = ? AND record = ? AND field = ? AND locale = ?",
			use.Module, use.Entity, use.Record, use.Field, use.Locale).
		Order("file_id").Find(&existing).Error; err != nil {
		return nil, nil, fmt.Errorf("file: what %s/%s %s shows: %w", use.Module, use.Entity, use.Record, err)
	}
	seen := make(map[uuid.UUID]contracts.FileUse, len(existing))
	from := make([]uuid.UUID, 0, len(existing))
	for _, row := range existing {
		seen[row.FileID] = row
		from = append(from, row.FileID)
	}
	return seen, from, nil
}

// lockNamed takes one lock per file either side names, in one ascending walk, before
// any of it is decided: FOR UPDATE where this rewrite ends a use and so decides
// whether the file has any left, FOR KEY SHARE where it only names the file. No lock
// is taken weak and upgraded later, which two rewrites of one file would deadlock,
// and the order is SetShown's too, so a publish and an edit that share an image
// serialise on it rather than each reading a snapshot the other has written.
//
// Every file the new set names has to exist. A file this rewrite only ends may
// already be gone — the doors that let a file leave under a body still showing it are
// erasure and the release sweep, each of which ends every use of what it removes
// (README Limits) — and that dangling row is what the caller came here to remove:
// refusing at it would strand the record trying to stop existing.
func (s *Service) lockNamed(ctx context.Context, tx db.Tx[db.Tenant], wanted, remove []uuid.UUID) error {
	for _, id := range union(wanted, remove) {
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
			return err
		}
		if !found && slices.Contains(wanted, id) {
			return fmt.Errorf("%w: %s is not a file of this tenant", crud.ErrNotFound, id)
		}
	}
	return nil
}

// fileUseRows moves the ledger: every ended row deleted hard, because a row that
// outlived the body that stopped naming the file is the answer to "what reads this
// file" being wrong, and every begun row filed with the audience the record asserts.
func (s *Service) fileUseRows(ctx context.Context, tx db.Tx[db.Tenant], use contracts.Use, add, remove []uuid.UUID, seen map[uuid.UUID]contracts.FileUse, public bool) error {
	for _, id := range remove {
		if err := crud.Delete[*contracts.FileUse](tx, seen[id].ID, false); err != nil {
			return err
		}
	}
	for _, id := range add {
		row := &contracts.FileUse{
			FileID: id, Module: use.Module, Entity: use.Entity,
			Record: use.Record, Field: use.Field, Locale: use.Locale,
			Public: public,
		}
		if err := crud.Create(ctx, tx, row); err != nil {
			return err
		}
	}
	if len(add) == 0 {
		return nil
	}
	// Something shows these again, so their grace period is over. This is the other
	// arm of the same stamp, and it is why an image can be moved from one body to
	// another inside the grace period and survive: the write that starts the use is
	// the write that ends the countdown.
	//
	// unused_since IS NOT NULL is in the WHERE because a countdown that was never
	// started has nothing to stop, and an UPDATE locks every row whose predicate it
	// weighs: written this way, the ordinary save — a picture uploaded a minute ago
	// and named for the first time — writes and locks no file row at all.
	if err := tx.DB().WithContext(ctx).Model(&contracts.File{}).
		Where("id IN ? AND unused_since IS NOT NULL", add).Update("unused_since", nil).Error; err != nil {
		return fmt.Errorf("file: say %d images are shown again: %w", len(add), err)
	}
	return nil
}

// stopCounting answers which of these files nothing else reads any more, and starts
// their grace period on the same answer.
//
// The count is taken after every lock the rewrite holds, which is what makes it
// trustworthy: a rewrite that ended another use of the same file was waiting on that
// lock and, having landed, its rows are gone from this statement's snapshot. Counting
// before the lock, or with a lock another rewrite can hold at the same time, is the
// two-both-decide-it-is-still-used answer.
func (s *Service) stopCounting(ctx context.Context, tx db.Tx[db.Tenant], remove []uuid.UUID) ([]uuid.UUID, error) {
	var live []uuid.UUID
	if err := tx.DB().WithContext(ctx).Model(&contracts.FileUse{}).
		Where("file_id IN ?", remove).Distinct().Pluck("file_id", &live).Error; err != nil {
		return nil, fmt.Errorf("file: whether %d files are still read: %w", len(remove), err)
	}
	// What ended and is not read by anything else. The order of the two answers
	// is "what to write, what to end", so the second one is the list here.
	_, unused := contracts.DiffUses(remove, live)
	if len(unused) == 0 {
		return unused, nil
	}
	// The grace period starts now, durably, in the transaction that removed the last
	// use row — the same one that wrote the body that stopped naming the picture, so
	// an edit that rolls back moves no clock. The sweep measures its day from this
	// instant; nothing else records when a file stopped being shown, and deleted_at
	// belongs to a row that is not being deleted.
	//
	// width > 0 is in the WHERE because only a file the image pass measured is ever
	// stamped — the ledger speaks about pictures, files_unused_since_measured CHECKs
	// the stamp against the measurement, and a document upload that loses the last
	// record that named it has no grace period to start. The list returned is wider
	// than what the sweep can act on, and the sweep's own predicate asks width > 0.
	at := db.Now()
	if err := tx.DB().WithContext(ctx).Model(&contracts.File{}).
		Where("id IN ? AND deleted_at IS NULL AND width > 0", unused).Update("unused_since", &at).Error; err != nil {
		return nil, fmt.Errorf("file: stamp when %d images stopped being shown: %w", len(unused), err)
	}
	return unused, nil
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
	// earlier release wrote the answer a publish implied into that row column, so the
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
