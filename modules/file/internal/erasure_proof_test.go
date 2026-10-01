package internal_test

import (
	"context"
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

// TestAnErasureProofRowFilesTheReasonAndTheSweepFilesNone is the half the trail
// assertion cannot reach: which of the two places the reason actually lives.
//
// The outbox is transport. A removal's work order carries the sentence so the
// worker can file it, and a delivery that has been handled is not where anybody
// answers a data-protection question in two years — file_erasures is, because it
// is the record that outlives the row it proofs. So the reason has to be read
// back out of the proof table by name, not merely be present somewhere in what
// the transaction wrote.
//
// The sweep is the other half, and it is why the column defaults instead of
// demanding: an expired file was removed by a class and a date, nobody was asked
// for a sentence, and a proof that invented one would be the record of a
// conversation nobody had.
func TestAnErasureProofRowFilesTheReasonAndTheSweepFilesNone(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	svc := internal.NewService(store, filetest.Limit, 0)
	const reason = "data protection request 2026-0412"
	subject, officer := uuid.New(), uuid.New()

	// Two files, two owners of the upload: the diary belongs to the subject the
	// command erases, and the old invoice belongs to the tenant at large, so the
	// sweep is the one that removes it. One subject for both would leave the
	// sweep nothing to do and the second half of this case unasked.
	seed := func(who tenancy.Tenant, actor uuid.UUID, name, kind string, agoDays int) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		err := db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), who), actor), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				f, err := svc.Upload(ctx, held(tx), contracts.Upload{
					Name: name, ContentType: "text/plain", Declared: -1,
					Kind: kind, Body: strings.NewReader("the body of " + name),
				})
				if err != nil {
					return err
				}
				id = f.ID
				if agoDays == 0 {
					return nil
				}
				return tx.DB().Exec(`UPDATE files SET created_at = created_at - make_interval(days => ?) WHERE id = ?`,
					agoDays, id).Error
			})
		if err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
		return id
	}
	diary := seed(acme, subject, "diary.txt", "invoice", 0)
	invoice := seed(acme, officer, "invoice-old.txt", "invoice", 40)

	err := db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), officer), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			receipt, err := svc.EraseSubject(ctx, tx, subject, reason)
			if err == nil && receipt.Files != 1 {
				t.Errorf("the erasure removed %d files, want the subject's one", receipt.Files)
			}
			return err
		})
	if err != nil {
		t.Fatalf("erase the subject: %v", err)
	}
	scheduled := internal.NewSweep(store, internal.SweepConfig{
		Retention: map[string]time.Duration{"invoice": keepInvoices},
		Tenants:   sweepWalk{[]tenancy.Tenant{acme}},
	}).Jobs()
	if err := scheduled[0].Run(t.Context(), conn); err != nil {
		t.Fatalf("the retention sweep: %v", err)
	}
	if err := deliverEach(t.Context(), conn, acme, internal.EraseBlobs(store)); err != nil {
		t.Fatalf("deliver the removals: %v", err)
	}

	type proof struct{ cause, reason string }
	rows, err := admin.QueryContext(t.Context(),
		`SELECT file_id, cause, reason FROM file_erasures`)
	if err != nil {
		t.Fatalf("read the proofs: %v", err)
	}
	defer rows.Close()
	got := map[uuid.UUID]proof{}
	for rows.Next() {
		var id uuid.UUID
		var p proof
		if err := rows.Scan(&id, &p.cause, &p.reason); err != nil {
			t.Fatalf("read a proof row: %v", err)
		}
		got[id] = p
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the proofs: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("the two removals left %d proof rows, want one each", len(got))
	}
	if got[diary] != (proof{contracts.EraseSubject, reason}) {
		t.Errorf("the subject's proof row is %+v, want cause %q and reason %q",
			got[diary], contracts.EraseSubject, reason)
	}
	if got[invoice] != (proof{contracts.EraseExpired, ""}) {
		t.Errorf("the sweep's proof row is %+v; it was asked nothing, and a proof that invented a sentence would be a record of a conversation nobody had",
			got[invoice])
	}
}
