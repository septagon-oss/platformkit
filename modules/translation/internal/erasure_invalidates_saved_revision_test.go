package internal_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

func TestErasureInvalidatesTheRevisionAnOpenTranslationFormHolds(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	svc := internal.NewService(nil, nil)
	person := personaldata.Subject{ID: uuid.New(), Email: "jo.jones@example.com", DisplayName: "Jo Jones"}
	// A colleague's form stays authorized when the named person's credentials are erased.
	ctx := tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), uuid.New())
	query := rest.SaveQuery{
		Module: "pages", Entity: "page", RecordID: uuid.New(), Locale: "pt-PT",
		Source: map[string]string{"body": "Contact " + person.DisplayName + " at " + person.Email},
		Values: map[string]string{"body": "Contacte " + person.DisplayName + " em " + person.Email},
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Save(ctx, tx, query)
	}); err != nil {
		t.Fatal(err)
	}
	var before internal.Row
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Where("record_id = ?", query.RecordID).First(&before).Error
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return internal.PersonalData().Data.Erase(ctx, tx, person)
	}); err != nil {
		t.Fatal(err)
	}
	var erased internal.Row
	var eventsBefore int64
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Where("id = ?", before.ID).First(&erased).Error; err != nil {
			return err
		}
		return tx.DB().Table(outbox).Count(&eventsBefore).Error
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(erased.Value, person.Email) || strings.Contains(erased.SourceText, person.Email) {
		t.Fatal("initial erasure did not remove the address")
	}
	query.Expected = map[string]int64{"body": before.Revision}
	query.Values["body"] += "; texto editado"
	// The write route refreshes the source; the submitted translation is still the old form.
	query.Source["body"] = erased.SourceText
	// Commit even after a refusal, so a rollback cannot hide an early write or event.
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := svc.Save(ctx, tx, query); !errors.Is(err, crud.ErrConflict) {
			t.Errorf("save with the pre-erasure revision = %v, want crud.ErrConflict", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	var after internal.Row
	var eventsAfter int64
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Where("id = ?", before.ID).First(&after).Error; err != nil {
			return err
		}
		return tx.DB().Table(outbox).Count(&eventsAfter).Error
	}); err != nil {
		t.Fatal(err)
	}
	if after.Value != erased.Value || after.SourceText != erased.SourceText || after.Revision != erased.Revision {
		t.Errorf("stale form changed the erased translation: value=%q source=%q revision=%d", after.Value, after.SourceText, after.Revision)
	}
	if eventsAfter != eventsBefore {
		t.Errorf("stale form emitted %d new events", eventsAfter-eventsBefore)
	}
}
