package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestAPublicUploadKeepsItsDoorWhenTheRecordThatShowedItDropsIt is the floor under
// who may read a file. What an upload was given — the choice of whoever holds
// file:manage — is the one audience fact about a file that is not derived, so the
// ledger widens it and never narrows it: a site logo uploaded public, shown by a
// published page, then dropped by that page, is still served to a stranger, and its
// row says public throughout because nothing rewrote it. The audience a publish
// asserted is the other half, and that one does go back in the write that ends the
// use that asserted it: the same two writes on a private upload leave it anonymously
// unreadable. Both halves publish their flip and nothing else — an audience that
// never moved records nothing.
func TestAPublicUploadKeepsItsDoorWhenTheRecordThatShowedItDropsIt(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	ctx, cancel := context.WithTimeout(tenancy.WithTenant(t.Context(), acme), 20*time.Second)
	defer cancel()

	given := uploaded("logo.png")
	given.Visibility = contracts.VisibilityPublic
	var hand, raised uuid.UUID
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, up := range []contracts.Upload{given, uploaded("page-picture.png")} {
			row, err := svc.Upload(ctx, held(tx), up)
			if err != nil {
				return err
			}
			if up.Visibility == contracts.VisibilityPublic {
				hand = row.ID
			} else {
				raised = row.ID
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	// Each of the two is shown by a published record, which files a use row that
	// asserts the public audience, and then dropped by that same record.
	shown := useOf(uuid.New())
	for _, id := range []uuid.UUID{hand, raised} {
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.SetUsesShown(ctx, tx, shown, []uuid.UUID{id}, true)
			return err
		}); err != nil {
			t.Fatalf("a published record showing %s: %v", id, err)
		}
	}
	for _, id := range []uuid.UUID{hand, raised} {
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.SetUses(ctx, tx, shown, nil)
			return err
		}); err != nil {
			t.Fatalf("the record dropping %s: %v", id, err)
		}
	}

	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, want := range []struct {
			id        uuid.UUID
			anonymous bool
			audience  string
			flips     int
		}{
			{hand, true, contracts.VisibilityPublic, 0},
			{raised, false, contracts.VisibilityPrivate, 2},
		} {
			_, body, err := svc.Open(ctx, tx, want.id, true)
			switch {
			case want.anonymous && err != nil:
				t.Errorf("%s after the page that showed it dropped it, anonymously = %v, want served",
					want.id, err)
			case !want.anonymous && !errors.Is(err, crud.ErrNotFound):
				t.Errorf("%s after the page that showed it dropped it, anonymously = %v, want %v",
					want.id, err, crud.ErrNotFound)
			}
			if body != nil {
				body.Close()
			}
			row, err := crud.Get[*contracts.File](tx, want.id)
			if err != nil {
				return err
			}
			if row.Visibility != want.audience {
				t.Errorf("%s is %q, want %q: the row holds what its upload was given, and no rewrite of a record's uses moves it",
					want.id, row.Visibility, want.audience)
			}
			var flips int64
			if err := tx.DB().Table(outbox).Where("name = ? AND payload->>'fileId' = ?",
				contracts.EventVisibility, want.id.String()).Count(&flips).Error; err != nil {
				return err
			}
			if int(flips) != want.flips {
				t.Errorf("%s published %d audience flips, want %d", want.id, flips, want.flips)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
