package internal

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"log/slog"
	"time"

	xdraw "golang.org/x/image/draw"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/jobs"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
)

// variants.go is the encode: the pixels one stored image becomes at one planned
// width, and the clock job that gets round to every pending row.
//
// Two rules run through it. The plan is arithmetic and lives in contracts, so
// this file executes a plan a fake can also run — filetest files the same rows
// with the same keys and no scaler at all. And no blob write happens with a
// transaction open, which is Upload's rule for the same reason: a decode is the
// step whose cost belongs to the caller's file and not to a database connection,
// and a transaction held across two hundred encodes is a pool exhausted by one
// tenant's photographs.

// variantBatch and variantTick bound one tenant's tick: at most variantTick
// encodes, taken in claims of variantBatch rows.
//
// The retention sweep's 200 is the wrong number here because its rows are
// deletes and these are encodes: what bounds this loop is not how many rows can
// be written but how long one tenant may hold the scheduler's attention.
const (
	variantBatch = 20
	variantTick  = 200
)

// Variants is the job that turns pending rows into ready ones.
//
// It is a job and not a subscription for the reason kit/jobs' package comment
// gives: work triggered by the clock belongs here, and "these four widths are
// worth building" is not something that happened. It is also why nothing about an
// upload waits for an encode — the original is stored, charged and served in the
// request that uploaded it, and a srcset names the parent alone until this job
// has been round, which is what "the original is served until they are ready"
// promises. The trace id the job's lines carry is the kernel's: kit/jobs opens
// the tenant span and attaches it before Run is called, so nothing here builds
// one.
type Variants struct {
	svc     *Service
	storage contracts.Storage
	tenants jobs.TenantLister
	every   time.Duration
}

// NewVariants prepares the encode pass. With no way to walk the tenants there is
// nothing to walk, and Jobs says so by mounting nothing — the same rule the
// retention sweep follows, because a job that quietly never runs is the failure
// nobody notices.
func NewVariants(svc *Service, storage contracts.Storage, tenants jobs.TenantLister, every time.Duration) *Variants {
	return &Variants{svc: svc, storage: storage, tenants: tenants, every: every}
}

// Jobs is the encode pass, or nothing when no lister was wired.
func (v *Variants) Jobs() []jobs.Job {
	if v.tenants == nil {
		return nil
	}
	job := jobs.Job{Name: "file-variants", Cron: "*/5 * * * *"}
	if v.every > 0 {
		// The interval is for a deployment that will not wait five minutes and
		// for a test that cannot wait at all; the schedule is the shipped answer.
		job.Cron, job.Every = "", v.every
	}
	job.Run = v.run
	return []jobs.Job{job}
}

func (v *Variants) run(ctx context.Context, conn *db.Conn) error {
	return jobs.PerTenant(ctx, conn, v.tenants, func(ctx context.Context, conn *db.Conn, t tenancy.Tenant) error {
		made := 0
		for made < variantTick {
			ids, err := v.claim(ctx, conn)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				return nil
			}
			for _, id := range ids {
				if err := v.encodeOne(ctx, conn, id); err != nil {
					return err
				}
			}
			made += len(ids)
			if len(ids) < variantBatch {
				// Less than a batch was left to claim, so this tenant's queue is
				// empty and a second pass would read the same nothing.
				return nil
			}
		}
		slog.InfoContext(ctx, "file: image widths: this tenant's turn is over for now",
			"tenant", t.Slug, "encoded", made)
		return nil
	})
}

// claim is one tenant's pending rows, oldest plan first, under
// FOR UPDATE SKIP LOCKED.
//
// What the lock covers is the claim itself, and nothing beyond it: it ends when
// this transaction commits, which is before a single pixel is decoded. So this is
// not the mechanism that keeps two overlapping ticks off one row — that work is
// done by the second reading (`look`) and by the state flip under the row's own
// lock in `finish`, which is why an attempt that arrives after another finished
// writes nothing and charges nothing. Two ticks that overlap in the window between
// one's claim and the other's flip can still both scale the same frame, and both
// write the same derived key; `finish` decides which one's numbers the row carries.
// SKIP LOCKED is here for the window the lock does hold: a row another tick is
// still sitting on is skipped rather than waited on, so nothing ever blocks on an
// encode. The order is the oldest first, which is the one reading order where a
// reader never waits behind a newer upload's four widths.
//
// Ids come back rather than rows, because what happens next is a decode and a
// decoded frame held while a transaction is open is the thing this file exists to
// avoid.
func (v *Variants) claim(ctx context.Context, conn *db.Conn) ([]uuid.UUID, error) {
	var ids []uuid.UUID
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var rows []*contracts.Variant
		err := tx.DB().WithContext(ctx).Model(&contracts.Variant{}).
			Where("state = ? AND deleted_at IS NULL", contracts.VariantPending).
			Clauses(lockForUpdate(true, contracts.Variant{}.TableName())).
			Order("created_at").Order("id").Limit(variantBatch).Find(&rows).Error
		if err != nil {
			return fmt.Errorf("file: read this tenant's pending image widths: %w", err)
		}
		ids = make([]uuid.UUID, 0, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		return nil
	})
	return ids, err
}

