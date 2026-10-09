package internal_test

import (
	"context"
	"errors"
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

func TestAnotherTenantCannotReadOrPublishContent(t *testing.T) {
	admin, conn := dbtest.Schema(t, content.Migrations)
	page := &contracts.Content{Slug: "private-draft", Title: "Private draft"}
	if err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return crud.Create(ctx, tx, page)
	}); err != nil {
		t.Fatal(err)
	}
	other := tenancy.Tenant{ID: uuid.New(), Slug: "other"}
	if err := db.Run(tenancy.WithTenant(t.Context(), other), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if row, err := crud.Get[*contracts.Content](tx, page.ID); !errors.Is(err, crud.ErrNotFound) || row != nil {
			t.Errorf("foreign read = %+v, %v", row, err)
		}
		if row, err := internal.NewService().Publish(ctx, tx, page.ID); !errors.Is(err, crud.ErrNotFound) || row != nil {
			t.Errorf("foreign publish = %+v, %v", row, err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var status string
	if err := admin.QueryRowContext(t.Context(), "SELECT status FROM contents WHERE id = $1", page.ID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != contracts.StatusDraft {
		t.Errorf("foreign command committed status %q", status)
	}
	var events int
	if err := admin.QueryRowContext(t.Context(), "SELECT count(*) FROM platformkit_outbox WHERE name = $1", contracts.EventPublished).Scan(&events); err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Errorf("foreign command committed %d events", events)
	}
}
