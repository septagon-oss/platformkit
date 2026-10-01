package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm/clause"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// Retain places or replaces one file's hold. See contracts.Service.
//
// The file row is locked first, with crud.GetForUpdate, and the hold is written
// under that lock: without it, a hold and a sweep could read the same "no live
// hold" and one of them would be wrong about what it was allowed to delete.
// Locking the root before the child is the only ordering rule here, and it is
// the same one charge takes on the tenant.
func (s *Service) Retain(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID, until *time.Time, reason string) (*contracts.Hold, error) {
	if _, err := crud.GetForUpdate[*contracts.File](tx, fileID); err != nil {
		return nil, err
	}
	now := db.Now()
	if until != nil && !until.After(now) {
		return nil, fmt.Errorf("%w: a hold that expires at or before now holds nothing", crud.ErrInvalid)
	}
	actor, _ := tenancy.ActorFrom(ctx)
	holder, err := holdOn(tx, fileID)
	if err != nil {
		return nil, err
	}
	hold := &contracts.Hold{FileID: fileID, Until: until, Reason: reason, PlacedBy: actor}
	if holder != nil {
		// Replaced, not added: two holds on one file is one question — until when
		// — with two answers, and the file would be held to whichever a query
		// happened to read first.
		hold.Base = holder.Base
		if err := crud.Update(ctx, tx, hold); err != nil {
			return nil, err
		}
	} else if err := crud.Create(ctx, tx, hold); err != nil {
		return nil, err
	}
	return hold, events.Publish(ctx, tx, contracts.EventRetained, contracts.Retained{
		FileID: fileID, Until: until, Reason: reason, At: now,
	})
}

// Release removes one file's hold. See contracts.Service.
//
// No hold is success and not ErrNotFound: the caller's intent is "this file is
// governed by its class again", and that is already true. Refusing would be
// refusing a write that found nothing, which this architecture refuses on
// purpose.
func (s *Service) Release(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID) error {
	if _, err := crud.GetForUpdate[*contracts.File](tx, fileID); err != nil {
		return err
	}
	holder, err := holdOn(tx, fileID)
	if err != nil {
		return err
	}
	if holder == nil {
		// No hold is success: the caller's intent is "this file is governed by
		// its class again", which is already true.
		return nil
	}
	if err := crud.Delete[*contracts.Hold](tx, holder.ID, false); err != nil {
		return err
	}
	return events.Publish(ctx, tx, contracts.EventReleased, contracts.Released{
		FileID: fileID, At: db.Now(),
	})
}

// held reports whether a live hold stops this file from being removed, and the
// reason when it does. Every removal asks: the sweep before it deletes, a
// person's delete before it writes, and the subject erasure before it publishes
// a single work order.
// holdOn reads the one hold a file may have, or nil. It is a raw select and not
// crud.List because List checks a filter name against the entity's field names,
// and "the hold on this file" is a lookup by a column no caller ever names:
// List's filters are for a screen's query string, which this is not.
func holdOn(tx db.Tx[db.Tenant], fileID uuid.UUID) (*contracts.Hold, error) {
	var holder []*contracts.Hold
	err := tx.DB().Model(&contracts.Hold{}).Where("file_id = ? AND deleted_at IS NULL", fileID).
		Order("created_at").Limit(1).Find(&holder).Error
	if err != nil {
		return nil, err
	}
	if len(holder) == 0 {
		return nil, nil
	}
	return holder[0], nil
}

func held(ctx context.Context, tx db.Tx[db.Tenant], fileID uuid.UUID) (bool, string, error) {
	holder, err := holdOn(tx, fileID)
	if err != nil || holder == nil {
		return false, "", err
	}
	if !holder.Live(db.Now()) {
		return false, "", nil
	}
	return true, holder.Reason, nil
}

