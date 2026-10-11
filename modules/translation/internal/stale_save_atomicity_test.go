package internal_test

import (
	"context"
	"errors"
	"slices"
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

func TestStaleTranslationSaveCommitsNoPartialFieldsOrEvents(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	svc := internal.NewService(nil, nil)
	ctx := tenancy.WithTenant(t.Context(), acme)
	id := uuid.New()
	query := rest.SaveQuery{
		Module: "pages", Entity: "page", RecordID: id, Locale: "pt-PT",
		Source: map[string]string{"body": "Our story.", "title": "About us."},
		Values: map[string]string{"body": "A nossa história.", "title": "Sobre nós."},
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Save(ctx, tx, query)
	}); err != nil {
		t.Fatal(err)
	}
	query.Values = map[string]string{"body": "Outro parágrafo.", "title": "Outra história."}
	// The body sorts first and has a current revision; only the last field is
	// stale. Commit the surrounding transaction after observing the refusal so
	// a rollback cannot hide an early write or an early outbox publication.
	query.Expected = map[string]int64{"body": 1, "title": 99}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.Save(ctx, tx, query); !errors.Is(err, crud.ErrConflict) {
			t.Errorf("stale save = %v; want a conflict", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	type saved struct {
		Field, Value string
		Revision     int64
	}
	var rows []saved
	var emitted int
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw("SELECT field, value, revision FROM translations WHERE record_id = ? ORDER BY field", id).Scan(&rows).Error; err != nil {
			return err
		}
		return tx.DB().Raw("SELECT count(*) FROM "+outbox+" WHERE payload->>'recordId' = ?", id.String()).Scan(&emitted).Error
	}); err != nil {
		t.Fatal(err)
	}
	want := []saved{{"body", "A nossa história.", 1}, {"title", "Sobre nós.", 1}}
	if !slices.Equal(rows, want) || emitted != 2 {
		t.Fatalf("after committing the refused save: rows=%v events=%d; want original rows=%v and only 2 initial events", rows, emitted, want)
	}
}
