package internal_test

// The person's own language: which writes land, which are refused, and what each
// one leaves behind.
//
// `speaksEnglishAndPortuguese` stands in for the tenant module here — this
// package's schema owns users and nothing else, so the set a choice is checked
// against is handed to the service rather than read from a tenants row it cannot
// create. What the set *is*, and who may change it, is the tenant module's own
// test; that the reference composition reaches the real one through the port is
// apps/platformkit's.

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
)

// speaksLanguages is the whole of this package's tenant module: a set somebody
// declared, offered to the command in the shape contracts.LocalePolicy asks for.
type speaksLanguages struct{ tenancy.Languages }

func (l speaksLanguages) CurrentLanguages(context.Context, db.Tx[db.Tenant]) (tenancy.Languages, error) {
	return l.Languages, nil
}

// speaksEnglishAndPortuguese is the set every case here is wired with: the order
// is the one a negotiation reads, the default first.
var speaksEnglishAndPortuguese = speaksLanguages{
	tenancy.Languages{Default: "en", Others: []string{"pt-PT"}},
}

// asSomeone is the signed-in person the transaction will be told about. The kernel
// puts it there (kit/httpx calls tenancy.WithPrincipal after authenticating); a
// case that leaves it out is an anonymous request.
func asSomeone(ctx context.Context, id uuid.UUID) context.Context {
	return tenancy.WithPrincipal(ctx, tenancy.Principal{UserID: id})
}

func TestAPersonChoosesALanguageThisTenantServes(t *testing.T) {
	conn, ctx := tenantWith(t)
	svc := newService()
	ada := invite(t, conn, "ada@acme.example.com")

	if err := db.Run(asSomeone(ctx, ada), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		before := len(outbox(t, tx))
		u, err := svc.SetPreferredLocale(ctx, tx, "pt-PT")
		if err != nil {
			return err
		}
		if u.PreferredLocale != "pt-PT" {
			t.Errorf("the row the command returned says %q, want pt-PT", u.PreferredLocale)
		}
		if names := outbox(t, tx); len(names) != before+1 || names[len(names)-1] != contracts.EventLanguageSet {
			t.Errorf("the outbox ends with %v, want %s beside the write", names, contracts.EventLanguageSet)
		}
		return nil
	}); err != nil {
		t.Fatalf("SetPreferredLocale: %v", err)
	}

	if got := preferred(t, conn, ada); got != "pt-PT" {
		t.Errorf("the row holds %q, want pt-PT", got)
	}
}

func TestALanguageThisTenantDoesNotServeIsRefusedAndLeavesNothing(t *testing.T) {
	conn, ctx := tenantWith(t)
	svc := newService()
	ada := invite(t, conn, "ada@acme.example.com")

	err := db.Run(asSomeone(ctx, ada), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.SetPreferredLocale(ctx, tx, "de")
		return err
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("SetPreferredLocale(de) = %v, want ErrInvalid: the set is the tenant's declaration", err)
	}
	if got := preferred(t, conn, ada); got != "" {
		t.Errorf("the refused choice left %q behind", got)
	}
	// The write and its event are refused together: the transaction this ran in
	// rolled back, so read the trail from a fresh one.
	if names := outboxNames(t, conn); countNamed(names, contracts.EventLanguageSet) != 0 {
		t.Errorf("the refusal published %v", names)
	}
}

func TestTheChoiceAlreadyHeldChangesNothingAndPublishesNothing(t *testing.T) {
	conn, ctx := tenantWith(t)
	svc := newService()
	ada := invite(t, conn, "ada@acme.example.com")

	err := db.Run(asSomeone(ctx, ada), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.SetPreferredLocale(ctx, tx, "pt-PT"); err != nil {
			return err
		}
		before := len(outbox(t, tx))
		again, err := svc.SetPreferredLocale(ctx, tx, "pt-PT")
		if err != nil {
			return err
		}
		if again.PreferredLocale != "pt-PT" {
			t.Errorf("the row returned by the second call says %q", again.PreferredLocale)
		}
		if names := outbox(t, tx); len(names) != before {
			t.Errorf("the second call published %v over the first's %d", names[before:], before)
		}
		return errRollback
	})
	if !errors.Is(err, errRollback) {
		t.Fatalf("SetPreferredLocale twice: %v", err)
	}
}

