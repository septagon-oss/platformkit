package internal

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// unused.go is the release: the file nobody is showing anymore, and the clock
// that gives its bytes back after the grace period.
//
// It is a third job and not a mode of the retention sweep for the reason the
// retention sweep's own comment gives for being a second job: the predicate is
// unrelated. That one deletes because a class ran out and refuses any class it
// was never priced for; this one deletes because the ledger stopped naming a
// record that shows the picture, whatever class it arrived under, and it applies
// to every measured image. Two deletions that share a job would be one job whose
// refusals nobody can read, and one `reason` column answering two questions.
//
// It is also not a mode of the orphan sweep: that one starts at the store,
// because its targets are objects no row names, and this one starts at the rows.

// DefaultUnusedAfter is the grace period a deployment that names none gets: a day
// in which a person can move an image out of one draft and into another, or
// abandon an upload and go back to it, before the bytes are given back.
const DefaultUnusedAfter = 24 * time.Hour

// Unused reaps the images nothing is showing.
type Unused struct {
	tenants jobs.TenantLister
	after   time.Duration
	every   time.Duration
}

// NewUnused prepares the release sweep. With no way to walk the tenants there is
// nothing to walk, and Jobs mounts nothing — the rule every job here follows,
// because a sweep that quietly never runs is worse than one that refuses to start.
func NewUnused(tenants jobs.TenantLister, after, every time.Duration) *Unused {
	if after <= 0 {
		after = DefaultUnusedAfter
	}
	return &Unused{tenants: tenants, after: after, every: every}
}

// Jobs is the release sweep, or nothing when no lister was wired.
//
// Once a day, an hour after the retention sweep: the two read the same table for
// different reasons and a deployment that is going to lose an hour of throughput
// to a batch of deletes should lose it while nobody is working.
func (u *Unused) Jobs() []jobs.Job {
	if u.tenants == nil {
		return nil
	}
	job := jobs.Job{Name: "file-unused", Cron: "45 4 * * *"}
	if u.every > 0 {
		job.Cron, job.Every = "", u.every
	}
	job.Run = u.run
	return []jobs.Job{job}
}

// unusedSweep is the batch ceiling, shared with the retention sweep because it is
// the same bound for the same reason: how many rows one tenant may hold a
// connection for while they are removed.
func (u *Unused) run(ctx context.Context, conn *db.Conn) error {
	return jobs.PerTenant(ctx, conn, u.tenants, func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
		return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			now := db.Now()
			var rows []*contracts.File
			// The predicate is the whole rule, in the query: measured, stamped, past
			// the grace, unread, unheld. Every arm is there rather than in the loop
			// for the reason sweepBatch's comment gives — a limit taken before a
			// filter is a window that never moves.
			//
			// width > 0 is the first arm and not an optimisation: only a file the
			// image pass measured is ever stamped (migrations/000055 CHECKs it), and
			// a document upload has no uses, no ledger rows and no business being
			// reaped by a rule about pictures nobody is showing.
			err := tx.DB().WithContext(ctx).Model(&contracts.File{}).
				Where("deleted_at IS NULL AND width > 0 AND unused_since IS NOT NULL AND unused_since <= ?", now.Add(-u.after)).
				Where(`NOT EXISTS (SELECT 1 FROM file_uses u WHERE u.file_id = files.id AND u.deleted_at IS NULL)`).
				Where(liveHold, now).
				Clauses(lockForUpdate(true, contracts.File{}.TableName())).
				Order("unused_since").Order("id").Limit(sweepBatch).Find(&rows).Error
			if err != nil {
				return fmt.Errorf("file: read this tenant's unshown images: %w", err)
			}
			var removed int
			for _, f := range rows {
				// Re-read, not trusted — the two checks the retention sweep names,
				// plus the clock, because a body that started showing this picture
				// again between the read and here both clears the stamp and files the
				// use row, and either one alone makes this delete untrue.
				var uses int64
				if err := tx.DB().WithContext(ctx).Model(&contracts.FileUse{}).
					Where("file_id = ?", f.ID).Count(&uses).Error; err != nil {
					return fmt.Errorf("file: whether %s is shown by anything: %w", f.ID, err)
				}
				if uses > 0 {
					continue
				}
				fresh, err := crud.Get[*contracts.File](tx, f.ID)
				if errors.Is(err, crud.ErrNotFound) {
					continue
				}
				if err != nil {
					return err
				}
				if fresh.UnusedSince == nil || fresh.UnusedSince.After(now.Add(-u.after)) {
					continue
				}
				if live, why, err := held(ctx, tx, f.ID); err != nil {
					return err
				} else if live {
					slog.InfoContext(ctx, "file: a live hold keeps an image nobody is showing",
						"file", f.ID, "tenant", t.Slug, "reason", why)
					continue
				}
				if err := crud.Delete[*contracts.File](tx, f.ID, false); err != nil {
					return err
				}
				widths, err := removeWidths(ctx, tx, f.ID)
				if err != nil {
					return err
				}
				// Two events, in this order, in this transaction. The work order is
				// the only thing that can remove bytes after a commit; the release is
				// the fact a trail and a person read. Either alone is a lie: bytes
				// gone with no announcement, or an announcement with nothing behind it.
				if err := events.Publish(ctx, tx, contracts.EventDeleted, contracts.Deleted{
					FileID: f.ID, StorageKey: f.StorageKey, SHA256: f.SHA256, Size: f.Size,
					VariantKeys: widths, Cause: contracts.EraseUnused, At: now,
				}); err != nil {
					return err
				}
				if err := events.Publish(ctx, tx, contracts.EventReleased, contracts.Released{
					FileID: f.ID, Cause: contracts.EraseUnused, At: now,
				}); err != nil {
					return err
				}
				removed++
			}
			if removed > 0 {
				slog.InfoContext(ctx, "file: released images nothing had shown for the grace period",
					"tenant", t.Slug, "count", removed, "after", u.after)
			}
			return nil
		})
	})
}
