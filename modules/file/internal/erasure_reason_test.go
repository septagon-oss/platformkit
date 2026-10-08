package internal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestAnErasureRecordsTheReasonItWasAskedFor is the erasure's own sentence, kept.
//
// The route declares a reason on the command body and documents it as "Why, for
// the audit trail"; Service.EraseSubject takes the same parameter and the audit
// trail never sees it. The row it writes (file_erasures) records cause, subject,
// actor, digest and proof — and not one word about why a person asked. The
// deleted event payload that reaches modules/audit carries no reason either.
//
// So the answer to "why was this person's file removed" is nothing, in the one
// module whose whole promise is a removal somebody can be asked about. It is the
// asymmetry a hold already gets right: file_holds.reason exists, is validated,
// and is quoted back by the refusal that cites it, on the module's own argument
// that a hold is a decision "somebody made and said a reason for". A reason typed
// into an erasure and thrown away is worse than no reason field at all, because
// the caller believes it was filed.
//
// The assertion is over everything the module wrote about this erasure — the
// events it published in the same transaction as the removal, and the proof row
// that outlives the row it proofs — because either is a defensible place to keep
// it. What is not defensible is neither.
func TestAnErasureRecordsTheReasonItWasAskedFor(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	dir := t.TempDir()
	store := internal.NewLocal(dir)
	svc := internal.NewService(store, filetest.Limit, 0, 0)

	const reason = "data protection request 2026-0412"
	subject, officer := uuid.New(), uuid.New()

	upload := func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.Upload(ctx, held(tx), contracts.Upload{
			Name: "diary.txt", ContentType: "text/plain", Declared: -1,
			Body: strings.NewReader("something this person wrote"),
		})
		return err
	}
	// The file belongs to the subject: Validate stamps UploaderID from the actor
	// on the context, which is the one thing that makes a row a subject's.
	err := db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), subject), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error { return upload(ctx, tx) })
	if err != nil {
		t.Fatalf("seed the subject's file: %v", err)
	}

	var receipt *contracts.ErasureReceipt
	err = db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), officer), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			receipt, err = svc.EraseSubject(ctx, tx, subject, reason)
			return err
		})
	if err != nil {
		t.Fatalf("erase the subject: %v", err)
	}
	if receipt.Files != 1 {
		t.Fatalf("the erasure removed %d files, want the one this subject uploaded", receipt.Files)
	}

	// The bytes go through the subscription, as they always do, and it writes the
	// proof the command promised.
	if err := deliverEach(t.Context(), conn, acme, internal.EraseBlobs(store)); err != nil {
		t.Fatalf("deliver the removal: %v", err)
	}
	var proofs int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM file_erasures WHERE subject_id = $1 AND cause = $2`,
		subject, contracts.EraseSubject).Scan(&proofs); err != nil {
		t.Fatalf("read the proof back: %v", err)
	}
	if proofs != 1 {
		t.Fatalf("the erasure left %d proof rows, want one", proofs)
	}

	// Everything this module wrote about this removal, in one string.
	var trail, rows string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT coalesce(string_agg(payload::text, E'\n'), '') FROM `+outbox+` WHERE name IN ($1, $2)`,
		contracts.EventDeleted, contracts.EventErased).Scan(&trail); err != nil {
		t.Fatalf("read what the erasure published: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(),
		`SELECT coalesce(string_agg(to_jsonb(e)::text, E'\n'), '') FROM file_erasures e WHERE subject_id = $1`,
		subject).Scan(&rows); err != nil {
		t.Fatalf("read the proof table: %v", err)
	}
	if !strings.Contains(trail+rows, reason) {
		t.Errorf("nothing this module wrote about the erasure of %s carries the reason it was asked for (%q); the published events are %s and the proof rows %s",
			subject, reason, trail, rows)
	}
	// The reachability of that assertion does not rest on the missing reason: the
	// command did remove the file, and the trail it left is the module's own
	// events, read back through the transport contract.
	var still int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM files WHERE uploader_id = $1 AND deleted_at IS NULL`, subject).Scan(&still); err != nil {
		t.Fatalf("count what is left: %v", err)
	}
	if still != 0 {
		t.Errorf("%d of this subject's files survive the erasure", still)
	}
}
