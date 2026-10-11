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

// keepPhotos is a class the deployment priced long, which is the state that
// makes a batch expensive: not a class nobody priced — the sweep cannot see one
// of those at all now — but one it did price, far out, so its files are this
// tenant's oldest rows and never a deletion.
const keepPhotos = 100 * 365 * 24 * time.Hour

// TestTheRetentionSweepDoesNotSpendItsBatchOnAClassItWasNotGiven is the class
// door of the same starvation, aged the way the read meets it.
//
// The sweep reads oldest first, which is the order that drains a backlog and the
// order a tenant's history arrives in. It is also the order that puts a
// hundred-year class in front of a thirty-day one: photos uploaded eighteen
// months ago are older than an invoice uploaded five weeks ago, so on a tenant
// that has been keeping photos, the front of the read is always photos and the
// ceiling is spent on rows the policy will never delete.
//
// That is the same quiet failure with a different cause — the batch is a batch
// of whatever sorts first rather than of the classes the deployment named — and
// the fix is the class predicate itself: one (kind, cutoff) arm per priced class
// in the query, so a class the sweep was not given is not in the batch it reads.
// The batch ceiling stays, which is what the case checks alongside: two hundred
// and one rows this tenant owns, and the tick still finishes in one read.
func TestTheRetentionSweepDoesNotSpendItsBatchOnAClassItWasNotGiven(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	svc := internal.NewService(store, filetest.Limit, 0, 0)

	const photos = 201
	var expired uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for n := range photos {
			f, err := svc.Upload(ctx, held(tx), contracts.Upload{
				Name: fmt.Sprintf("photo-%03d.txt", n), ContentType: "text/plain", Declared: -1,
				Kind: "photo", Body: strings.NewReader("a"),
			})
			if err != nil {
				return fmt.Errorf("seed photo %d: %w", n, err)
			}
			// Older than the file the sweep is after, so the oldest-first read
			// meets the priced class first.
			if err := tx.DB().Exec(`UPDATE files SET created_at = created_at - INTERVAL '500 days' WHERE id = ?`,
				f.ID).Error; err != nil {
				return err
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
		t.Fatalf("seed %d photos and one expired invoice: %v", photos, err)
	}

	sweep := internal.NewSweep(store, internal.SweepConfig{
		Retention: map[string]time.Duration{"invoice": keepInvoices, "photo": keepPhotos},
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
		t.Errorf("the file whose class ran out is still here after three ticks (%s): the %d rows ahead of it are a class priced at %v, so the read is spent on files no policy covers",
			expired, photos, keepPhotos)
	}
	var kept int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM files WHERE kind = 'photo' AND deleted_at IS NULL`).Scan(&kept); err != nil {
		t.Fatalf("count the photos: %v", err)
	}
	if kept != photos {
		t.Errorf("%d of the %d photos are gone; a long window is a window, not no window", photos-kept, photos)
	}
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
