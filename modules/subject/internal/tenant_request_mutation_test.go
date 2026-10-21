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
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/subject"
	"github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestAnotherTenantCannotReadOrChangeAPersonalDataRequest(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	person := personaldata.Subject{ID: uuid.New(), Email: "tenant.only@example.com", DisplayName: "Tenant Person"}
	seed(t, conn, person)
	svc := newDoor(&stamps{at: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}, &bundlesStub{}, &notesStub{})
	id := ask(t, svc, conn, person)
	beta := tenancy.Tenant{ID: uuid.New(), Slug: "beta", Name: "Beta"}
	ctx := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), beta), person.ID),
		tenancy.Principal{UserID: person.ID, Permissions: []string{contracts.PermissionExport, contracts.PermissionErase}})
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, err := svc.Request(ctx, tx, id, person.ID, true)
		if !errors.Is(err, crud.ErrNotFound) || row != nil {
			t.Errorf("cross-tenant read = %v, row=%v, want not found and no row", err, row)
		}
		res := tx.DB().Model(&contracts.Request{}).Where("id = ?", id).
			UpdateColumns(map[string]any{"state": contracts.StateErased, "revision": 2})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected != 0 {
			t.Errorf("cross-tenant mutation changed %d rows", res.RowsAffected)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		row, receipt, err := svc.RequestErase(ctx, tx, contracts.EraseInput{Subject: person.ID, Confirm: person.Email})
		if row != nil || receipt != nil {
			t.Errorf("cross-tenant erasure returned a row or receipt: %v %v", row, receipt)
		}
		return err
	})
	if !errors.Is(err, crud.ErrNotFound) {
		t.Errorf("cross-tenant erasure = %v, want not found", err)
	}
	if row := readRequest(t, conn, id); row.State != contracts.StateRequested || row.Revision != 1 {
		t.Errorf("other tenant changed the original row: %+v", row)
	}
	if n := published(t, conn, contracts.EventErased); n != 0 {
		t.Errorf("refused cross-tenant erasure emitted %d events", n)
	}
	if err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var requests int64
		if err := tx.DB().Model(&contracts.Request{}).Count(&requests).Error; err != nil {
			return err
		}
		if requests != 0 {
			t.Errorf("refused cross-tenant erasure committed %d requests", requests)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