// EraseSubject removes one subject's rows and bytes for this tenant. See
// contracts.Service.
//
// Which rows are a subject's is the module's smallest honest answer: the files
// that subject uploaded. It is a WHERE over uploader_id inside this tenant's own
// transaction — RLS bounds the tenant and the caller bounds the subject — and
// anything wider (a subject's name appears in a document's text, in a comment,
// in another module's row) is a platform erasure and not this module's; see the
// README.
//
// Every row goes in this one transaction, so a refusal writes nothing at all:
// one file under a live hold refuses the whole erasure with ErrHeld and names
// it, rather than removing the other nine and leaving a receipt nobody can act
// on. The bytes then go in the worker that handles each row's event.
//
// The reason is why a person asked, and it is kept: it goes into every work
// order this command publishes, and the worker files it beside the digest in the
// proof row, which is the record that outlives the row it proofs. A hold is a
// decision somebody made and said a reason for and this module says so about the
// hold table; a subject erasure is the same decision with a stronger claim on the
// record, so the sentence a caller typed is read back rather than thrown away.
// Empty is allowed — an erasure can be asked for by a schedule — and one over
// contracts.MaxErasureReason is refused before anything is removed.
func (s *Service) EraseSubject(ctx context.Context, tx db.Tx[db.Tenant], subject uuid.UUID, reason string) (*contracts.ErasureReceipt, error) {
	if subject == uuid.Nil {
		return nil, fmt.Errorf("%w: an erasure names the subject it erases", crud.ErrInvalid)
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > contracts.MaxErasureReason {
		return nil, fmt.Errorf("%w: an erasure's reason is at most %d characters", crud.ErrInvalid, contracts.MaxErasureReason)
	}
	var rows []*contracts.File
	// FOR UPDATE, so a delete and a hold cannot both read this set and each
	// believe it holds the file.
	err := tx.DB().WithContext(ctx).Model(&contracts.File{}).
		Where("uploader_id = ? AND deleted_at IS NULL", subject).
		Clauses(lockForUpdate(false, "")).Order("id").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("file: find what this subject uploaded: %w", err)
	}
	receipt := &contracts.ErasureReceipt{Subject: subject, At: db.Now()}
	if len(rows) == 0 {
		// An erasure of nothing is an answer, not a failure: nothing is written,
		// nothing is published, and the receipt says zero in counts.
		return receipt, nil
	}
	for _, f := range rows {
		if live, why, err := held(ctx, tx, f.ID); err != nil {
			return nil, err
		} else if live {
			return nil, fmt.Errorf("%w: %s is held (%s); release it or erase around this subject", contracts.ErrHeld, f.ID, why)
		}
	}
	for _, f := range rows {
		if err := crud.Delete[*contracts.File](tx, f.ID, false); err != nil {
			return nil, err
		}
		if err := events.Publish(ctx, tx, contracts.EventDeleted, contracts.Deleted{
			FileID: f.ID, StorageKey: f.StorageKey, SHA256: f.SHA256, Size: f.Size,
			Cause: contracts.EraseSubject, Subject: subject, Reason: reason, At: receipt.At,
		}); err != nil {
			return nil, err
		}
		receipt.Files++
		receipt.Bytes += f.Size
	}
	return receipt, nil
}

// SweepConfig is what the retention sweep needs and cannot decide: which class
// lives how long, and who can list the tenants to walk.
type SweepConfig struct {
	// Retention maps a kind to how long its files are kept. It is the product's
	// table and the deployment's durations; this module names no class and
	// interprets no value in it.
	Retention map[string]time.Duration
	Tenants   jobs.TenantLister
	Every     time.Duration
}

// Sweep removes files whose retention class has run out.
//
// It is the opposite of the orphan sweep and that is why it is a second job and
// not a second mode of the first: an orphan is found by starting at the store,
// because the ones it hunts are exactly the ones no row names, and an expired
// file is found by starting at the rows. Starting at the rows means walking the
// tenants, which is jobs.PerTenant, which hands each callback its own
// tenant transaction — so the delete that follows is bounded by that tenant's
// own row-level security and this job never writes another tenant's row, even
// though it runs in one process across all of them.
//
// A kind with no configured policy is never deleted, and never read either: the
// predicate it is found with is built from the configured classes, so an
// unpriced class is not in the batch. That is the right half of the bargain —
// refuse the write that takes the last copy away, never the write that finds
// none, and a class the deployment forgot to price is a configuration mistake
// rather than a licence to delete — and the price is that the sweep has nothing
// to say about a class it never heard of: it reports what it removed, and the
// classes it was given are the only ones it can see.
type Sweep struct {
	storage contracts.Storage
	cfg     SweepConfig
}

