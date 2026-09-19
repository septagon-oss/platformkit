package internal_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/user"
	"github.com/septagon-oss/platformkit/modules/user/contracts"
	"github.com/septagon-oss/platformkit/modules/user/contracts/usertest"
	"github.com/septagon-oss/platformkit/modules/user/internal"
)

// tenantWith is a schema of this case's own, a connection to it, and a context
// already carrying the tenant — the shape a concurrency case needs, because two
// transactions at once cannot share one test's single open transaction.
func tenantWith(t *testing.T) (*db.Conn, context.Context) {
	t.Helper()
	conn, ctx, _ := tenantWatched(t)
	return conn, ctx
}

// tenantWatched is the same with the owner connection kept, for the one case
// that has to look at what a still-open transaction is holding.
func tenantWatched(t *testing.T) (*db.Conn, context.Context, *sql.DB) {
	t.Helper()
	admin, conn := dbtest.Schema(t, user.Migrations)
	return conn, httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn), admin
}

// administratorIn creates somebody who can administer this tenant today —
// active, with a password, holding the administering role — in a transaction of
// its own, and returns their id.
//
// The password is not decoration. Somebody who has not accepted their
// invitation is deliberately not counted when the floor asks who is left, so an
// invited administrator would make every stand-down below refuse for the wrong
// reason. See contracts.CanAdminister.
func administratorIn(t *testing.T, ctx context.Context, conn *db.Conn, svc *internal.Service, email string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := svc.Invite(ctx, tx, email, "")
		if err != nil {
			return err
		}
		if err := svc.SetPassword(ctx, tx, u.ID, "correct horse battery staple"); err != nil {
			return err
		}
		u, err = svc.SetRoles(ctx, tx, u.ID, []string{usertest.Administering})
		id = u.ID
		return err
	})
	if err != nil {
		t.Fatalf("appoint %s: %v", email, err)
	}
	return id
}

// administratorsIn is who can still administer this tenant, read in a
// transaction of its own after the others have committed — because what the
// floor protects is the committed state and not anybody's snapshot.
func administratorsIn(t *testing.T, ctx context.Context, conn *db.Conn) []string {
	t.Helper()
	var emails []string
	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		var users []*contracts.User
		if err := tx.DB().Find(&users).Error; err != nil {
			return err
		}
		for _, u := range users {
			if u.CanAdminister([]string{usertest.Administering}) {
				emails = append(emails, u.Email)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the users back: %v", err)
	}
	return emails
}

// blockedOnAdministration waits until somebody is queued for this tenant's
// administration lock and reports it, or fails.
//
// It replaces a stopwatch. The old version slept a second and concluded from
// silence that the second transaction was waiting, which is a guess with a
// generous margin rather than an observation — on this machine the unguarded
// write answers in about seventy milliseconds, so the margin was real, but a
// loaded runner makes the same assertion say nothing. pg_locks records the
// waiter with granted = false, so the fact itself is readable: a row on this
// key that nobody has been given is a backend queued for it.
func blockedOnAdministration(t *testing.T, admin *sql.DB, tenant uuid.UUID) {
	t.Helper()
	key := "administration/" + tenant.String()
	const q = `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted
		AND classid = ((hashtextextended($1, 0) >> 32) & 4294967295)::oid
		AND objid = (hashtextextended($1, 0) & 4294967295)::oid`
	deadline := time.Now().Add(20 * time.Second)
	for {
		var waiting int
		if err := admin.QueryRowContext(t.Context(), q, key).Scan(&waiting); err != nil {
			t.Fatalf("read pg_locks: %v", err)
		}
		if waiting > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("nothing is queued for %q; the second write was not blocked by the first", key)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestTwoAdministratorsCannotBothStandDown is the floor read at the one moment
// it is easiest to walk through: two of them at once.
//
// The rule reads the other users and then writes, and kit/db sets no isolation
// level, so both transactions run at read committed and the two writes touch
// different primary keys. Two administrators pressing the same button in two
// tabs — or one person double-submitting — each read a tenant that still had
// somebody else who could administer it, both were allowed, and the tenant
// ended with neither. The floor was right twice about a fact that stopped being
// true in between.
//
// pg_advisory_xact_lock on the tenant is what makes the reads and the write one
// step. The waiting is the fix, so the waiting is what this asserts — read out
// of pg_locks rather than timed: blockedOnAdministration waits for the second
// transaction to appear as a backend queued on the agreed key, and only then is
// its silence evidence. It must then refuse once the first has committed.
func TestTwoAdministratorsCannotBothStandDown(t *testing.T) {
	conn, ctx, admin := tenantWatched(t)
	svc := newService()
	ada := administratorIn(t, ctx, conn, svc, "ada@acme.example.com")
	grace := administratorIn(t, ctx, conn, svc, "grace@acme.example.com")

	// The first stands down and then waits, holding its transaction open.
	written, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, err := svc.SetRoles(ctx, tx, ada, nil); err != nil {
				return err
			}
			close(written)
			<-release
			return nil
		})
	}()
	<-written

	// The second starts while the first is still uncommitted, and asks the same
	// question about the same tenant. Deactivate rather than SetRoles, because
	// the two doors have to queue behind each other and not only behind
	// themselves.
	second := make(chan error, 1)
	go func() {
		second <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.Deactivate(ctx, tx, grace)
			return err
		})
	}()

	// Deterministic: the second transaction is queued for the lock the first
	// holds, observed rather than inferred from a quiet second.
	blockedOnAdministration(t, admin, acme.ID)
	select {
	case err := <-second:
		close(release)
		<-first
		t.Fatalf("the second stand-down answered %v while the first was still open", err)
	default:
	}
	close(release)

	if err := <-first; err != nil {
		t.Fatalf("the first stand-down = %v, want it allowed", err)
	}
	if err := <-second; !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("the second stand-down = %v, want ErrInvalid", err)
	}
	if got := administratorsIn(t, ctx, conn); len(got) == 0 {
		t.Error("nobody can administer this tenant any more; both stand-downs were allowed")
	}
}

