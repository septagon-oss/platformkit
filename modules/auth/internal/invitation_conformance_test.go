package internal_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/porttest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/notification"
	notificationcontracts "github.com/septagon-oss/platformkit/modules/notification/contracts"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
)

type invitationSQLFixture struct {
	authtest.InvitationFixture
	conn      *db.Conn
	ctx       context.Context
	service   *internal.Service
	clock     *porttest.Clock
	authority *usercontracts.InvitationAuthorityFunc
}

func invitationSQL(t *testing.T) invitationSQLFixture {
	t.Helper()
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "invitation"}
	clock := porttest.NewClock(time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
	authority := &usercontracts.InvitationAuthorityFunc{Ask: usertest.InvitationTestAuthority{}.InspectInvitationAuthority}
	users, _ := user.New(user.Deps{Administration: &usercontracts.AdministrationFunc{Ask: auth.AdministeringRoles}, Clock: clock, InvitationTiming: usercontracts.InvitationTiming{Lifetime: contracts.TokenLifetime, ResendInterval: contracts.ResetInterval}, InvitationAuthority: authority, Granting: allowGranting})
	mail := new(authtest.InvitationMailbox)
	notices, _ := notification.New(notification.Deps{Mailer: mail})
	ledger := notices.(notificationcontracts.MailLedger)
	service := internal.NewService(users, nil, internal.Delivery{Mailer: mail, Mails: ledger, Hosts: authtest.Host("acme.example.com"), Secure: true})
	service.ConfigureInvitations(clock, users.(contracts.InvitationUsers), users.(usercontracts.InvitationReader))
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), tenant), conn)
	actor := &usercontracts.User{Email: "actor@example.com", Status: usercontracts.StatusActive, Roles: usercontracts.Roles{"admin"}}
	if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error { return crud.Create(ctx, tx, actor) }); err != nil {
		t.Fatal(err)
	}
	ctx = tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: actor.ID})
	return invitationSQLFixture{InvitationFixture: authtest.InvitationFixture{Step: func(fn func(context.Context, db.Tx[db.Tenant]) error) error { return db.Run(ctx, conn, fn) }, Users: users, Commands: users.(usercontracts.InvitationCommands), Links: service, Auth: service, Mail: mail, Advance: clock.Advance}, conn: conn, ctx: ctx, service: service, clock: clock, authority: authority}
}
func TestSQLInvitationLinksConform(t *testing.T) {
	authtest.RunInvitationLinks(t, func(t *testing.T, run func(authtest.InvitationFixture)) { run(invitationSQL(t).InvitationFixture) })
}
