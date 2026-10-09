package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/content/internal"
)

func TestErasingRenePreservesReneesName(t *testing.T) {
	_, conn := dbtest.Schema(t, content.Migrations)
	ctx := tenancy.WithTenant(t.Context(), acme)
	person := personaldata.Subject{ID: uuid.New(), Email: "rene@example.com", DisplayName: "René", Handle: "rene"}
	post := uuid.New()
	const title = "Renée's notes"
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`INSERT INTO contents (id, tenant_id, slug, title, body, author_id) VALUES (?, ?, ?, ?, ?, ?)`,
			post, acme.ID, "handover-notes", title, "Renée thanks René for the notes.", uuid.New()).Error
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, person)
	}); err != nil {
		t.Fatalf("erase: %v", err)
	}
	var gotTitle, gotBody string
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT title, body FROM contents WHERE id = ?`, post).Row().Scan(&gotTitle, &gotBody)
	}); err != nil {
		t.Fatalf("read surviving row: %v", err)
	}
	if gotTitle != title {
		t.Errorf("another person's title changed: got %q, want %q", gotTitle, title)
	}
	if want := "Renée thanks " + person.Token() + " for the notes."; gotBody != want {
		t.Errorf("body = %q, want %q", gotBody, want)
	}
}
