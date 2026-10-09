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
	"github.com/septagon-oss/platformkit/modules/translation/contracts/translationtest"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

func TestMachineRichTextIsValidatedBeforeItCanBeReviewed(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	source := translationtest.NewStubSource()
	machine := &translationtest.Machine{Answer: func(context.Context, string, string, string) (string, error) {
		return `<script>alert("traduzido")</script>`, nil
	}}
	svc := internal.NewService([]rest.TranslationSource{source}, machine)
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		err := svc.Suggest(ctx, tx, rest.SuggestQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", From: "en", RecordID: uuid.New(),
			Fields: []string{"body"}, Source: map[string]string{"body": "Our story."},
			RichText: map[string]bool{"body": true},
		})
		if !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("machine richtext refusal = %v, want crud.ErrInvalid", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if rows, events := storedRows(ctx, tx), publishedPayloads(ctx, tx); len(rows) != 0 || len(events) != 0 {
			t.Errorf("invalid machine richtext committed %d rows and %d events; want none", len(rows), len(events))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