// NewSweep prepares the retention job. A nil Tenants lister with a non-empty
// policy is a wiring mistake and module.Module says so at composition.
func NewSweep(storage contracts.Storage, cfg SweepConfig) *Sweep {
	return &Sweep{storage: storage, cfg: cfg}
}

// Jobs is the retention sweep, or none: a policy nobody configured is not a job.
func (s *Sweep) Jobs() []jobs.Job {
	if len(s.cfg.Retention) == 0 || s.cfg.Tenants == nil {
		return nil
	}
	job := jobs.Job{Name: "file-retention", Cron: "15 4 * * *"}
	if s.cfg.Every > 0 {
		job.Cron, job.Every = "", s.cfg.Every
	}
	job.Run = s.run
	return []jobs.Job{job}
}

// sweepBatch is how many files one tenant's tick will take. A tenant that has
// been keeping invoices for a decade should not hold a connection while it
// removes all of them; the next tick takes the rest.
//
// For that sentence to be true, the batch has to be a batch of the files the
// policy covers, and so the cutoff and the hold are in the query and not in the
// loop. A limit taken before the filter is a window over this tenant's files,
// and a window only moves when the rows at the front of it leave: order the
// read by id — a random UUID, drawn identically every tick — keep the rows the
// class says to keep, and the expired file behind them is never read again.
// The files that stay are then not in the window at all, so a tick either makes
// progress or finds nothing, and the ceiling costs latency alone.
const sweepBatch = 200

func (s *Sweep) run(ctx context.Context, conn *db.Conn) error {
	return jobs.PerTenant(ctx, conn, s.cfg.Tenants, func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
		return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			now := db.Now()
			expired, priced := s.covered(now)
			if len(priced) == 0 {
				// Nothing this deployment priced is nothing this job may delete, and
				// an empty predicate would read every row in the table to find that
				// out. Jobs() refuses to schedule this job at all without a policy.
				return nil
			}
			var rows []*contracts.File
			// One arm per priced class, because a cutoff is a per-class thing: the
			// one date a deployment of one shape would use is a different date for
			// every class, and a single min() over the table would delete the short
			// class's files while reading the long one's.
			//
			// The live-hold predicate is a subquery and not a join: a join would put
			// file_holds rows in the FROM, which is where FOR UPDATE looks for rows
			// to lock, and the sweep would take a lock on somebody's hold while it
			// decided not to delete the file beside it. It is in the query for the
			// same reason the cutoff is — a held file is not a deletion, so it does
			// not belong in the batch — and held is asked again below, because the
			// decision has to be true at the moment of the delete, not at the moment
			// of the read.
			err := tx.DB().WithContext(ctx).Model(&contracts.File{}).
				Where("deleted_at IS NULL").
				Where(expired, priced...).Where(liveHold, now).
				Clauses(lockForUpdate(true, contracts.File{}.TableName())).Order("created_at").Limit(sweepBatch).Find(&rows).Error
			if err != nil {
				return fmt.Errorf("file: read this tenant's files for their retention: %w", err)
			}
			var removed int
			for _, f := range rows {
				// Re-read, not trusted: the query is what made this row worth
				// looking at, and these two checks are what makes the delete true.
				cutoff, ok := s.cutoff(f.Kind)
				if !ok || f.CreatedAt.After(cutoff) {
					continue
				}
				if live, why, err := held(ctx, tx, f.ID); err != nil {
					return err
				} else if live {
					slog.InfoContext(ctx, "file: a live hold keeps a file whose class expired",
						"file", f.ID, "tenant", t.Slug, "reason", why)
					continue
				}
				if err := crud.Delete[*contracts.File](tx, f.ID, false); err != nil {
					return err
				}
				if err := events.Publish(ctx, tx, contracts.EventDeleted, contracts.Deleted{
					FileID: f.ID, StorageKey: f.StorageKey, SHA256: f.SHA256, Size: f.Size,
					Cause: contracts.EraseExpired, At: db.Now(),
				}); err != nil {
					return err
				}
				removed++
			}
			if removed > 0 {
				slog.InfoContext(ctx, "file: removed files whose retention class ran out",
					"tenant", t.Slug, "count", removed)
			}
			return nil
		})
	})
}

