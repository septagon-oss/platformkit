package internal_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/contracts/filetest"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestAReasonOfFiveHundredCharactersIsFiveHundredInAnyAlphabet holds the two
// reason doors to the count their own message, their own column and the route's
// own declared maxLength use: characters.
//
// The package already knows this. File.Validate refuses a name over MaxName with
// utf8.RuneCountInString, on the argument that a 90-character emoji is not 360
// characters, and modules/content, modules/site and modules/billing made the same
// correction. The two retention reasons were the last two `len()`s in it, so a
// data-protection sentence of 500 characters spelled with two-byte letters — the
// ordinary case for every tenant that does not write English — was a 422, and the
// erasure it belonged to was refused. A guard that measures bytes while its error
// message counts characters refuses work it has no ground to refuse, and the
// schema-side maxLength huma enforces is a character count, so the two guards
// disagreed about the same request.
func TestAReasonOfFiveHundredCharactersIsFiveHundredInAnyAlphabet(t *testing.T) {
	admin, conn := dbtest.Schema(t, file.Migrations)
	store := internal.NewLocal(t.TempDir())
	svc := internal.NewService(store, filetest.Limit, 0)

	// A hold's reason, at the ceiling and one over, spelled in characters a
	// two-byte letter counts once each.
	fileID := uuid.New()
	at := contracts.Hold{FileID: fileID, Reason: strings.Repeat("é", contracts.MaxHoldReason)}
	if err := at.Validate(t.Context()); err != nil {
		t.Errorf("a hold reason of %d two-byte characters = %d bytes was refused: %v; the door says %d characters",
			contracts.MaxHoldReason, len(at.Reason), err, contracts.MaxHoldReason)
	}
	over := contracts.Hold{FileID: fileID, Reason: strings.Repeat("é", contracts.MaxHoldReason+1)}
	if err := over.Validate(t.Context()); err == nil {
		t.Error("a hold reason one character over the ceiling was accepted")
	}

	subject := uuid.New()
	seed := func(owner uuid.UUID, name string) {
		err := db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), owner), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, err := svc.Upload(ctx, held(tx), contracts.Upload{
					Name: name, ContentType: "text/plain", Declared: -1,
					Body: strings.NewReader("something this person wrote"),
				})
				return err
			})
		if err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	seed(subject, "diary.txt")

	// The erasure's own door, at the ceiling.
	sentence := strings.Repeat("é", contracts.MaxErasureReason)
	if len(sentence) <= contracts.MaxErasureReason {
		t.Fatalf("the fixture sentence is %d bytes for %d characters; it tests nothing",
			len(sentence), utf8.RuneCountInString(sentence))
	}
	var receipt *contracts.ErasureReceipt
	err := db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), uuid.New()), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var err error
			receipt, err = svc.EraseSubject(ctx, tx, subject, sentence)
			return err
		})
	if err != nil {
		t.Fatalf("an erasure whose reason is %d characters was refused: %v",
			utf8.RuneCountInString(sentence), err)
	}
	// The sentence is filed as it was sent, not truncated to a byte count.
	var filed string
	if err := admin.QueryRowContext(t.Context(),
		`SELECT payload->>'reason' FROM `+outbox+` WHERE name = $1`, contracts.EventDeleted).Scan(&filed); err != nil {
		t.Fatalf("read the work order's reason: %v", err)
	}
	if utf8.RuneCountInString(filed) != contracts.MaxErasureReason {
		t.Errorf("the work order filed %d characters of a %d-character reason",
			utf8.RuneCountInString(filed), contracts.MaxErasureReason)
	}
	if receipt == nil || receipt.Files != 1 {
		t.Fatalf("the erasure receipt is %+v, want the subject's one file", receipt)
	}

	// One character over is still refused, and refused before anything is removed:
	// the second subject's file is where it was, with no work order beside it.
	second := uuid.New()
	seed(second, "second.txt")
	if err := db.Run(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), second), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.EraseSubject(ctx, tx, second, strings.Repeat("é", contracts.MaxErasureReason+1))
			return err
		}); err == nil {
		t.Error("an erasure reason one character over the ceiling was accepted")
	}
	var left int
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*) FROM files WHERE uploader_id = $1 AND deleted_at IS NULL`, second).Scan(&left); err != nil {
		t.Fatalf("count the refused erasure's files: %v", err)
	}
	if left != 1 {
		t.Errorf("the refused erasure left %d of the second subject's files: a refusal writes nothing", left)
	}
}
