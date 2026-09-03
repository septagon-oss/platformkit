package internal

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// reconcileCron is a quarter to four in the morning, daily. An orphan costs
// disk and nothing else, so this is the least urgent work in the application
// and it runs when nothing else does.
const reconcileCron = "45 3 * * *"

// orphanAge is how old a blob has to be before it is considered abandoned. An
// upload writes the bytes and then the row, and the two are one HTTP request
// apart, so an hour is four orders of magnitude more slack than the window it
// is protecting — and the cost of getting it wrong is deleting the bytes of a
// file somebody has just uploaded.
const orphanAge = time.Hour

// Reconcile removes blobs no row references.
//
// It is the one piece of periodic work this module has, and it exists because
// of the ordering the module chose on purpose: an upload writes the bytes
// first, so a transaction that fails afterwards leaves bytes nobody references.
// That is the right trade — the other order leaves a row whose download fails
// forever — and this is the cost of it, swept up once a day.
//
// It is not jobs.PerTenant, and the reason is the shape of the problem rather
// than a preference. PerTenant walks the tenants and hands each one a context;
// a blob on disk carries no tenant, and the ones this job is looking for are
// exactly the ones no row names, so there is no tenant to walk to them from.
// The sweep therefore starts at the store — Lister enumerates the keys — and
// asks the database which of them are known, under system access, because the
// question crosses every tenant by construction: a key belonging to another
// tenant's row is a key that must not be deleted, and a tenant-scoped query
// could not see it.
type Reconcile struct {
	storage contracts.Storage
	every   time.Duration
	token   tenancy.SystemToken
}

// NewReconcile prepares the sweep. every replaces the daily schedule for a test.
func NewReconcile(storage contracts.Storage, every time.Duration) *Reconcile {
	return &Reconcile{storage: storage, every: every}
}

// Use hands over the capability that opens a cross-tenant transaction. It is
// called from Module.Routes, which is the one moment the kernel offers a token
// — a job is constructed before the API exists — and the job does nothing
// without one, which is a boot-order mistake rather than a request-time
// condition, so it says so in the log rather than deleting anything.
func (r *Reconcile) Use(token tenancy.SystemToken) { r.token = token }

// Jobs is the daily sweep, or none: a Storage that cannot enumerate what it
// holds cannot be reconciled, and a job that could never do anything is worse
// than no job because it appears in the schedule.
func (r *Reconcile) Jobs() []jobs.Job {
	if _, ok := r.storage.(contracts.Lister); !ok {
		return nil
	}
	job := jobs.Job{Name: "file-reconcile", Cron: reconcileCron, Run: r.run}
	if r.every > 0 {
		job.Cron, job.Every = "", r.every
	}
	return []jobs.Job{job}
}

// batch is how many keys one query asks about. A store with a million blobs is
// a million-element IN list otherwise, which Postgres will accept and nobody
// should send.
const batch = 500

func (r *Reconcile) run(ctx context.Context, conn *db.Conn) error {
	lister, ok := r.storage.(contracts.Lister)
	if !ok {
		return nil
	}
	if r.token == nil {
		return fmt.Errorf("file: the reconciliation was never given the system capability; Module.Routes hands it over")
	}
	keys, err := lister.Keys(ctx, time.Now().Add(-orphanAge))
	if err != nil {
		return fmt.Errorf("file: list what the store holds: %w", err)
	}
	var removed int
	for chunk := range slices.Chunk(keys, batch) {
		var known []string
		err := db.RunSystem(ctx, conn, r.token, func(_ context.Context, tx db.Tx[db.System]) error {
			return tx.DB().Model(&contracts.File{}).Where("storage_key IN ?", chunk).
				Pluck("storage_key", &known).Error
		})
		if err != nil {
			return fmt.Errorf("file: which of these blobs are known: %w", err)
		}
		for _, key := range chunk {
			if slices.Contains(known, key) {
				continue
			}
			if err := r.storage.Delete(ctx, key); err != nil {
				return fmt.Errorf("file: remove the orphan %s: %w", key, err)
			}
			removed++
		}
	}
	if removed > 0 {
		slog.InfoContext(ctx, "file: removed blobs no row references", "count", removed)
	}
	return nil
}

// Keys is contracts.Lister on the filesystem: every blob written before before.
//
// It walks the two-character directories Put creates and reads each entry's
// modification time, which is when the upload finished writing it.
func (l *Local) Keys(_ context.Context, before time.Time) ([]string, error) {
	var out []string
	err := filepath.WalkDir(l.dir, func(at string, e fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case e.IsDir():
			return nil
		case !key.MatchString(e.Name()):
			// Not something this package wrote. A directory somebody else's
			// backup tool left here is not this job's to delete.
			return nil
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(before) {
			out = append(out, e.Name())
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("file: walk %s: %w", l.dir, err)
	}
	return out, nil
}

var _ contracts.Lister = (*Local)(nil)
