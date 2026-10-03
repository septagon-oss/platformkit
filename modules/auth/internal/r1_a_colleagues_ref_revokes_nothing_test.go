package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
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

// TestAColleaguesSessionRefRevokesNothingInsideOneTenant is the row-scope claim
// the sessions file makes about a ref, taken one step nearer than the tenant
// boundary.
//
// internal/sessions.go says: "The ref must belong to this person as well as to
// this tenant. A ref that is somebody else's session, another tenant's session,
// or no session at all is one answer — crud.ErrNotFound". The tenant half is
// proven, hard, in TestAnotherTenantsSessionRefRevokesNothing, where the row is
// not visible at all. This is the case where the row IS in this tenant and IS
// visible to this transaction, and the only thing between Ada's machine and
// Bhavna's session is `user_id = ?` in a command whose whole purpose is to end
// a credential. Row-level security cannot help here: the row is this tenant's,
// so the filter is the whole of the boundary, and a list that renders a ref per
// row and a route that takes it as a path parameter is a form a person can edit.
//
// So the case asks for Bhavna's ref from a command acting as Ada, and demands
// three things — the refusal, Bhavna still being signed in afterwards, and no
// entry in the trail for a revocation that did not happen.
func TestAColleaguesSessionRefRevokesNothingInsideOneTenant(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{})
	seed(t, conn, acme)

	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
	var bhavna, bhavnaSession uuid.UUID
	var ref string
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		session, id := sessionOfPerson(t, ctx, tx, svc, users, "bhavna@acme.example.com")
		bhavna, bhavnaSession, ref = id, session.ID, contracts.SessionRef(session.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("sign Bhavna in: %v", err)
	}

	// Ada, in the same tenant, signed in on her own machine, holding Bhavna's ref
	// and Bhavna's user id.
	var ada uuid.UUID
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, ada = sessionOfPerson(t, ctx, tx, svc, users, "ada@acme.example.com")
		if err := svc.RevokeSession(ctx, tx, ada, ref); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("revoking a colleague's session ref = %v, want the same refusal an unknown ref gets", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("acme: %v", err)
	}

	// Bhavna is still signed in: the refusal wrote nothing.
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.Identify(ctx, tx, bhavnaSession, nobody); err != nil {
			t.Errorf("the colleague's session stopped identifying after a stranger's refusal: %v", err)
		}
		var events int64
		if err := tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventSessionRevoked).
			Count(&events).Error; err != nil {
			return err
		}
		if events != 0 {
			t.Errorf("the outbox holds %d revocations for a refused command, want none", events)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read back: %v", err)
	}

	// And the list Ada reads off her own screen names only her own machine. What
	// a list names, its revoke route can end, so a row that is not hers in a
	// list she can revoke from is the bug this case is about — and the row that
	// is hers is the one row she may end.
	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		list, err := svc.Sessions(ctx, tx, ada, uuid.Nil)
		if err != nil {
			return err
		}
		if len(list) != 1 {
			t.Errorf("Ada's list holds %d sessions, want the one she opened", len(list))
		}
		for _, row := range list {
			if row.Ref == ref {
				t.Error("Ada's list names Bhavna's session, by the ref that ends it")
			}
		}
		// Bhavna, asked for by her own id in the same tenant, still sees her one
		// session — the refusal above took nothing off her list.
		hers, err := svc.Sessions(ctx, tx, bhavna, uuid.Nil)
		if err != nil {
			return err
		}
		if len(hers) != 1 || hers[0].Ref != ref {
			t.Errorf("Bhavna's own list holds %d rows after a stranger's refusal, want the one session she has",
				len(hers))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
}
