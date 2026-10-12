package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/rest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/translation"
	"github.com/septagon-oss/platformkit/modules/translation/internal"
)

// TestTwoFirstTranslationsOfOneFieldHaveOneLoser: two translators both start
// from "no row yet" (expected revision 0). One wins; the other is a conflict
// that commits nothing and publishes nothing.
func TestTwoFirstTranslationsOfOneFieldHaveOneLoser(t *testing.T) {
	_, conn := dbtest.Schema(t, translation.Migrations)
	svc := internal.NewService(nil, nil)
	id := uuid.New()
	save := func(ctx context.Context, tx db.Tx[db.Tenant], value string) error {
		return svc.Save(ctx, tx, rest.SaveQuery{
			Module: "pages", Entity: "page", Locale: "pt-PT", RecordID: id,
			Values: map[string]string{"title": value}, Expected: map[string]int64{"title": 0},
			Source: map[string]string{"title": "About us."},
		})
	}
	ctx := tenancy.WithTenant(t.Context(), acme)
	second := make(chan error, 1)
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := save(ctx, tx, "Sobre nós."); err != nil {
			return err
		}
		go func() {
			second <- db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return save(ctx, tx, "Acerca de nós.")
			})
		}()
		time.Sleep(300 * time.Millisecond) // let the second block on the identity index
		return nil
	})
	if err != nil {
		t.Fatalf("the first translator: %v", err)
	}
	if err := <-second; !errors.Is(err, crud.ErrConflict) {
		t.Errorf("the second translator answered %v, want crud.ErrConflict", err)
	}
	var values []string
	var events int
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if err := tx.DB().Raw(`SELECT value FROM translations WHERE record_id = ?`, id).Scan(&values).Error; err != nil {
			return err
		}
		return tx.DB().Raw(`SELECT count(*) FROM `+outbox+` WHERE payload->>'recordId' = ?`, id.String()).Scan(&events).Error
	}); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if len(values) != 1 || values[0] != "Sobre nós." || events != 1 {
		t.Errorf("after the race: values %v, %d events; want the winner's one row and one event", values, events)
	}
}
