package internal_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
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
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// sessionOfPerson is a person with a password and one live session, in this tenant.
func sessionOfPerson(t *testing.T, ctx context.Context, tx db.Tx[db.Tenant], svc *internal.Service, users usercontracts.Service, email string) (*contracts.Session, uuid.UUID) {
	t.Helper()
	u, err := users.Invite(ctx, tx, email, email)
	if err != nil {
		t.Fatalf("invite %s: %v", email, err)
	}
	if err := users.SetPassword(ctx, tx, u.ID, authtest.Password); err != nil {
		t.Fatalf("set %s's password: %v", email, err)
	}
	session, _, err := svc.Login(ctx, tx, email, authtest.Password, nobody)
	if err != nil {
		t.Fatalf("sign %s in: %v", email, err)
	}
	return session, u.ID
}

// TestAnotherTenantsSessionRefRevokesNothing is the tenant boundary of the
// sessions page, and it is a database claim: the ref is a real row, presented
// inside the wrong tenant's transaction, and the policy does not return it.
//
// Two shapes are refused, and the second is the one a reviewer should look at.
// The first — acme's own caller holding globex's ref — would be refused by the
// `user_id = ?` filter alone, so it proves nothing about the boundary. The
// second asks for globex's *own* user id inside acme's transaction: a filter
// that matched would still find no row, because RLS says the row is not in this
// tenant. That is the case that fails if the policy is dropped, which is the
// failure the whole design says cannot happen by forgetting a WHERE clause.
func TestAnotherTenantsSessionRefRevokesNothing(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{})
	seed(t, conn, acme)
	seed(t, conn, globex)

	var theirs uuid.UUID
	var ref string
	var presented uuid.UUID
	ctx := httpx.WithConn(t.Context(), conn)
	atGlobex := func(run func(context.Context, db.Tx[db.Tenant]) error) {
		t.Helper()
		if err := db.Run(tenancy.WithTenant(ctx, globex), conn, run); err != nil {
			t.Fatalf("globex: %v", err)
		}
	}
	atGlobex(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		session, id := sessionOfPerson(t, ctx, tx, svc, users, "ada@globex.example.com")
		theirs, ref, presented = id, contracts.SessionRef(session.ID), session.ID
		return nil
	})

	atAcme := tenancy.WithTenant(ctx, acme)
	err := db.Run(atAcme, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, mine := sessionOfPerson(t, ctx, tx, svc, users, "ada@acme.example.com")
		if err := svc.RevokeSession(ctx, tx, mine, ref); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("acme revoking globex's ref = %v, want ErrNotFound", err)
		}
		// The same ref, asked for as the person it belongs to. Still nothing:
		// the row is not in this tenant, so no id that names it can reach it.
		if err := svc.RevokeSession(ctx, tx, theirs, ref); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("acme revoking globex's ref as globex's user = %v, want ErrNotFound", err)
		}
		// And the list cannot see it either, asked for the same way.
		list, err := svc.Sessions(ctx, tx, theirs, uuid.Nil)
		if err != nil {
			return err
		}
		if len(list) != 0 {
			t.Errorf("acme lists %d of globex's sessions, want none", len(list))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("acme: %v", err)
	}

	// Globex's session is untouched: refused, it wrote nothing.
	atGlobex(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.Identify(ctx, tx, presented, nobody); err != nil {
			t.Errorf("globex's session stopped identifying after a stranger's refusal: %v", err)
		}
		return nil
	})
}

// TestTwoTabsRevokingOneSessionSettleOnce is the concurrency claim: two
// transactions end the same session at the same time, and the tenant gets one
// revocation and one event, not two of each.
//
// Without the row lock both read the row, both delete (one affecting a row and
// one none) and both publish — a trail that says the same machine left twice,
// and a page counting two revocations nobody made. Locked, the second settles
// behind the first, finds no row, and refuses with nothing written.
func TestTwoTabsRevokingOneSessionSettleOnce(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	users := realUsers()
	svc := internal.NewService(users, nil, internal.Delivery{})
	seed(t, conn, acme)

	var (
		id  uuid.UUID
		ref string
	)
	ctx := httpx.WithConn(t.Context(), conn)
	at := tenancy.WithTenant(ctx, acme)
	err := db.Run(at, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		session, who := sessionOfPerson(t, ctx, tx, svc, users, "ada@acme.example.com")
		id, ref = who, contracts.SessionRef(session.ID)
		return nil
	})
	if err != nil {
		t.Fatalf("open a session: %v", err)
	}

	results := make([]error, 2)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = db.Run(at, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				return svc.RevokeSession(ctx, tx, id, ref)
			})
		}()
	}
	wg.Wait()

	ok, refused := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, crud.ErrNotFound):
			refused++
		default:
			t.Fatalf("RevokeSession = %v, want it to work or to refuse", err)
		}
	}
	if ok != 1 || refused != 1 {
		t.Errorf("%d revocations and %d refusals for one session, want exactly one of each", ok, refused)
	}

	var events int64
	err = db.Run(at, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("platformkit_outbox").Where("name = ?", contracts.EventSessionRevoked).
			Count(&events).Error
	})
	if err != nil {
		t.Fatalf("count the revocations: %v", err)
	}
	if events != 1 {
		t.Errorf("the outbox holds %d revocations for one session, want one", events)
	}
}
