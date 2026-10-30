package internal_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	notificationmodule "github.com/septagon-oss/platformkit/modules/notification"
	usermodule "github.com/septagon-oss/platformkit/modules/user"
)

func TestSweepWinnerKeepsDeliveryUnmarked(t *testing.T) {
	admin, conn := dbtest.Schema(t, usermodule.Migrations, notificationmodule.Migrations, auth.Migrations)
	mailer := &blockingDispatchMailer{entered: make(chan struct{}), release: make(chan struct{})}
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{
		Mailer: mailer, MailDelivery: mailer, Hosts: authtest.Host("acme.example.com"), Secure: true,
	})
	seed(t, conn, acme)
	const address = "dispatch-sweep-winner@acme.example.com"
	person := invitedPerson(t, conn, users, address)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	done := make(chan error, 1)
	go func() {
		done <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return svc.Reissue(ctx, tx, address)
		})
	}()

	select {
	case <-mailer.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("the hand-off never reached the transport")
	}
	token := mailer.token
	exec(t, admin, "UPDATE auth_mail_dispatch SET created_at = created_at - interval '10 minutes'")
	if _, err := purgeOnce(t, conn, svc); err != nil {
		t.Fatalf("sweep while a hand-off was open: %v", err)
	}
	close(mailer.release)
	if err := <-done; err != nil {
		t.Fatalf("the hand-off: %v", err)
	}

	var state string
	var sentAt sql.NullTime
	if err := admin.QueryRowContext(t.Context(),
		"SELECT d.state, p.sent_at FROM auth_mail_dispatch d JOIN password_tokens p USING (tenant_id, user_id) WHERE d.user_id = $1 AND d.token_hash = $2",
		person, []byte(contracts.Hash(token))).Scan(&state, &sentAt); err != nil {
		t.Fatalf("read the dispatch and credential marks: %v", err)
	}
	if state != "unknown" || sentAt.Valid {
		t.Errorf("sweep state=%q credential sent_at=%v, want unknown with no successful delivery mark", state, sentAt)
	}
	if n := countDispatched(t, admin); n != 1 {
		t.Errorf("dispatch rows=%d, want the one row the sweep settled", n)
	}
}
