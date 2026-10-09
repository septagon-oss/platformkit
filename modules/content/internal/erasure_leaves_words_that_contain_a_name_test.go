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

// TestErasingAPersonLeavesOtherPeoplesWordsAlone erases a person called Ann whose
// handle is "ann" from a tenant where a colleague wrote a post that mentions her once
// and also uses the words "Annual" and "planning". The mention goes; the colleague's
// words that merely contain the three letters are not the person and stay as written.
func TestErasingAPersonLeavesOtherPeoplesWordsAlone(t *testing.T) {
	_, conn := dbtest.Schema(t, content.Migrations)
	in := tenancy.WithTenant(t.Context(), acme)
	subject := personaldata.Subject{ID: uuid.New(), Email: "ann@example.com", DisplayName: "Ann", Handle: "ann"}
	post := uuid.New()

	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec(`INSERT INTO contents (id, tenant_id, slug, title, body, author_id) VALUES (?, ?, ?, ?, ?, ?)`,
			post, acme.ID, "annual-planning", "Annual planning", "Thanks to Ann for the planning notes.", uuid.New()).Error
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := db.Run(in, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, subject)
	}); err != nil {
		t.Fatalf("erase: %v", err)
	}

	var title, body string
	if err := db.Run(in, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT title, body FROM contents WHERE id = ?`, post).Row().Scan(&title, &body)
	}); err != nil {
		t.Fatalf("re-read: %v", err)
	}
	if title != "Annual planning" {
		t.Errorf("the colleague's title was rewritten: %q", title)
	}
	want := "Thanks to " + subject.Token() + " for the planning notes."
	if body != want {
		t.Errorf("body = %q, want %q", body, want)
	}
}