// encodeOne is one row: read the plan, read the picture, scale, write the object,
// and only then ask the quota and flip the state.
//
// Every step re-reads its own truth, because the claim's lock was released when
// the claim's transaction committed: between the claim and the write the file may
// have been deleted, another worker may have finished this same width, or this
// tenant's room may have gone. What the store was told is never undone by a
// refusal — the bytes written are removed again — and a refusal never touches the
// original, which was accepted, charged and served in its own request.
func (v *Variants) encodeOne(ctx context.Context, conn *db.Conn, id uuid.UUID) error {
	row, parent, err := v.look(ctx, conn, id)
	if err != nil || row == nil {
		return err
	}
	scope, err := contracts.ScopeOf(ctx)
	if err != nil {
		return err
	}
	encoded, height, reason, err := v.scale(ctx, scope, parent, row)
	if errors.Is(err, errWidthDeferred) {
		// Nothing was written and nothing was decided: the frame could not be read
		// right now. The row stays pending, which is the only state a later tick
		// claims, so the encode happens when the store answers — and the tick fails,
		// which is what makes the failure legible to whoever reads the job's log
		// rather than a row that says a healthy picture was undecodable.
		slog.WarnContext(ctx, "file: image width deferred: the frame could not be read",
			"file", row.FileID, "width", row.Width, "error", err)
		return err
	}
	if err != nil {
		// The reason is written and the row is left failed: a frame no decoder
		// reads never becomes readable and a quota refusal is permanent by
		// decision, so a job that retried either forever is a job whose failure
		// nobody can read off the row. The original keeps serving; this width does
		// not, and `reason` is what a person asks when they notice.
		slog.WarnContext(ctx, "file: image width not built",
			"file", row.FileID, "width", row.Width, "reason", reason, "error", err)
		return v.fail(ctx, conn, id, reason)
	}
	key := contracts.Key(row.StorageKey)
	// Delete before Put, on a key derived from (file, width) and never minted: an
	// attempt that died between the two left an object at this name and a row
	// still pending, so the retry replaces it instead of colliding with it, and
	// while the state is pending no reader is served from it either way.
	if err := v.storage.Delete(ctx, scope, key); err != nil {
		return fmt.Errorf("file: clear %s before writing it: %w", key, err)
	}
	meta := contracts.MetaFor(&contracts.File{ContentType: parent.ContentType, Visibility: parent.Visibility})
	if err := v.storage.Put(ctx, scope, key, bytes.NewReader(encoded), int64(len(encoded)), meta); err != nil {
		return fmt.Errorf("file: store %s: %w", key, err)
	}
	if err := v.finish(ctx, conn, id, encoded, height); err != nil {
		// The row did not move, so the bytes are not anybody's to serve: take
		// them back and let the next tick, or the orphan sweep, be rid of them.
		_ = v.storage.Delete(ctx, scope, key)
		if errors.Is(err, errQuotaWidth) {
			// Named on the row and not retried: this tenant's disk is the reason,
			// and the reason will not change by asking again in five minutes.
			slog.InfoContext(ctx, "file: image width refused: no room left",
				"file", row.FileID, "width", row.Width, "bytes", len(encoded))
			return v.fail(ctx, conn, id, contracts.VariantFailedQuota)
		}
		return err
	}
	return nil
}

// look is the claim's second reading: the row, its parent, and nothing at all
// when either has gone or the row is not pending any more.
func (v *Variants) look(ctx context.Context, conn *db.Conn, id uuid.UUID) (*contracts.Variant, *contracts.File, error) {
	var row *contracts.Variant
	var parent *contracts.File
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		r, err := crud.Get[*contracts.Variant](tx, id)
		if errors.Is(err, crud.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		f, err := crud.Get[*contracts.File](tx, r.FileID)
		if errors.Is(err, crud.ErrNotFound) {
			// The picture went away between the claim and here. The row goes with
			// its parent and there is nothing to scale meanwhile.
			return nil
		}
		if err != nil {
			return err
		}
		if r.State != contracts.VariantPending {
			return nil
		}
		row, parent = r, f
		return nil
	})
	return row, parent, err
}