// liveHold is the sweep's other predicate, as SQL: no hold that is in force at
// the instant the batch is read. It is a NOT EXISTS over file_holds rather than
// a join so the lock stays on the files table (see run), and it is the same
// question contracts.Hold.Live asks in Go — no row, or no expiry, or an expiry
// ahead of now — written the way a row-level security policy can answer it.
const liveHold = `NOT EXISTS (SELECT 1 FROM file_holds h
	WHERE h.file_id = files.id AND h.deleted_at IS NULL
	  AND (h.until IS NULL OR h.until > ?))`

// covered is the deletion predicate as one expression — every arm parenthesised
// and the whole group parenthesised, so it ANDs against the rest of the WHERE as
// one condition and not as the last arm of it — with the kinds it covers beside
// it. A class priced at zero or less is not a policy, and a class the deployment
// never priced is not read here at all: refuse the write that takes the last one
// away, never the write that finds none.
//
// The kinds are walked in sorted order so the SQL one tenant's tick sends is the
// same string every tick, which is what lets Postgres plan it once instead of
// per file.
func (s *Sweep) covered(now time.Time) (clause.Expression, []any) {
	kinds := make([]string, 0, len(s.cfg.Retention))
	for kind, keep := range s.cfg.Retention {
		if keep > 0 {
			kinds = append(kinds, kind)
		}
	}
	slices.Sort(kinds)
	arms := make([]string, 0, len(kinds))
	vars := make([]any, 0, 2*len(kinds))
	for _, kind := range kinds {
		arms = append(arms, "(kind = ? AND created_at <= ?)")
		vars = append(vars, kind, now.Add(-s.cfg.Retention[kind]))
	}
	if len(arms) == 0 {
		return nil, nil
	}
	return clause.Expr{SQL: "(" + strings.Join(arms, " OR ") + ")", Vars: vars}, vars
}

// cutoff is the newest a file of this kind may have been created at to be swept
// now, and false for a kind the deployment never gave a duration — which is
// never a deletion.
func (s *Sweep) cutoff(kind string) (time.Time, bool) {
	keep, ok := s.cfg.Retention[kind]
	if !ok || keep <= 0 {
		return time.Time{}, false
	}
	return db.Now().Add(-keep), true
}

