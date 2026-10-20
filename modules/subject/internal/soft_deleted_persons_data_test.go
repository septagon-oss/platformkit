package internal_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/subject"
	"github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/modules/subject/internal"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestPersonalDataCommandsStillReachASoftDeletedPerson(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	people, _ := user.New(user.Deps{
		Administration: &usercontracts.AdministrationFunc{Ask: func(context.Context, db.Tx[db.Tenant]) ([]string, error) {
			return nil, nil
		}},
		Granting: &usercontracts.GrantingFunc{Ask: func(context.Context, db.Tx[db.Tenant]) (bool, error) {
			return true, nil
		}},
		// Which languages this tenant's people may be written in is the tenant
		// module's answer in the reference composition. What these cases exercise is
		// erasure of a hidden person, so the answer here is the one language this
		// file could otherwise be read in; the language door has its own cases in
		// modules/user.
		LocalePolicy: &usercontracts.LocalePolicyFunc{Ask: func(context.Context, db.Tx[db.Tenant]) (tenancy.Languages, error) {
			return tenancy.Languages{Default: "en"}, nil
		}},
	})
	person := personaldata.Subject{ID: uuid.New(), Email: "retired@example.com", DisplayName: "Retired Person"}
	actor := personaldata.Subject{ID: uuid.New(), Email: "officer@example.com", DisplayName: "Officer"}
	seed(t, conn, person)
	seed(t, conn, actor)
	if err := transact(t, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE users SET deleted_at = ? WHERE id = ?", db.Now(), person.ID).Error
	}); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	svc := internal.NewService(internal.Deps{
		People: people,
		Parts:  []personaldata.Part{{Module: "alpha", Data: ownerStub{name: "alpha", rec: rec}}},
		Now:    func() time.Time { return time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC) },
	})
	events.Declare[contracts.Erased](contracts.EventErased)
	ctx := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), actor.ID),
		tenancy.Principal{UserID: actor.ID, Permissions: []string{contracts.PermissionExport, contracts.PermissionErase}})
	var bundle *contracts.Bundle
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		bundle, err = svc.Export(ctx, tx, person.ID)
		return err
	})
	if err != nil || bundle == nil {
		t.Errorf("export of a held soft-deleted person = %v, bundlePresent=%v", err, bundle != nil)
	} else if !bytes.Contains(bundle.Zip, []byte(person.Email)) {
		t.Errorf("export does not carry the held person's address")
	}
	var receipt *contracts.Erasure
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		receipt, err = svc.Erase(ctx, tx, contracts.EraseInput{Subject: person.ID, Confirm: person.Email})
		return err
	})
	if err != nil || receipt == nil {
		t.Errorf("erasure of a held soft-deleted person = %v, receiptPresent=%v", err, receipt != nil)
	}
	if n := published(t, conn, contracts.EventErased); n != 1 {
		t.Errorf("successful erasure of the held person must commit one event, got %d", n)
	}
}
