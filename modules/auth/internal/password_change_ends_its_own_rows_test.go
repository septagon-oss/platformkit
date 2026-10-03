package internal_test

// The row scope of the one revocation: a password change ends its own sessions.
//
// 04e563f merged two DELETE statements into internal.Service.revoke and made
// both revocations publish: the password change's clause now leaves an
// auth.session_revoked per machine it signed out. That widens what a raw
// statement does with a caller's transaction — it both ends rows and reports
// them — and the neighbouring case covers only the "keep none"
// caller across two tenants. This is the other caller, the password change,
// inside one tenant, where row-level security is no help: every row here is
// visible to this transaction and the only clause between Ada's password change
// and Bhavna's session is `user_id = ?`.
//
// Three claims, in the order a person would lose them:
//
// - a refused change writes nothing and emits nothing: the wrong current
// password ends no session and adds no revocation to the outbox;
// - the accepted change publishes exactly the rows its own statement removed —
// two, not one and not three;
// - it ends nobody else's: Bhavna's session still identifies, and Ada keeps
// the machine she asked from.
//
// Every assertion reaches the state through a live-session count and a count of
// the outbox, so it holds whichever sentence the answers carry.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/contracts/authtest"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

func TestAPasswordChangeEndsOnlyTheRowsItsOwnStatementRemoved(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users, svc := realUsers(), internal.NewService(realUsers(), nil, internal.Delivery{})
	seed(t, conn, acme)
	ctx := httpx.WithConn(t.Context(), conn)

	var ada, bhavna, kept uuid.UUID
	err := db.Run(tenancy.WithTenant(ctx, acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		adaSession, adaID := sessionOfPerson(t, ctx, tx, svc, users, "ada@acme.example.com")
		_, bhavna = sessionOfPerson(t, ctx, tx, svc, users, "bhavna@acme.example.com")
		ada, kept = adaID, adaSession.ID
		for _, agent := range []string{"Firefox", "Safari"} {
			if _, _, err := svc.Open(ctx, tx, ada, contracts.Client{UserAgent: agent}); err != nil {
				return err
			}
		}

		// The refused change: no revocation reaches the trail. Had it ended a
		// session, the count the accepted change reports would change too.
		if err := svc.ChangePassword(ctx, tx, ada, kept, "not her passphrase", "a new passphrase"); !errors.Is(err, contracts.ErrCredentials) {
			t.Errorf("a password change with the wrong current password = %v, want %v", err, contracts.ErrCredentials)
		}
		if ended := revocations(t, tx); ended != 0 {
			t.Errorf("the refused change published %d revocations: a mutation nobody performed put rows on the trail", ended)
		}

		// The accepted one: her other two machines, and not Bhavna's.
		if err := svc.ChangePassword(ctx, tx, ada, kept, authtest.Password, "a new passphrase"); err != nil {
			return err
		}
		if ended := revocations(t, tx); ended != 2 {
			t.Errorf("the change published %d revocations, want the 2 rows its own statement removed", ended)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("change Ada's password: %v", err)
	}

	inside := tenancy.WithTenant(ctx, acme)
	if got := liveSessions(t, inside, conn, svc, ada); got != 1 {
		t.Errorf("%d of Ada's sessions are live after her change, want the one she asked from", got)
	}
	if got := liveSessions(t, inside, conn, svc, bhavna); got != 1 {
		t.Errorf("%d of Bhavna's sessions are live after Ada's change, want the one no clause of Ada's command may reach", got)
	}
}

// revocations counts this tenant's auth.session_revoked rows in the caller's
// transaction — the rows the commit would carry out of the door.
func revocations(t *testing.T, tx db.Tx[db.Tenant]) int {
	t.Helper()
	counted := 0
	for _, name := range outbox(t, tx) {
		if strings.HasSuffix(name, ".session_revoked") {
			counted++
		}
	}
	return counted
}
