package internal_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestTheRetentionSweepTakesTheExpiredFileBehindAHeldWindow is the same
// starvation as TestTheRetentionSweepReachesEveryFileWhoseClassRanOut arriving
// through the other door.
//
// A class priced far beyond a file's age keeps it out of the batch; a live hold
// keeps it out of nothing unless the query says so, because a hold is placed on
// the files the sweep would otherwise delete — that is what a hold is for. A
// legal hold with no until is held forever, so twenty-one hundred of them ahead
// of one expired file, ordered oldest-first, are a window that never moves for
// as long as the hold stands, which is years.
//
// The sweep's answer is the same shape as the class's: a file a live hold
// protects is not a deletion, so it is not in the batch the tick reads. The Go
// check stays beside it because the decision has to be true at the delete and not
// at the read — which is the half of this a query cannot hold for itself.
//
// Nothing is removed except the expired file, so the case is also the promise
// that a held file survives every tick the hold stands for, read at the row.
func TestTheRetentionSweepTakesTheExpiredFileBehindAHeldWindow(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	svc := internal.NewService(store, filetest.Limit, 0)

	// Older than the file the sweep is after, so the oldest-first read meets the
	// window of holds first: 201 of them, one more than the batch, because a
	// window of exactly sweepBatch is the ceiling itself and one over it is what
	// the ceiling does to a file behind it.
	const heldFiles = 201
	var expired uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for n := range heldFiles {
			f, err := svc.Upload(ctx, held(tx), contracts.Upload{
				Name: fmt.Sprintf("invoice-%03d.txt", n), ContentType: "text/plain", Declared: -1,
				Kind: "invoice", Body: strings.NewReader("a"),
			})
			if err != nil {
				return fmt.Errorf("seed held invoice %d: %w", n, err)
			}
			if err := tx.DB().Exec(`UPDATE files SET created_at = created_at - INTERVAL '200 days' WHERE id = ?`,
				f.ID).Error; err != nil {
				return err
			}
			// Held with no until, which is the shape that never leaves the window
			// on its own: a legal hold released by a person and by no clock.
			if _, err := svc.Retain(ctx, tx, f.ID, nil, "legal hold, no end stated"); err != nil {
				return fmt.Errorf("hold invoice %d: %w", n, err)
			}
		}
		f, err := svc.Upload(ctx, held(tx), contracts.Upload{
			Name: "invoice-late.txt", ContentType: "text/plain", Declared: -1,
			Kind: "invoice", Body: strings.NewReader("b"),
		})
		if err != nil {
			return fmt.Errorf("seed the expired invoice: %w", err)
		}
		expired = f.ID
		return tx.DB().Exec(`UPDATE files SET created_at = created_at - INTERVAL '40 days' WHERE id = ?`,
			expired).Error
	})
	if err != nil {
		t.Fatalf("seed %d held files and one expired: %v", heldFiles, err)
	}

	sweep := internal.NewSweep(store, internal.SweepConfig{
		Retention: map[string]time.Duration{"invoice": keepInvoices},
		Tenants:   sweepWalk{[]tenancy.Tenant{acme}},
	})
	scheduled := sweep.Jobs()
	if len(scheduled) != 1 {
		t.Fatalf("the module schedules %v", scheduled)
	}
	for tick := range 3 {
		if err := scheduled[0].Run(t.Context(), conn); err != nil {
			t.Fatalf("tick %d: %v", tick+1, err)
		}
	}

	var gone bool
	if err := admin.QueryRowContext(t.Context(),
		`SELECT NOT EXISTS (SELECT 1 FROM files WHERE id = $1 AND deleted_at IS NULL)`, expired).Scan(&gone); err != nil {
		t.Fatalf("read the expired file back: %v", err)
	}
	if !gone {
		t.Errorf("the file whose class ran out is still here after three ticks (%s): the 201 files ahead of it are all held, and a batch spent on files this tick cannot delete is a window that never moves",
			expired)
	}
	var survivors int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM files f WHERE f.deleted_at IS NULL AND f.id <> $1`, expired).Scan(&survivors); err != nil {
		t.Fatalf("count the held files: %v", err)
	}
	if survivors != heldFiles {
		t.Errorf("%d of the held files are gone; want all %d — the hold protects them and the sweep reads past them, not through them",
			heldFiles-survivors, heldFiles)
	}
	// Once, and once only: a row the sweep removed is a row its predicate leaves
	// behind on the next tick, and a second work order for one removal would mean
	// the deleted_at IS NULL no longer reaches every arm of the class predicate.
	var published int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM `+outbox+` WHERE name = $1 AND payload->>'fileId' = $2`,
		contracts.EventDeleted, expired).Scan(&published); err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	if published != 1 {
		t.Errorf("the expired file's removal published %d work orders, want one", published)
	}
}
