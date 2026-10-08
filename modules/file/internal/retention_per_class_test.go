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

// keepPhotosForever is a class priced long enough that no file of it is ever a
// deletion within the case: five years old against a hundred-year class.
const keepPhotosForever = 100 * 365 * 24 * time.Hour

// TestTheRetentionSweepKeepsAClassPricedLongBesideOnePricedShort reads the half
// of the sweep's predicate that a batch limit cannot see.
//
// A cutoff is a per-class thing: the date a photo has to outlive is not the date
// an invoice has to outlive, and one deployment's "delete the invoices after a
// month" says nothing about the photos. So the sweep reads one
// (kind, created_at <= cutoff) arm per class the deployment priced rather than a
// single newest-date over the table. Read the two classes as one date — the
// shortest of them, which is the tempting min(), because it is the one that
// drains the tenant's backlog in a tick — and every photo older than a month
// leaves the store on the same tick that an expired invoice does, while the
// deployment's own table says those photos are kept for a century.
//
// The age matters here: a long class whose rows are all young distinguishes
// nothing, because any cutoff leaves them alone. So this case ages the long
// class past the short class's window — the state a real tenant reaches without
// noticing, because photos keep arriving for years and invoices expire monthly —
// and asks of the store, after the tick, which rows left. The answer has to be
// exactly the class whose own window closed.
func TestTheRetentionSweepKeepsAClassPricedLongBesideOnePricedShort(t *testing.T) {
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
			// Five years old: inside the photo class's own window, far outside the
			// invoice's, which is what tells one class's cutoff from the other's.
			if err := tx.DB().Exec(`UPDATE files SET created_at = created_at - INTERVAL '5 years' WHERE id = ?`,
				f.ID).Error; err != nil {
				return err
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
		return tx.DB().Exec(`UPDATE files SET created_at = created_at - INTERVAL '40 days' WHERE id = ?`,
			expired).Error
	})
	if err != nil {
		t.Fatalf("seed %d photos and one invoice: %v", photos, err)
	}

	sweep := internal.NewSweep(store, internal.SweepConfig{
		Retention: map[string]time.Duration{
			"invoice": keepInvoices,
			"photo":   keepPhotosForever,
		},
		Tenants: sweepWalk{[]tenancy.Tenant{acme}},
	})
	scheduled := sweep.Jobs()
	if len(scheduled) != 1 {
		t.Fatalf("the module schedules %v", scheduled)
	}
	if err := scheduled[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("tick: %v", err)
	}

	// The rows the tick took, read at the store rather than at what the job says
	// it did: every file still here has to be a photo, and every photo has to be
	// here.
	var left int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM files WHERE kind = 'photo' AND deleted_at IS NULL`).Scan(&left); err != nil {
		t.Fatalf("count the photos: %v", err)
	}
	if left != photos {
		var deleted int
		if err := admin.QueryRowContext(t.Context(),
			`SELECT count(*) FROM files WHERE kind = 'photo' AND deleted_at IS NOT NULL`).Scan(&deleted); err != nil {
			t.Fatalf("count the photos that left: %v", err)
		}
		t.Errorf("the sweep removed %d of %d files whose own class runs out in %v, keeping %d: only the class whose own window closed may leave, and the deployment priced photo at %v",
			deleted, photos, keepPhotosForever, left, keepPhotosForever)
	}
	var gone bool
	if err := admin.QueryRowContext(t.Context(),
		`SELECT NOT EXISTS (SELECT 1 FROM files WHERE id = $1 AND deleted_at IS NULL)`, expired).Scan(&gone); err != nil {
		t.Fatalf("read the invoice back: %v", err)
	}
	if !gone {
		t.Errorf("the invoice whose class ran out is still here (%s): the tick read a batch and found nothing to delete",
			expired)
	}
	// Each removal is one work order naming its own class's cause, and nothing
	// else published one: a photo that left would have left with a work order
	// beside it, so the outbox is the second read of the same answer.
	var orders int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM `+outbox+` WHERE name = $1`, contracts.EventDeleted).Scan(&orders); err != nil {
		t.Fatalf("count the work orders: %v", err)
	}
	if orders != 1 {
		t.Errorf("the tick published %d removal work orders, want one — the invoice's — because %d photos are inside their class",
			orders, photos)
	}
}