// scale reads the parent's bytes and returns the frame at this width, with no
// transaction open for the length of the decode.
//
// The container is the parent's and is not re-decided: alpha was scanned once, on
// the frame, and a variant that scanned again could disagree with the picture it
// was scaled from — a PNG with transparency downscaled into a JPEG would lose the
// transparency its own parent keeps. The quality is contracts.JPEGQuality, the
// constant the upload pass encodes with, because two constants for one number is
// one of them going stale.
func (v *Variants) scale(ctx context.Context, scope contracts.Scope, parent *contracts.File, row *contracts.Variant) ([]byte, int, string, error) {
	body, err := v.storage.Get(ctx, scope, contracts.Key(parent.StorageKey))
	if err != nil {
		// Not a judgement about the picture: the provider did not answer. A frame
		// that is sitting healthy in a store that restarted is the common case, and
		// filing it as `decode` would be a permanent verdict on a temporary outage.
		return nil, 0, "", fmt.Errorf("%w: read %s back: %w", errWidthDeferred, parent.StorageKey, err)
	}
	defer body.Close()
	src, _, err := image.Decode(body)
	if err != nil {
		return nil, 0, contracts.VariantFailedDecode, fmt.Errorf("decode %s: %w", parent.StorageKey, err)
	}
	height := contracts.VariantHeight(parent.Width, parent.Height, row.Width)
	if height <= 0 {
		return nil, 0, contracts.VariantFailedDecode, fmt.Errorf("%s carries no frame to scale", parent.StorageKey)
	}
	dst := image.NewRGBA(image.Rect(0, 0, row.Width, height))
	// Catmull-Rom: a box filter on a photographic frame is visibly coarse at 480
	// px, and a filter chosen because it is what the other side reaches for is one
	// fewer judgement call to defend. It reads the decoded frame and writes one
	// RGBA of the planned size, so the pixels in between are never stored.
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	out := &bytes.Buffer{}
	if parent.ContentType == "image/png" {
		if err := png.Encode(out, dst); err != nil {
			return nil, 0, contracts.VariantFailedStore, fmt.Errorf("re-encode %s at %d px: %w", parent.StorageKey, row.Width, err)
		}
		return out.Bytes(), height, "", nil
	}
	if err := jpeg.Encode(out, dst, &jpeg.Options{Quality: contracts.JPEGQuality}); err != nil {
		return nil, 0, contracts.VariantFailedStore, fmt.Errorf("re-encode %s at %d px: %w", parent.StorageKey, row.Width, err)
	}
	return out.Bytes(), height, "", nil
}

// finish spends the quota and flips the row, in one transaction, under the row's
// own lock.
//
// The order is Upload's: charge first, then write, because the quota is a sum over
// sizes and the row's size is the charge — a row written before the sum was read
// would count itself twice, and one charged after it was written would count
// itself not at all. Re-checking the state under the lock is what makes the
// second tick behind a slow encode find nothing left to do rather than write a
// fresh digest over a row that already has one: a ready row is never rewritten,
// which is the same write-once rule Storage.Put enforces over the object.
func (v *Variants) finish(ctx context.Context, conn *db.Conn, id uuid.UUID, encoded []byte, height int) error {
	return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		r, err := crud.GetForUpdate[*contracts.Variant](tx, id)
		if errors.Is(err, crud.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		if r.State != contracts.VariantPending {
			return nil
		}
		if _, err := crud.Get[*contracts.File](tx, r.FileID); err != nil {
			return nil
		}
		if err := v.svc.charge(ctx, tx, int64(len(encoded))); err != nil {
			// No room, so no width. The refusal is permanent and named: the
			// original was already accepted and charged, and this module does not
			// reach back to refuse an upload that is already serving.
			return fmt.Errorf("%w: %w", errQuotaWidth, err)
		}
		sum := sha256.Sum256(encoded)
		r.State, r.Size = contracts.VariantReady, int64(len(encoded))
		r.SHA256, r.Height = hex.EncodeToString(sum[:]), height
		return crud.Update(ctx, tx, r)
	})
}

// fail leaves a row failed, with the reason, once.
//
// A row that is no longer pending is not touched: a ready row is a fact about
// bytes that exist, and nothing about a later failure of an unrelated attempt
// should be able to un-say it.
func (v *Variants) fail(ctx context.Context, conn *db.Conn, id uuid.UUID, reason string) error {
	if reason == "" {
		reason = contracts.VariantFailedStore
	}
	return db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		r, err := crud.GetForUpdate[*contracts.Variant](tx, id)
		if err != nil || r.State != contracts.VariantPending {
			return err
		}
		r.State, r.Reason = contracts.VariantFailed, reason
		return crud.Update(ctx, tx, r)
	})
}

// errQuotaWidth marks a width that will not fit, so encodeOne can tell a quota
// refusal — permanent, and it should say so on the row — from any other failure
// of the transaction that just needs the tick to fail and retry.
var errQuotaWidth = errors.New("file: this width does not fit this tenant's remaining quota")

// errWidthDeferred marks an attempt that learned nothing and decided nothing, so
// encodeOne can leave the row pending and let the tick fail. The permanent
// reasons — a frame no decoder reads, a width that will not fit — are the two
// this sentinel is not, and the difference is what a `reason` column means.
var errWidthDeferred = errors.New("file: this width waits for another tick")