func TestTakingTheChoiceBackIsAWriteOfItsOwn(t *testing.T) {
	conn, ctx := tenantWith(t)
	svc := newService()
	ada := invite(t, conn, "ada@acme.example.com")

	if err := db.Run(asSomeone(ctx, ada), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.SetPreferredLocale(ctx, tx, "pt-PT"); err != nil {
			return err
		}
		if _, err := svc.SetPreferredLocale(ctx, tx, ""); err != nil {
			return err
		}
		names := outbox(t, tx)
		if countNamed(names, contracts.EventLanguageSet) != 2 {
			t.Errorf("choosing and then clearing published %v, want two language_set events: clearing is a change and the trail says when", names)
		}
		return nil
	}); err != nil {
		t.Fatalf("choose then clear: %v", err)
	}
	if got := preferred(t, conn, ada); got != "" {
		t.Errorf("after taking it back the row holds %q, want the empty choice", got)
	}
}

// TestNobodySignedInWritesNothing is the first half of house rule 9 at this door:
// the command asks who is asking, and with no answer it refuses rather than
// choosing a person for the write.
func TestNobodySignedInWritesNothing(t *testing.T) {
	conn, ctx := tenantWith(t)
	svc := newService()
	ada := invite(t, conn, "ada@acme.example.com")

	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.SetPreferredLocale(ctx, tx, "pt-PT")
		return err
	})
	if !errors.Is(err, contracts.ErrNoSignIn) {
		t.Fatalf("SetPreferredLocale with no principal = %v, want ErrNoSignIn", err)
	}
	if got := preferred(t, conn, ada); got != "" {
		t.Errorf("a command with nobody in front of it wrote %q", got)
	}
}

// TestAnotherTenantsPersonIsNotThereAtAll is the same door from the other side: the
// person the principal names is a row in another tenant, which row-level security
// makes absent rather than forbidden.
func TestAnotherTenantsPersonIsNotThereAtAll(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations)
	svc := newService()
	globexUser := inviteIn(t, conn, globex, "ada@globex.example.com")

	err := db.Run(asSomeone(tenancy.WithTenant(t.Context(), acme), globexUser), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.SetPreferredLocale(ctx, tx, "pt-PT")
			return err
		})
	if !errors.Is(err, crud.ErrNotFound) {
		t.Fatalf("another tenant's person = %v, want ErrNotFound", err)
	}
	if got := preferredIn(t, conn, globex, globexUser); got != "" {
		t.Errorf("the refused write left %q on the other tenant's row", got)
	}
}

// inviteIn creates somebody in the tenant named and returns their id, in a
// transaction of its own that commits: a case whose own transaction then fails or
// rolls back on purpose still needs a row to have happened.
func inviteIn(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := newService().Invite(ctx, tx, email, "")
		id = u.ID
		return err
	})
	if err != nil {
		t.Fatalf("invite %s: %v", email, err)
	}
	return id
}

// invite is inviteIn in acme, which is where every case but one works.
func invite(t *testing.T, conn *db.Conn, email string) uuid.UUID {
	t.Helper()
	return inviteIn(t, conn, acme, email)
}

// preferred is acme's own read of one person's choice.
func preferred(t *testing.T, conn *db.Conn, id uuid.UUID) string {
	t.Helper()
	return preferredIn(t, conn, acme, id)
}

// preferredIn reads one person's choice scoped to the tenant named, so a case
// about another tenant's row reads that tenant's row and not its own.
func preferredIn(t *testing.T, conn *db.Conn, tenant tenancy.Tenant, id uuid.UUID) string {
	t.Helper()
	var want string
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := crud.Get[*contracts.User](tx, id)
		want = u.PreferredLocale
		return err
	})
	if err != nil {
		t.Fatalf("read the choice: %v", err)
	}
	return want
}

// outboxNames reads the trail a committed write left, from a transaction of its
// own, so a case that rolled its own back can still ask what survived it.
func outboxNames(t *testing.T, conn *db.Conn) []string {
	t.Helper()
	var names []string
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Table("platformkit_outbox").Pluck("name", &names).Error
	})
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return names
}

func countNamed(names []string, want string) int {
	n := 0
	for _, name := range names {
		if name == want {
			n++
		}
	}
	return n
}