// EraseBlobs is this module's subscription to its own file.deleted, and the
// reason that event exists.
//
// Removing the bytes cannot happen in the transaction that removed the row: a
// file delete is not something a rollback can undo, so a transaction that failed
// after it would leave the row back and the bytes gone — a download that fails
// forever. An event is the only thing in this architecture that is delivered
// exactly after a commit, so the row's removal publishes where the bytes are and
// this handler removes them, checks that nothing is left, and writes the proof in
// the same transaction that claims the delivery.
//
// It is idempotent twice over: kit/events claims each delivery, and a key with
// nothing at it is not an error, so a redelivery after a half-finished attempt
// finishes it. The proof row is unique on (tenant, file, cause), which is what
// makes a redelivery write one row rather than two certificates for one removal.
func EraseBlobs(storage contracts.Storage) events.Subscription {
	return events.Subscription{
		Module: "file",
		Name:   contracts.EventDeleted,
		Handler: func(ctx context.Context, tx db.Tx[db.Tenant], ev events.Event) error {
			var deleted contracts.Deleted
			if err := json.Unmarshal(ev.Payload, &deleted); err != nil {
				return fmt.Errorf("file: read %s: %w", ev.Name, err)
			}
			if deleted.StorageKey == "" {
				return nil
			}
			scope, cause := contracts.ScopeOfTx(tx), deleted.Cause
			if cause == "" {
				// An event published before this module named its causes. The
				// bytes still go; the row says caller, which is what a delete
				// always was.
				cause = contracts.EraseCaller
			}
			if err := storage.Delete(ctx, scope, contracts.Key(deleted.StorageKey)); err != nil {
				// An error rolls the handler's transaction back, which releases
				// the claim, which is what makes the next delivery try again.
				return err
			}
			seen, verified := 0, (*time.Time)(nil)
			if prover, ok := storage.(contracts.Prover); ok {
				n, err := prover.Prove(ctx, scope, contracts.Key(deleted.StorageKey))
				if err != nil {
					return fmt.Errorf("file: prove %s is gone: %w", deleted.StorageKey, err)
				}
				seen = n
				if n == 0 {
					now := db.Now()
					verified = &now
				} else {
					// The bytes are refused as unprovable rather than certified:
					// the delete succeeded and the store still has something at
					// that name, which is a bucket posture this module cannot
					// change. The row is written with the count and a NULL
					// verified_at, because a certificate that lied is worse than
					// no certificate.
					slog.WarnContext(ctx, "file: the store still reports copies of a removed file",
						"file", deleted.FileID, "tenant", db.TenantOf(tx).Slug,
						"storageKey", deleted.StorageKey, "versionsSeen", n)
				}
			}
			proof := &contracts.Erasure{
				FileID: deleted.FileID, StorageKey: deleted.StorageKey, SHA256: deleted.SHA256,
				Size: deleted.Size, Cause: cause, SubjectID: deleted.Subject, Reason: deleted.Reason,
				RemovedAt: db.Now(), VerifiedAt: verified, VersionsSeen: seen,
			}
			if actor, ok := tenancy.ActorFrom(ctx); ok {
				proof.Actor = actor
			}
			proof.TenantID = db.TenantOf(tx).ID
			// The unique index answers "already certified", not the error.
			//
			// Postgres aborts the whole transaction at its first failing statement,
			// so a duplicate-key error this handler caught would still be a
			// transaction that can do nothing else: the commit fails where the
			// insert did, the claim is released, and the delivery is retried forever
			// on a removal that is already proved. kit/crud says the same thing about
			// Update. Asking the index with ON CONFLICT DO NOTHING is what makes a
			// redelivery a no-op that commits — the same reason the registration
			// write in modules/user/internal/registration.go spells a duplicate the
			// same way. Zero rows is the answer "this removal already has its
			// certificate, so this event is its second delivery", and no second
			// file.erased goes out for one removal.
			written := tx.DB().WithContext(ctx).Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "tenant_id"}, {Name: "file_id"}, {Name: "cause"}},
				DoNothing: true,
			}).Create(proof)
			if written.Error != nil {
				return fmt.Errorf("file: record the erasure of %s: %w", deleted.FileID, crud.Classify(written.Error))
			}
			if written.RowsAffected == 0 {
				return nil
			}
			return events.Publish(ctx, tx, contracts.EventErased, contracts.Erased{
				FileID: proof.FileID, StorageKey: proof.StorageKey, SHA256: proof.SHA256,
				Size: proof.Size, Cause: proof.Cause, Subject: proof.SubjectID, Reason: proof.Reason,
				Actor: proof.Actor, VersionsSeen: proof.VersionsSeen, VerifiedAt: proof.VerifiedAt,
			})
		},
	}
}

// lockForUpdate is the row lock a critical section spanning rows needs, and the
// two callers of it want different ones.
//
// A subject erasure takes FOR UPDATE: it is about to delete exactly the set it
// read, and a hold placed by a concurrent request must either see those rows gone
// or make the erasure refuse, never be silently skipped. A SKIP LOCKED is what the
// sweep wants instead: two replicas running the same sweep must not queue behind
// each other row by row, and a row another replica is holding is by definition
// already being removed — the next tick will find it gone. Waiting there would be
// a job that serialises itself across a fleet for no safety at all.
//
// The sweep names its table in OF as well, because it reads with a subquery: FOR
// UPDATE locks the rows of the ranges a statement retrieves from, and naming
// files says which one that is rather than leaving a reader to work out what a
// statement with a NOT EXISTS over file_holds locks.
func lockForUpdate(skipLocked bool, of string) clause.Locking {
	locking := clause.Locking{Strength: "UPDATE"}
	if of != "" {
		locking.Table = clause.Table{Name: of}
	}
	if skipLocked {
		locking.Options = clause.LockingOptionsSkipLocked
	}
	return locking
}
