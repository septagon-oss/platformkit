package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

// TestErasureRefusesTheCaptionFormServedBeforeIt captions a picture, erases the person the
// caption names, and then replays the caption through the same command with the instant the
// form was answered with: the save is refused as a conflict, it writes nothing, and the
// person's words stay out of the row.
//
// This is the half of the alt-text erasure the caption guard makes necessary. SetAlt refuses
// a caption written against a file the caller was not shown, and a form served before the
// erasure holds an instant that only the erasure itself can invalidate: a rewrite that left
// files.updated_at where it was would answer the stale form with a match, and the next save
// would write the person's sentence back over the top of their erasure while the row went on
// reporting that nobody had touched it since.
func TestErasureRefusesTheCaptionFormServedBeforeIt(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	person := personaldata.Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones"}
	ctx := tenancy.WithTenant(t.Context(), acme)
	var row *contracts.File
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		row, err = svc.Upload(ctx, held(tx), contracts.Upload{
			Name: "offsite.png", ContentType: "text/plain", Declared: -1, Body: strings.NewReader("pixels"),
		})
		if err != nil {
			return err
		}
		row, err = svc.SetAlt(ctx, tx, row.ID, "Jo Jones at the offsite", false, row.UpdatedAt)
		return err
	}); err != nil {
		t.Fatalf("caption a picture with the person's name: %v", err)
	}
	// The instant a caption form served now would travel back with its save.
	served := row.UpdatedAt
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, person)
	}); err != nil {
		t.Fatalf("erasure failed: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		returned, err := svc.SetAlt(ctx, tx, row.ID, "Jo Jones <jo.jones@example.com> at the offsite", false, served)
		if returned != nil || !errors.Is(err, crud.ErrConflict) {
			t.Errorf("the caption served before the erasure returned row=%v error=%v; want a conflict", returned != nil, err)
		}
		return nil
	}); err != nil {
		t.Fatalf("a refused caption must still let the transaction finish: %v", err)
	}
	var kept *contracts.File
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		kept, err = crud.Get[*contracts.File](tx, row.ID)
		return err
	}); err != nil {
		t.Fatalf("the captioned file did not survive: %v", err)
	}
	for _, id := range []string{person.Email, person.DisplayName} {
		if strings.Contains(strings.ToLower(kept.Alt), strings.ToLower(id)) {
			t.Errorf("the refused caption left %q in alt text %q", id, kept.Alt)
		}
	}
}
