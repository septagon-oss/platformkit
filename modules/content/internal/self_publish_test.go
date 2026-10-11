package internal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/content"
	"github.com/septagon-oss/platformkit/modules/content/contracts"
	"github.com/septagon-oss/platformkit/modules/content/internal"
)

func TestAuthorCannotPublishTheirOwnContent(t *testing.T) {
	admin, conn := dbtest.Schema(t, content.Migrations)
	author := uuid.New()
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), author)
	page := &contracts.Content{Slug: "authored-page", Title: "Authored page", Body: "## Introduction"}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, page)
	}); err != nil {
		t.Fatal(err)
	}
	if page.AuthorID != author {
		t.Fatalf("author setup failed: got %s, want %s", page.AuthorID, author)
	}

	var returned *contracts.Content
	var refusal error
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		returned, refusal = internal.NewService().Publish(ctx, tx, page.ID)
		return nil // Commit even on refusal so an accidental mutation remains visible.
	}); err != nil {
		t.Fatal(err)
	}
	if refusal == nil || !strings.Contains(strings.ToLower(refusal.Error()), "author") {
		t.Errorf("self-publication needs a reason naming authorship, got %v", refusal)
	}
	if returned != nil {
		t.Errorf("refused publication returned a row: %+v", returned)
	}
	var status string
	if err := admin.QueryRowContext(t.Context(), "SELECT status FROM contents WHERE id = $1", page.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != contracts.StatusDraft {
		t.Errorf("self-publication committed status %q", status)
	}
	var events int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT count(*) FROM platformkit_outbox WHERE name = $1 AND tenant_id = $2", contracts.EventPublished, acme.ID).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Errorf("self-publication committed %d events", events)
	}
}
