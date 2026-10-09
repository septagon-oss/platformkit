package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/entity"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation"
	"github.com/septagon-oss/platformkit/modules/translation/contracts/translationtest"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

// shortTitled is a page whose title the schema caps at twelve characters.
type shortTitled struct {
	entity.Base
	Title string `json:"title" maxLength:"12" i18n:"translatable"`
}

func (shortTitled) TableName() string { return "short_titled_pages" }

func TestAMachineDraftLongerThanItsFieldIsRefusedAndNothingIsWritten(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	source := rest.TranslationSourceOf(rest.Spec[*shortTitled]{Module: "pages", Entity: "page"})
	machine := &translationtest.Machine{Answer: func(context.Context, string, string, string) (string, error) {
		return "Este título é demasiado longo", nil
	}}
	svc := internal.NewService([]rest.TranslationSource{source}, machine)
	ctx := tenancy.WithTenant(t.Context(), acme)
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		err := svc.Suggest(ctx, tx, rest.SuggestQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", From: "en", RecordID: uuid.New(),
			Fields: []string{"title"}, Source: map[string]string{"title": "About us."},
		})
		if !errors.Is(err, crud.ErrInvalid) {
			t.Errorf("machine draft over the field limit = %v, want crud.ErrInvalid", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if rows, events := storedRows(ctx, tx), publishedPayloads(ctx, tx); len(rows) != 0 || len(events) != 0 {
			t.Errorf("a machine draft over the field limit committed %d rows and %d events; want none", len(rows), len(events))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
