package internal_test

import (
	"context"
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

// TestErasureRemovesThePersonsNameAndAddressFromAltText captions a picture with the
// person's name and address through the caption command, erases the person through
// the file module's contributor, and rereads the row in a fresh transaction: the row
// and its file survive, and neither the name nor the address is left in its alt text.
func TestErasureRemovesThePersonsNameAndAddressFromAltText(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	person := personaldata.Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones"}
	ctx := tenancy.WithTenant(t.Context(), acme)
	var row *contracts.File
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		row, err = svc.Upload(ctx, held(tx), contracts.Upload{Name: "offsite.png", ContentType: "text/plain", Declared: -1, Body: strings.NewReader("pixels")})
		if err != nil {
			return err
		}
		row, err = svc.SetAlt(ctx, tx, row.ID, "Jo Jones <jo.jones@example.com> at the offsite", false, row.UpdatedAt)
		return err
	}); err != nil {
		t.Fatalf("caption a picture with the person's name: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, person)
	}); err != nil {
		t.Fatalf("erasure failed: %v", err)
	}
	var kept *contracts.File
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		kept, err = crud.Get[*contracts.File](tx, row.ID)
		return err
	}); err != nil {
		t.Fatalf("the captioned file did not survive the erasure: %v", err)
	}
	for _, id := range []string{person.Email, person.DisplayName} {
		if strings.Contains(strings.ToLower(kept.Alt), strings.ToLower(id)) {
			t.Errorf("committed erasure left %q in alt text %q", id, kept.Alt)
		}
	}
}
