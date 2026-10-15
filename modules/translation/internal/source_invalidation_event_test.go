package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

func TestSourceInvalidationPublishesTheChangedTranslation(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	svc := internal.NewService(nil, nil)
	id := uuid.New()
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Save(ctx, tx, rest.SaveQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
			Values: map[string]string{"title": "Sobre nós."}, Source: map[string]string{"title": "About us."},
		})
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.MarkOutdated(ctx, tx, "pages", "page", "title", id)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, events := storedRows(ctx, tx), publishedPayloads(ctx, tx)
		if len(rows) != 1 || rows[0].Status != rest.StateOutdated {
			t.Fatalf("invalidation did not reach the row: %+v", rows)
		}
		if len(events) != 2 {
			t.Errorf("invalidation left %d events, want the original save and the outdated update", len(events))
		} else if events[1].Status != rest.StateOutdated || events[1].Revision != rows[0].Revision {
			t.Errorf("event does not describe the committed invalidation: %+v", events[1])
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
