package internal_test

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/auth/internal"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// TestRevokeAllEndsOnlyWhatItsOwnStatementRemoved is this case's pin over the
// cure: the read-then-delete became one raw DELETE ... RETURNING, and a raw
// statement is only as tenant-bounded as the transaction carrying it. Two
// tenants hold sessions; the command runs in the first. Its return value must
// equal the auth.session_revoked rows its own transaction committed (the count
// the route reports and the audit copy trusts), acme's rows must all be gone,
// and globex's rows and trail must be untouched.
func TestRevokeAllEndsOnlyWhatItsOwnStatementRemoved(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users, svc := realUsers(), internal.NewService(realUsers(), nil, internal.Delivery{})
	seed(t, conn, acme)
	seed(t, conn, globex)
	acmeCtx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	globexCtx := httpx.WithConn(tenancy.WithTenant(t.Context(), globex), conn)
	var ada, bob uuid.UUID
	err := db.Run(acmeCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, ada = sessionOfPerson(t, ctx, tx, svc, users, "ada@acme.example.com")
		for _, agent := range []string{"Firefox", "Safari"} {
			_, _, err := svc.Open(ctx, tx, ada, contracts.Client{UserAgent: agent})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("open acme's sessions: %v", err)
	}
	err = db.Run(globexCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, bob = sessionOfPerson(t, ctx, tx, svc, users, "bob@globex.example.com")
		return nil
	})
	if err != nil {
		t.Fatalf("open globex's session: %v", err)
	}
	var ended int
	err = db.Run(acmeCtx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		if ended, err = svc.RevokeAllSessions(ctx, tx, ada); err != nil {
			return err
		}
		published := 0
		for _, name := range outbox(t, tx) {
			if strings.HasSuffix(name, ".session_revoked") {
				published++
			}
		}
		if published != ended {
			t.Errorf("the command returned %d and published %d revocations in the same transaction", ended, published)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("revoke acme's sessions: %v", err)
	}
	if ended != 3 {
		t.Errorf("RevokeAllSessions returned %d, want acme's three sessions", ended)
	}
	if got := liveSessions(t, acmeCtx, conn, svc, ada); got != 0 {
		t.Errorf("%d of acme's sessions are left, want none", got)
	}
	if got := liveSessions(t, globexCtx, conn, svc, bob); got != 1 {
		t.Errorf("%d of globex's sessions are left, want the one acme's DELETE must never reach", got)
	}
	err = db.Run(globexCtx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		for _, name := range outbox(t, tx) {
			if strings.HasSuffix(name, ".session_revoked") {
				t.Errorf("globex's outbox carries %s: acme's revocation reached another tenant's trail", name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read globex's outbox: %v", err)
	}
}
