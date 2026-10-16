package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

func TestRemovingATranslationChecksTheRevisionBeforeDeletingAnyField(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	svc := internal.NewService(nil, nil)
	id := uuid.New()
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Save(ctx, tx, rest.SaveQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
			Values: map[string]string{"title": "Título", "body": "Texto"},
			Source: map[string]string{"title": "Title", "body": "Text"},
		})
	}); err != nil {
		t.Fatal(err)
	}
	// Commit after observing the refusal, so rollback cannot hide a partial delete.
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		err := svc.Untranslate(ctx, tx, rest.ReviewQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
			Expected: map[string]int64{"title": 99, "body": 1},
		})
		if !errors.Is(err, crud.ErrConflict) {
			t.Errorf("stale removal = %v, want crud.ErrConflict", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, events := storedRows(ctx, tx), publishedPayloads(ctx, tx)
		if len(rows) != 2 || len(events) != 2 {
			t.Errorf("after stale removal: %d rows, %d events; want 2 unchanged rows and the 2 original events", len(rows), len(events))
		}
		for _, row := range rows {
			if row.Revision != 1 {
				t.Errorf("%s revision = %d, want 1", row.Field, row.Revision)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