// TestDeletingAndStandingDownQueueBehindEachOther is the same race across the
// third door, which is not a lifecycle command at all: DELETE {id} is the
// generated CRUD route, guarded by the Spec's AfterDelete hook.
//
// It is the one door where the row lock is taken before the floor's lock — the
// route reads and locks the row before any hook can run — so it is also the one
// that says the lock order is the same everywhere. A path that took the tenant
// lock first would be the other half of a deadlock with this one.
func TestDeletingAndStandingDownQueueBehindEachOther(t *testing.T) {
	conn, ctx, admin := tenantWatched(t)
	svc := newService()
	ada := administratorIn(t, ctx, conn, svc, "ada@acme.example.com")
	grace := administratorIn(t, ctx, conn, svc, "grace@acme.example.com")

	deleted, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			// What rest.Spec.deleteRow does, in the order it does it: the
			// row read and locked, the soft delete, then the hook.
			u, err := crud.GetForUpdate[*contracts.User](tx, ada)
			if err != nil {
				return err
			}
			if err := crud.Delete[*contracts.User](tx, ada, true); err != nil {
				return err
			}
			if err := svc.RefuseLastAdministrator(ctx, tx, u); err != nil {
				return err
			}
			close(deleted)
			<-release
			return nil
		})
	}()
	<-deleted

	second := make(chan error, 1)
	go func() {
		second <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.SetRoles(ctx, tx, grace, nil)
			return err
		})
	}()

	blockedOnAdministration(t, admin, acme.ID)
	select {
	case err := <-second:
		close(release)
		<-first
		t.Fatalf("the survivor stood down with %v while the delete was still open", err)
	default:
	}
	close(release)

	if err := <-first; err != nil {
		t.Fatalf("deleting one of two administrators = %v, want it allowed", err)
	}
	if err := <-second; !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("the survivor standing down = %v, want ErrInvalid", err)
	}
	if got := administratorsIn(t, ctx, conn); len(got) == 0 {
		t.Error("nobody can administer this tenant any more")
	}
}

// TestAnAnswerNobodyGotIsNotAFreePass is the other way a floor gives way: not
// by deciding wrongly, but by deciding on nothing.
//
// The floor asks the application which roles can administer this tenant. If
// that answer is lost — the roles table is unreachable, the composition wired
// something that fails — an implementation that shrugged and carried on would
// conclude that nobody administers anything, and every one of the three doors
// would be open again at exactly the moment nothing can be verified. The
// failure is returned where it happened instead, so the write does not land.
func TestAnAnswerNobodyGotIsNotAFreePass(t *testing.T) {
	conn, ctx := tenantWith(t)
	unreachable := errors.New("the roles table is not readable right now")
	svc := internal.NewService(&contracts.AdministrationFunc{Ask: func(context.Context, db.Tx[db.Tenant]) ([]string, error) {
		return nil, unreachable
	}})

	var id uuid.UUID
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := svc.Invite(ctx, tx, "ada@acme.example.com", "")
		id = u.ID
		return err
	})
	if err != nil {
		t.Fatalf("Invite: %v", err)
	}
	// All three doors ask, including the one granting a role rather than taking
	// one away: whether this write is taking administration away is itself
	// something only the answer can decide, so there is no write here that gets
	// to skip the question.
	for _, door := range []struct {
		name string
		run  func(context.Context, db.Tx[db.Tenant]) error
	}{
		{"roles", func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.SetRoles(ctx, tx, id, []string{usertest.Administering})
			return err
		}},
		{"deactivate", func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.Deactivate(ctx, tx, id)
			return err
		}},
		{"delete", func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			u, err := crud.GetForUpdate[*contracts.User](tx, id)
			if err != nil {
				return err
			}
			return svc.RefuseLastAdministrator(ctx, tx, u)
		}},
	} {
		if err := db.Run(ctx, conn, door.run); !errors.Is(err, unreachable) {
			t.Errorf("%s with no answer available = %v, want the failure reported", door.name, err)
		}
	}
}

