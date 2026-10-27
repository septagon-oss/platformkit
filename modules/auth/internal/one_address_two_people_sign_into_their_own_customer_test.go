package internal_test

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
)

// The one address two customers may each invite their own person to becomes two
// active members once both have completed their own invitation, and the login
// lookup decides which of them a sign-in at one of those doors belongs to. The
// lookup is the fragile one here: it is a raw `lower(email) = ?` take bounded only
// by the transaction's own tenant, so the reading that keeps them apart is the
// same reading that lets the two invitations exist at one address at all. This
// asks it at the point where a mix-up would cost the most — after both people set
// the same password, when what a person types cannot say which customer it is
// for. Each customer's own view of the address is asked to hold exactly one
// person, and the sign-in at each door is asked to open that customer's own
// person and nobody else's, so a lookup that reached the address across customers
// answers a session for the wrong person rather than passing on a shared password.
func TestOneAddressTwoActivePeopleSignInIntoTheirOwnCustomer(t *testing.T) {
	const (
		shared  = "single.person@example.com"
		chosen  = "a sufficiently strong password"
		preface = "the sign-in of the one address two customers hold"
	)
	f := invitationSQL(t)
	globex := globexSession(t, f)
	abroad := invitedIn(t, f, globex, shared)
	atHome := invitedIn(t, f, f.ctx, shared)
	abroadLink := offeredIn(t, f, globex, abroad)
	homeLink := offeredIn(t, f, f.ctx, atHome)
	if abroadLink == "" || homeLink == "" {
		t.Fatalf("the links mailed for %s were %q and %q, want one live link per customer to complete",
			shared, abroadLink, homeLink)
	}
	if _, err := setPasswordIn(t, f, globex, abroadLink); err != nil {
		t.Fatalf("%s completing its invitation in the first customer returned %v, want two active people at %s",
			shared, err, shared)
	}
	if _, err := setPasswordIn(t, f, f.ctx, homeLink); err != nil {
		t.Fatalf("%s completing its invitation in the second customer returned %v, want two active people at %s",
			shared, err, shared)
	}
	for _, own := range []struct {
		door   context.Context
		person uuid.UUID
	}{
		{globex, abroad.ID},
		{f.ctx, atHome.ID},
	} {
		if rows := peopleAtIn(t, f, own.door, shared); rows != 1 {
			t.Errorf("the shared address %s holds %d people in the customer that is about to sign in, want exactly one: its own",
				shared, rows)
		}
		session, identity, err := signedInAt(t, f, own.door, shared, chosen)
		if err != nil {
			t.Errorf("%s at the door of its own customer returned %v, want a session for that customer's own person",
				preface, err)
			continue
		}
		if session.UserID != own.person || identity.UserID != own.person {
			t.Errorf("%s at the door of its own customer opened %s (identity %s), want its own person %s",
				preface, session.UserID, identity.UserID, own.person)
		}
	}
}

// signedInAt signs the one address in through the door of the tenant named and
// answers what that tenant was given: the session and the identity the service
// opened for it, or the refusal that door decided.
func signedInAt(t *testing.T, f invitationSQLFixture, ctx context.Context,
	email, password string) (*contracts.Session, *contracts.Identity, error) {
	t.Helper()
	var (
		session  *contracts.Session
		identity *contracts.Identity
	)
	err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var err error
		session, identity, err = f.Auth.Login(ctx, tx, email, password, contracts.Client{})
		return err
	})
	return session, identity, err
}

// peopleAtIn counts the people the tenant named can see at one address. The
// address is unique inside a customer and not across them, so the honest answer
// here is one, and a count above one is a view that reached past the tenant its
// transaction names.
func peopleAtIn(t *testing.T, f invitationSQLFixture, ctx context.Context, address string) int64 {
	t.Helper()
	var rows int64
	if err := db.Run(ctx, f.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw(`SELECT count(*) FROM users WHERE lower(email) = $1 AND deleted_at IS NULL`,
			address).Row().Scan(&rows)
	}); err != nil {
		t.Fatal(err)
	}
	return rows
}
