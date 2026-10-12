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

// TestForgettingARecordAnnouncesEveryRowRemoved: the event a deletion emits
// says the row was removed, as Untranslate's does, so a subscriber (the audit
// trail, a search index) can tell a deletion from an update.
func TestForgettingARecordAnnouncesEveryRowRemoved(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	svc := internal.NewService(nil, nil)
	id := uuid.New()

	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.Save(ctx, tx, rest.SaveQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
			Values: map[string]string{"title": "Sobre nós."}, Expected: map[string]int64{"title": 0},
			Source: map[string]string{"title": "About us."},
		}); err != nil {
			t.Fatalf("saving: %v", err)
		}
		before := len(publishedPayloads(ctx, tx))
		if err := svc.ForgetRecord(ctx, tx, "pages", "page", id); err != nil {
			t.Fatalf("forgetting the record: %v", err)
		}
		after := publishedPayloads(ctx, tx)[before:]
		if len(after) != 1 {
			t.Fatalf("forgetting one translated field published %d events, want 1", len(after))
		}
		if after[0].Status != rest.FallbackRemoved {
			t.Errorf("the deletion's event says status %q, want %q", after[0].Status, rest.FallbackRemoved)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