// TestTheAdministrationLockIsTheAgreedKey pins the one thing two modules have to
// agree about without importing each other.
//
// modules/auth takes the same advisory lock over the same property from the
// other side — it counts the roles that grant role:manage, this counts the
// people holding one — and two floors on different keys do not serialize
// against each other at all. Emptying a second administering role and standing
// the last administrator down would both pass, each having verified exactly
// what the other was falsifying, and the tenant both floors exist to protect
// would end up with nobody.
//
// The agreement is a string and a hash function, and both halves are
// load-bearing: the same key through hashtext is a different lock from the same
// key through hashtextextended, and a disagreement there is silent — two
// transactions each take a lock, neither waits, and nothing reports it.
//
// So this reads the lock the service actually took out of pg_locks and compares
// it with the key computed in SQL from the literal written out here. Writing
// the literal a second time is the point: an edit to either half of the
// agreement fails this case rather than quietly reopening the hole.
func TestTheAdministrationLockIsTheAgreedKey(t *testing.T) {
	conn, ctx, admin := tenantWatched(t)
	svc := newService()

	// The agreement, written out rather than built from the code under test.
	key := "administration/" + acme.ID.String()
	// pg_locks splits a bigint advisory key across two oid columns — the high
	// half in classid, the low half in objid — and the hash is signed, so both
	// halves are masked into oid range before they are compared.
	const q = `SELECT count(*) FROM pg_locks WHERE locktype = 'advisory'
		AND classid = ((hashtextextended($1, 0) >> 32) & 4294967295)::oid
		AND objid = (hashtextextended($1, 0) & 4294967295)::oid`
	held := func() int {
		t.Helper()
		var n int
		if err := admin.QueryRowContext(t.Context(), q, key).Scan(&n); err != nil {
			t.Fatalf("read pg_locks: %v", err)
		}
		return n
	}

	if n := held(); n != 0 {
		t.Fatalf("%d locks on the agreed key before anybody took one", n)
	}
	// admin is a connection of its own, so it sees the lock while the
	// transaction that took it is still open.
	// acquirable asks, from a session of its own, whether somebody else could
	// take the agreed lock right now. It is the try form so the answer arrives
	// instead of the test hanging, and the statement is the one modules/auth
	// runs.
	acquirable := func() bool {
		t.Helper()
		var ok bool
		const try = `SELECT pg_try_advisory_xact_lock(hashtextextended($1, 0))`
		if err := admin.QueryRowContext(t.Context(), try, key).Scan(&ok); err != nil {
			t.Fatalf("try the agreed lock: %v", err)
		}
		return ok
	}

	var taken int
	var available bool
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		u, err := svc.Invite(ctx, tx, "ada@acme.example.com", "")
		if err != nil {
			return err
		}
		if _, err := svc.SetRoles(ctx, tx, u.ID, []string{usertest.Administering}); err != nil {
			return err
		}
		taken, available = held(), acquirable()
		return nil
	})
	if err != nil {
		t.Fatalf("the writing transaction: %v", err)
	}
	if taken != 1 {
		t.Errorf("%d locks on %q while a person's roles were being written, want the one this module takes"+
			" — the key or the hash no longer matches what modules/auth takes", taken, key)
	}
	// Counting rows says the key matches. This says what the key is for: a
	// second holder of the agreed lock could not have had it. The statement is
	// the one modules/auth runs, spelled out here rather than imported, and the
	// try form answers instead of waiting so the case stays deterministic.
	if available && taken == 1 {
		t.Errorf("the agreed lock was free to a second session while this module held it;"+
			" %q resolves to two different locks", key)
	}
	// And it is a transaction lock, so it goes without anybody releasing it —
	// read both ways, because a lock nothing can see and a lock nothing can
	// take are two different claims.
	if !acquirable() {
		t.Errorf("the agreed lock was still held after the transaction ended")
	}
	if n := held(); n != 0 {
		t.Errorf("%d locks on the agreed key after the transaction ended", n)
	}
}
