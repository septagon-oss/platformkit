package usertest_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

func TestFakeInvitationsConform(t *testing.T) {
	usertest.RunInvitations(t, func(t *testing.T, run func(usertest.InvitationFixture)) {
		fake := usertest.NewFakeWithAdministration([]string{"admin"})
		tenant := tenancy.Tenant{ID: uuid.New()}
		clock := porttest.NewClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
		actor, err := fake.Provision(t.Context(), db.Tx[db.System]{}, tenant.ID, "actor@example.com", "", "a sufficiently long password", []string{"admin"})
		if err != nil {
			t.Fatal(err)
		}
		fake.ConfigureInvitations(clock, contracts.InvitationTiming{Lifetime: time.Hour, ResendInterval: 5 * time.Minute}, usertest.InvitationTestAuthority{})
		ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenant), tenancy.Principal{UserID: actor.ID})
		run(usertest.InvitationFixture{Ctx: ctx, Commands: fake, Reader: fake, Advance: clock.Advance, Published: fake.Published,
			Legacy: func() *contracts.User {
				fake.ConfigureInvitations(clock, contracts.InvitationTiming{}, nil)
				u, err := fake.Invite(ctx, db.Tx[db.Tenant]{}, uuid.NewString()+"@example.com", "Legacy")
				fake.ConfigureInvitations(clock, contracts.InvitationTiming{Lifetime: time.Hour, ResendInterval: 5 * time.Minute}, usertest.InvitationTestAuthority{})
				if err != nil {
					t.Fatal(err)
				}
				return u
			}})
	})
}
