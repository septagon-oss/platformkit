package internal_test

import (
	"context"
	"testing"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/events/providers/memory"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

func TestADeferredMailFailureNeverPersistsItsCredential(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	ctx, stop := context.WithCancel(t.Context())
	defer stop()
	users := realUsers()
	mailer := &verificationFailingMailer{fail: true}
	_, manifest := auth.New(auth.Deps{
		Users: users, Mailer: mailer, Mails: mailLedger(), Hosts: authtest.Host("acme.example.com"),
	})
	seed(t, conn, acme)
	transport := memory.New()
	if err := events.Consume(ctx, conn, transport, manifest.Subscriptions); err != nil {
		t.Fatal(err)
	}
	if err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := users.Invite(ctx, tx, "invited@acme.example.com", "Invited")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := events.Relay(ctx, conn, transport); err != nil {
		t.Fatal(err)
	}
	var dead int
	if err := admin.QueryRowContext(ctx, "SELECT count(*) FROM platformkit_dead_letters WHERE name = $1", usercontracts.EventInvited).Scan(&dead); err != nil {
		t.Fatal(err)
	}
	if dead != 1 || mailer.token == "" {
		t.Fatalf("terminal invitation rows = %d, credential reached transport = %v", dead, mailer.token != "")
	}
	refuseNothingYet(t, admin, mailer.token)
}
