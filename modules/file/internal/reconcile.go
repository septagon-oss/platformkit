package internal

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"time"

	"github.com/google/uuid"

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
// It is the store-facing sweep, and it exists because of the ordering the module
// chose on purpose: an upload writes the bytes first, so a transaction that fails
// afterwards leaves bytes nobody references. That is the right trade — the other
// order leaves a row whose download fails forever — and this is the cost of it,
// swept up once a day.
//
// It is not jobs.PerTenant, and the reason is the shape of the problem rather
// than a preference. PerTenant walks the tenants and hands each one a tenant
// transaction; the ones this job is looking for are exactly the ones no row
// names, so there is no tenant to walk to them from. It starts at the store and
// asks the database which names are known, under system access, because the
// question crosses every tenant by construction. That is why the port half it
// uses — contracts.Reconciler — takes a db.Tx[db.System] and its predecessor did
// not: an installation-wide listing is a cross-tenant read, and a sweep is the
// only thing allowed to ask for one, so the type says so.
//
// The retention sweep (retention.go) is the other direction — it starts at the
// rows and runs inside each tenant's own transaction — and the two are separate
// jobs precisely because they start at opposite ends.
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
	if _, ok := r.storage.(contracts.Reconciler); !ok {
		return nil
	}
	job := jobs.Job{Name: "file-reconcile", Cron: reconcileCron, Run: r.run}
	if r.every > 0 {
		job.Cron, job.Every = "", r.every
	}
	return []jobs.Job{job}
}

// batch is how many blobs one query asks about. A store with a million blobs is
// a million-element IN list otherwise, which Postgres will accept and nobody
// should send.
const batch = 500

func (r *Reconcile) run(ctx context.Context, conn *db.Conn) error {
	reconciler, ok := r.storage.(contracts.Reconciler)
	if !ok {
		return nil
	}
	if r.token == nil {
		return fmt.Errorf("file: the reconciliation was never given the system capability; Module.Routes hands it over")
	}
	var removed int
	err := db.RunSystem(ctx, conn, r.token, func(ctx context.Context, tx db.Tx[db.System]) error {
		blobs, err := reconciler.Blobs(ctx, tx, time.Now().Add(-orphanAge))
		if err != nil {
			return fmt.Errorf("file: list what the store holds: %w", err)
		}
		for _, chunk := range chunks(blobs, batch) {
			known, err := knownKeys(tx, chunk)
			if err != nil {
				return fmt.Errorf("file: which of these blobs are known: %w", err)
			}
			for _, b := range chunk {
				if slices.Contains(known, b.Key.String()) {
					continue
				}
				// The removal is in the same system transaction as the question
				// that justified it, so a sweep that died halfway reports as
				// failed and retries rather than reporting success over work it
				// did not do. Removing what is gone is success, so a retry is
				// cheap.
				if err := reconciler.RemoveBlob(ctx, tx, b); err != nil {
					return fmt.Errorf("file: remove the orphan %s: %w", b.Key, err)
				}
				removed++
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if removed > 0 {
		slog.InfoContext(ctx, "file: removed blobs no row references", "count", removed)
	}
	return nil
}

// knownKeys asks, under system access, which of these storage keys any tenant's
// row names. System scope is not a shortcut here: a key belonging to another
// tenant's row is a key that must not be deleted, and a tenant-scoped query
// could not see it to protect it.
func knownKeys(tx db.Tx[db.System], chunk []contracts.Blob) ([]string, error) {
	keys := make([]string, len(chunk))
	for i, b := range chunk {
		keys[i] = b.Key.String()
	}
	var known []string
	err := tx.DB().Model(&contracts.File{}).Where("storage_key IN ?", keys).
		Pluck("storage_key", &known).Error
	return known, err
}

// chunks splits a listing so a million blobs never become a million-element IN
// list.
func chunks(blobs []contracts.Blob, n int) [][]contracts.Blob {
	var out [][]contracts.Blob
	for len(blobs) > n {
		out = append(out, blobs[:n])
		blobs = blobs[n:]
	}
	if len(blobs) > 0 {
		out = append(out, blobs)
	}
	return out
}

// RemoveBlob is contracts.Reconciler's other half on the filesystem: remove one
// object the listing named. A blob the listing found in the flat layout carries
// no tenant, and the only name that could have written it is the key itself, so
// uuid.Nil is where the sweep deletes from.
func (l *Local) RemoveBlob(_ context.Context, _ db.Tx[db.System], b contracts.Blob) error {
	if b.TenantID == uuid.Nil {
		if err := os.Remove(l.legacy(b.Key)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("file: remove %s: %w", l.legacy(b.Key), err)
		}
		return nil
	}
	return l.remove(b.TenantID, b.Key)
}
