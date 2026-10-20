package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/personaldata"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/subject"
	"github.com/septagon-oss/platformkit/modules/subject/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestSubjectCommandsRefuseAnActorWithoutTheRequiredGrant(t *testing.T) {
	for _, command := range []string{"export another person", "erase another person"} {
		t.Run(command, func(t *testing.T) {
			_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
			person := personaldata.Subject{ID: uuid.New(), Email: "private@example.com", DisplayName: "Private Person"}
			actor := personaldata.Subject{ID: uuid.New(), Email: "member@example.com", DisplayName: "Member"}
			seed(t, conn, person)
			seed(t, conn, actor)
			rec := &recorder{}
			svc := newDoor(&stamps{at: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}, &bundlesStub{}, &notesStub{}, rec)
			// An explicit empty credential ceiling grants nothing, even if an
			// application later assigns this member a more powerful role.
			ctx := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), actor.ID),
				tenancy.Principal{UserID: actor.ID, Permissions: []string{}})
			var row *contracts.Request
			var receipt *contracts.Erasure
			err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				var err error
				if command == "export another person" {
					row, err = svc.RequestExport(ctx, tx, contracts.RequestExportInput{Subject: person.ID})
				} else {
					row, receipt, err = svc.RequestErase(ctx, tx, contracts.EraseInput{Subject: person.ID, Confirm: person.Email})
				}
				return err
			})
			if err == nil {
				t.Errorf("%s succeeded with an explicit empty permission ceiling", command)
			}
			if row != nil || receipt != nil {
				t.Errorf("unauthorized command returned a row or receipt: row=%v receipt=%v", row, receipt)
			}
			var rows int64
			if err := transact(t, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return tx.DB().Model(&contracts.Request{}).Count(&rows).Error
			}); err != nil {
				t.Fatal(err)
			}
			if rows != 0 || len(rec.asks) != 0 {
				t.Errorf("unauthorized command committed %d requests and called contributors %v", rows, rec.asks)
			}
			for _, event := range []string{contracts.EventExportRequested, contracts.EventErased} {
				if n := published(t, conn, event); n != 0 {
					t.Errorf("unauthorized command emitted %d %s events", n, event)
				}
			}
		})
	}
}
