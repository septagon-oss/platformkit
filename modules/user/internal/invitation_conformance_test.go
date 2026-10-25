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
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

func TestSQLInvitationsConform(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	usertest.RunInvitations(t, func(t *testing.T, run func(usertest.InvitationFixture)) {
		tenant := tenancy.Tenant{ID: uuid.New()}
		actor := &contracts.User{Email: "actor@example.com", Status: contracts.StatusActive, Roles: contracts.Roles{"admin"}}
		clock := porttest.NewClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
		svc := newService()
		svc.ConfigureInvitations(clock, contracts.InvitationTiming{Lifetime: time.Hour, ResendInterval: 5 * time.Minute}, usertest.InvitationTestAuthority{})
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := crud.Create(ctx, tx, actor); err != nil {
				return err
			}
			ctx = tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: actor.ID})
			run(usertest.InvitationFixture{Ctx: ctx, Tx: tx, Commands: svc, Reader: svc, Advance: clock.Advance,
				Legacy: func() *contracts.User {
					u := &contracts.User{Email: uuid.NewString() + "@example.com", Status: contracts.StatusInvited}
					if err := crud.Create(ctx, tx, u); err != nil {
						t.Fatal(err)
					}
					return u
				},
				Published: func() []string {
					var names []string
					if err := tx.DB().Table("platformkit_outbox").Order("created_at, id").Pluck("name", &names).Error; err != nil {
						t.Fatal(err)
					}
					return names
				}})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatal(err)
		}
	})
}
