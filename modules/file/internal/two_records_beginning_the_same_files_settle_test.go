package internal_test

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestTwoRecordsBeginningUsesOfTheSameFilesSettle saves two records that both
// start showing the same two pictures, each naming them in the order its own
// document happens to hold them — which for these two is the opposite order —
// from two transactions that are in flight at the same instant.
//
// It checks that the files are locked in one order that the document does not
// decide. A rewrite that locked in the order it was handed leaves the two
// transactions holding one picture and waiting for the other, which Postgres
// refuses with 40P01 and one person's save is lost to a collision they cannot
// see or fix; a rewrite that locks in its own ascending order makes the second
// wait for the first, which is a slow save and not a failed one. Both records
// are saved public, so a success is also asserted where the audience is written:
// both pictures end publicly readable, because both records that show them are.
func TestTwoRecordsBeginningUsesOfTheSameFilesSettle(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 60*time.Second)
	defer cancel()

	var picture bytes.Buffer
	if err := jpeg.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 20, 10)), nil); err != nil {
		t.Fatal(err)
	}
	var ids [2]uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for i, name := range [2]string{"first.jpg", "second.jpg"} {
			row, err := svc.Upload(ctx, held(tx), contracts.Upload{
				Name: name, ContentType: "image/jpeg", Image: true,
				Body: bytes.NewReader(picture.Bytes()), Declared: int64(picture.Len()),
			})
			if err != nil {
				return err
			}
			ids[i] = row.ID
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// A fresh pair of records per round: a rewrite that files nothing new takes
	// no lock on a file at all, so reusing a pair would settle rounds two to six
	// without asking the question the first one asked.
	const rounds = 6
	saved := make(chan error, rounds*2)
	for range rounds {
		first, second := uuid.New(), uuid.New()
		start := make(chan struct{})
		var both sync.WaitGroup
		for i, use := range [2]contracts.Use{useOf(first), useOf(second)} {
			refs := []uuid.UUID{ids[0], ids[1]}
			if i == 1 {
				refs = []uuid.UUID{ids[1], ids[0]}
			}
			both.Add(1)
			go func() {
				defer both.Done()
				<-start
				saved <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
					_, err := svc.SetUsesShown(ctx, tx, use, refs, true)
					return err
				})
			}()
		}
		close(start)
		both.Wait()
	}
	close(saved)
	for err := range saved {
		if err != nil {
			t.Fatalf("a record beginning two uses at once: %v", err)
		}
	}

	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, id := range ids {
			// The door answers, not the column. A use a record began asserts its
			// audience in its own row: a command that only names a file never writes
			// the file row, because that write is a lock held to the end of the save
			// that took it, and two records naming two shared pictures in each
			// other's order then each wait for a row the other holds. Who may read the
			// file is the visibility it was given plus the records showing it, which is
			// what this anonymous read resolves — and the row it hands back carries the
			// answer, so what is served and what the file is reported to be stay one
			// answer.
			row, body, err := svc.Open(ctx, tx, id, true)
			if err != nil {
				t.Errorf("an anonymous read of %s after two public records began showing it: %v", id, err)
				continue
			}
			if err := body.Close(); err != nil {
				return err
			}
			if !row.Public() {
				t.Errorf("file %s is not public after two public records began showing it", id)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
