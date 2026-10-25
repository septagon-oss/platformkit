package authtest_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification/contracts/notificationtest"
	user "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

func TestFakeInvitationLinksConform(t *testing.T) {
	authtest.RunInvitationLinks(t, func(t *testing.T, run func(authtest.InvitationFixture)) {
		users := usertest.NewFake()
		tenant := tenancy.Tenant{ID: uuid.New()}
		clock := porttest.NewClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
		actor, err := users.Provision(t.Context(), db.Tx[db.System]{}, tenant.ID, "actor@example.com", "", "a sufficiently strong password", []string{"admin"})
		if err != nil {
			t.Fatal(err)
		}
		users.ConfigureInvitations(clock, user.InvitationTiming{Lifetime: contracts.TokenLifetime, ResendInterval: contracts.ResetInterval}, usertest.InvitationTestAuthority{})
		ctx := tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), tenant), tenancy.Principal{UserID: actor.ID})
		auth := authtest.NewFake(users)
		mail := new(authtest.InvitationMailbox)
		ledger := notificationtest.NewFakeMailLedger()
		auth.Mailer = mail
		auth.ConfigureInvitations(clock, users, ledger)
		run(authtest.InvitationFixture{Step: func(fn func(context.Context, db.Tx[db.Tenant]) error) error { return fn(ctx, db.Tx[db.Tenant]{}) }, Users: users, Commands: users, Links: auth, Auth: auth, Mail: mail, Advance: clock.Advance})
	})
}
