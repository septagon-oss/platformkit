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

// lastKeySortsLast sorts above every UUID gen_random_uuid can mint (uuid_generate
// never sets all 128 bits, and this repository's keys are v4/v7), so a row given
// this id is outside the batch the retention sweep reads whenever the batch is
// full. It is not a trick the production code needs: it is how a real tenant
// reaches the state — the sweep orders by id and takes the first sweepBatch rows,
// and an id is a random UUID, so which of a tenant's files are "beyond the batch"
// is a draw, and the draw is redone identically every tick.
const lastKeySortsLast = "ffffffff-ffff-ffff-ffff-ffffffffffff"

// TestTheRetentionSweepReachesEveryFileWhoseClassRanOut is the batch's other
// half. The job's comment says the ceiling costs only latency — "the next tick
// takes the rest" — and that sentence is only true if the rest is what the tick
// reads next.
//
// The query is files with no predicate but deleted_at, ordered by id, limited to
// one batch; the cutoff, the class and the hold are all applied afterwards, in
// Go. So the batch is not a batch of expired files, it is a batch of this
// tenant's files, and a tenant whose earliest rows are of a class it never priced
// (or one priced at ten years) spends every tick reading those and deleting
// nothing. The expired file beyond row sweepBatch is never read, on this tick or
// on any later one, because the rows ahead of it are never removed: they are the
// rows the policy says to keep.
//
// That is a retention promise that quietly stops holding — the failure mode
// module.go names as the one worth panicking about, arriving through the back
// door rather than the missing one. A class the deployment priced is a legal
// ceiling on how long a byte may sit; a file that outruns it because of where its
// UUID fell is a file kept past the law that says to remove it, with no log line
// saying so.
func TestTheRetentionSweepReachesEveryFileWhoseClassRanOut(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	svc := internal.NewService(store, filetest.Limit, 0, 0)

	// One more file than the job's own batch, all of them in a class the
	// deployment priced far beyond their age: none of these is ever a deletion,
	// and together they fill the window the sweep reads.
	const keepers = 201
	var expired uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for n := range keepers {
			if _, err := svc.Upload(ctx, held(tx), contracts.Upload{
				Name: fmt.Sprintf("photo-%03d.txt", n), ContentType: "text/plain", Declared: -1,
				Kind: "photo", Body: strings.NewReader("a"),
			}); err != nil {
				return fmt.Errorf("seed photo %d: %w", n, err)
			}
		}
		f, err := svc.Upload(ctx, held(tx), contracts.Upload{
			Name: "invoice.txt", ContentType: "text/plain", Declared: -1,
			Kind: "invoice", Body: strings.NewReader("b"),
		})
		if err != nil {
			return fmt.Errorf("seed the invoice: %w", err)
		}
		expired = f.ID
		// The invoice is past its class's window before the job starts, and it
		// sorts above every keeper, which is the state the case is about.
		if err := tx.DB().Exec(`UPDATE files SET created_at = created_at - INTERVAL '40 days' WHERE id = ?`,
			expired).Error; err != nil {
			return err
		}
		return tx.DB().Exec(`UPDATE files SET id = ? WHERE id = ?`, lastKeySortsLast, expired).Error
	})
	if err != nil {
		t.Fatalf("seed %d files: %v", keepers+1, err)
	}
	expired = uuid.MustParse(lastKeySortsLast)

	sweep := internal.NewSweep(store, internal.SweepConfig{
		Retention: map[string]time.Duration{
			"invoice": keepInvoices,
			"photo":   100 * 365 * 24 * time.Hour,
		},
		Tenants: sweepWalk{[]tenancy.Tenant{acme}},
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

	// The keeper's age and the invoice's age are what the policy reads, so the
	// row is the whole answer: the file whose class ran out is gone, and the ones
	// inside their window are where they were.
	var left int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM files WHERE kind = 'photo' AND deleted_at IS NULL`).Scan(&left); err != nil {
		t.Fatalf("count the keepers: %v", err)
	}
	if left != keepers {
		t.Errorf("the sweep removed %d files whose class had not run out, want none", keepers-left)
	}
	var gone bool
	if err := admin.QueryRowContext(t.Context(),
		`SELECT NOT EXISTS (SELECT 1 FROM files WHERE id = $1 AND deleted_at IS NULL)`, expired).Scan(&gone); err != nil {
		t.Fatalf("read the expired file back: %v", err)
	}
	if !gone {
		t.Errorf("the file whose class ran out is still here after three ticks (%s): the batch the sweep reads is a batch of this tenant's files, not of the ones its policy covers",
			expired)
	}
	var published int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM `+outbox+` WHERE name = $1 AND payload->>'cause' = $2 AND payload->>'fileId' = $3`,
		contracts.EventDeleted, contracts.EraseExpired, expired).Scan(&published); err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	if published != 1 {
		t.Errorf("the expired file's removal published %d work orders, want one", published)
	}
}
