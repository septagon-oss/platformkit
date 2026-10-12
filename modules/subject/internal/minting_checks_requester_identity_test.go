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

func TestMintRefusesACallerClaimingAnotherRequestersIdentity(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, subject.Migrations)
	person := personaldata.Subject{ID: uuid.New(), Email: "private@example.com", DisplayName: "Private Person"}
	actor := personaldata.Subject{ID: uuid.New(), Email: "member@example.com", DisplayName: "Member"}
	seed(t, conn, person)
	seed(t, conn, actor)
	store := &bundlesStub{}
	svc := newDoor(&stamps{at: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)}, store, &notesStub{})
	id := ask(t, svc, conn, person)
	if err := walk(t, svc, conn, acme); err != nil {
		t.Fatal(err)
	}
	if row := readRequest(t, conn, id); row.State != contracts.StateReady {
		t.Fatalf("request state = %s, want ready", row.State)
	}
	holds := store.holds
	ctx := tenancy.WithPrincipal(tenancy.WithActor(tenancy.WithTenant(t.Context(), acme), actor.ID),
		tenancy.Principal{UserID: actor.ID, Permissions: []string{}})
	for _, command := range []string{"read", "mint"} {
		t.Run(command, func(t *testing.T) {
			err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				if command == "read" {
					row, err := svc.Request(ctx, tx, id, person.ID, false)
					if row != nil {
						t.Errorf("another credential received request %s", row.ID)
					}
					return err
				}
				link, err := svc.Mint(ctx, tx, id, person.ID, false)
				if link != nil {
					t.Errorf("another credential received a token onto request %s", link.Request)
				}
				return err
			})
			if err == nil {
				t.Errorf("%s accepted a requester id different from the transaction actor and principal", command)
			}
		})
	}
	var grants int64
	if err := transact(t, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Model(&contracts.Grant{}).Count(&grants).Error
	}); err != nil {
		t.Fatal(err)
	}
	if grants != 0 || store.holds != holds {
		t.Errorf("refused mint wrote %d grants and moved hold count from %d to %d", grants, holds, store.holds)
	}
}
