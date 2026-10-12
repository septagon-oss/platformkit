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

// TestTheTranslatorOfARowIsTheActorWhoWroteIt: translator_id is "the principal
// of the last write", and translation.updated names the same person.
func TestTheTranslatorOfARowIsTheActorWhoWroteIt(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	svc := internal.NewService(nil, nil)
	id, actor := uuid.New(), uuid.New()
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), actor)

	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.Save(ctx, tx, rest.SaveQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
			Values: map[string]string{"title": "Sobre nós."}, Expected: map[string]int64{"title": 0},
			Source: map[string]string{"title": "About us."},
		}); err != nil {
			t.Fatalf("saving: %v", err)
		}
		var stored []uuid.UUID
		if err := tx.DB().Raw(`SELECT translator_id FROM translations WHERE record_id = ?`, id).Scan(&stored).Error; err != nil {
			t.Fatalf("reading translator_id: %v", err)
		}
		if len(stored) != 1 || stored[0] != actor {
			t.Errorf("translator_id is %v, want the writing actor %v", stored, actor)
		}
		for _, ev := range publishedPayloads(ctx, tx) {
			if ev.Translator != actor {
				t.Errorf("translation.updated names translator %v, want %v", ev.Translator, actor)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
