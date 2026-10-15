package internal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/file"
	"github.com/septagon-oss/platformkit/modules/file/contracts"
	"github.com/septagon-oss/platformkit/modules/file/internal"
)

func TestCaptionCommandRefusesAReadOnlyActor(t *testing.T) {
	_, conn := dbtest.Schema(t, file.Migrations)
	svc := internal.NewService(internal.NewLocal(t.TempDir()), 1<<20, 0, 0)
	owner := tenancy.WithTenant(t.Context(), acme)
	var row *contracts.File
	if err := db.Run(owner, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		row, err = svc.Upload(ctx, held(tx), contracts.Upload{Name: "private.txt", ContentType: "text/plain", Declared: -1, Body: strings.NewReader("private")})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	reader := tenancy.WithPrincipal(owner, tenancy.Principal{UserID: uuid.New(), Permissions: []string{contracts.PermissionFileRead}})
	if err := db.Run(reader, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		before := len(published(t, tx))
		returned, err := svc.SetAlt(ctx, tx, row.ID, "reader changed this", false, row.UpdatedAt)
		if err == nil || returned != nil {
			t.Errorf("read-only actor caption returned row=%v error=%v; want a refusal and no row", returned != nil, err)
		}
		current, err := crud.Get[*contracts.File](tx, row.ID)
		if err != nil {
			return err
		}
		if current.Alt != "" || !current.UpdatedAt.Equal(row.UpdatedAt) || len(published(t, tx)) != before {
			t.Errorf("read-only actor mutation left alt=%q, moved revision=%v, new events=%d; want no effects", current.Alt, !current.UpdatedAt.Equal(row.UpdatedAt), len(published(t, tx))-before)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
