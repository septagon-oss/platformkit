package main

// The one property two modules guard from opposite sides, tested where both are
// composed.
//
// modules/user counts the people holding a role that can administer; modules/auth
// counts the roles that grant role:manage. Each module's own suite proves its half
// and pins the key it takes against its own copy of the agreed literal, so each can
// only ever see one side. Nothing else asserts the thing the agreement exists for:
// that a write in one module waits for a write in the other, in both orders. The key
// is spelled out below as a third copy, which matches only while the first two still
// agree with each other.

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	usercontracts "github.com/septagon-oss/platformkit/modules/user/contracts"
)

// declared is the catalogue SetRole is handed: the one permission both floors are
// about.
var declared = []tenancy.Grant{{Permission: authcontracts.PermissionRoleManage}}

// administrationKey is the agreement as the two modules write it — this literal and
// hashtextextended over 64 bits, in
// modules/user/internal/administration.go and modules/auth/internal/roles.go.
// Changing it means changing both in one delivery; the two module-level pin tests
// and this one are what make a one-sided edit loud instead of a quietly narrower
// lock.
func administrationKey(tenant tenancy.Tenant) string {
	return "administration/" + tenant.ID.String()
}

// administrationQueues counts the sessions holding, and waiting on, that one key.
// It reads on a connection of its own, so it can see a lock while the transaction
// that took it is still open. pg_locks splits a bigint advisory key across two oid
// columns, high half in classid, and the hash is signed, so both halves are masked
// into oid range — the same reading kit/jobs uses.
func administrationQueues(t *testing.T, admin *sql.DB, key string) (held, waiting int) {
	t.Helper()
	const q = `SELECT count(*) FILTER (WHERE granted), count(*) FILTER (WHERE NOT granted)
		FROM pg_locks WHERE locktype = 'advisory'
		AND classid = ((hashtextextended($1, 0) >> 32) & 4294967295)::oid
		AND objid = (hashtextextended($1, 0) & 4294967295)::oid`
	if err := admin.QueryRowContext(t.Context(), q, key).Scan(&held, &waiting); err != nil {
		t.Fatalf("read pg_locks: %v", err)
	}
	return held, waiting
}

// administrationSessions is the state each case starts from: the composed
// application, its tenant, and two active people holding the seeded admin role.
type administrationSessions struct {
	c      composition
	conn   *db.Conn
	admin  *sql.DB
	tenant tenancy.Tenant
	key    string
	ada    *usercontracts.User
	bob    *usercontracts.User
}

// ctx is a request context for this tenant: the same pair kit/httpx puts on a
// request, so the two services see one customer whichever of them is asked.
func (s administrationSessions) ctx(t *testing.T) context.Context {
	t.Helper()
	return httpx.WithConn(tenancy.WithTenant(t.Context(), s.tenant), s.conn)
}

// oneHoldsTheOtherWaits runs hold in one session and follow in another and proves
// the second is queued behind the first on the shared key before it may answer.
//
// The proof is pg_locks, not a clock. "It had not replied within a second" is also
// true of a saturated pool, an unrelated row lock and a slow plan, and a test that
// passed for any of those would keep passing the day the advisory lock is renamed.
// A row for this key with granted=false is the claim.
//
// It returns follow's error, so each direction can say what it expects the answer
// to be; the serialization is asserted here because it is the part worth sharing.
func oneHoldsTheOtherWaits(t *testing.T, s administrationSessions, label string,
	hold, follow func(context.Context, db.Tx[db.Tenant]) error,
) error {
	t.Helper()
	ctx := s.ctx(t)

	written, release := make(chan struct{}), make(chan struct{})
	holding := make(chan error, 1)
	go func() {
		holding <- db.Run(ctx, s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := hold(ctx, tx); err != nil {
				return err
			}
			close(written)
			<-release // hold the advisory lock across the wait, as an open request does
			return nil
		})
	}()
	<-written

	followed := make(chan error, 1)
	go func() {
		followed <- db.Run(ctx, s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return follow(ctx, tx)
		})
	}()

	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, waiting := administrationQueues(t, s.admin, s.key); waiting == 1 {
			break
		}
		select {
		case err := <-followed:
			close(release)
			<-holding
			t.Fatalf("%s: answered %v without queueing behind the other module on %q. The two"+
				" floors no longer take one lock: one side's key or hash moved, and this is the"+
				" test that can see the two halves pull apart", label, err, s.key)
		case <-time.After(20 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			close(release)
			<-holding
			t.Fatalf("%s: never appeared in pg_locks as waiting on %q", label, s.key)
		}
	}

	// Queued is not yet answered. It must still be waiting while the first session
	// holds the key, however long that takes to decide.
	select {
	case err := <-followed:
		close(release)
		<-holding
		t.Fatalf("%s: answered %v while the other module still held the lock", label, err)
	case <-time.After(250 * time.Millisecond):
	}

	close(release)
	if err := <-holding; err != nil {
		t.Fatalf("%s: the holding write = %v, want it allowed", label, err)
	}
	return <-followed
}

// TestTheAdministrationLockQueuesTheOtherModuleBehindIt is the cross-module case
// neither module can write: the two floors take one advisory lock, so a write in
// one module waits for a write in the other, in both directions because the orders
// they take are already opposite (auth locks before its deciding read; kit/rest has
// locked the user's row before user's floor runs) and "opposite orders are safe" is
// an argument about no transaction wanting both — the thing most likely to stop
// holding quietly. Nothing else can make either session wait: the writes touch
// disjoint tables and the pool is 16.
func TestTheAdministrationLockQueuesTheOtherModuleBehindIt(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	s := twoAdministrators(t, cfg, compose(cfg))
	c := s.c

	if err := oneHoldsTheOtherWaits(t, s, "user after auth",
		// auth holds: a new role granting role:manage, which its own floor allows
		// because the seeded admin role grants it too.
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := c.auth.SetRole(ctx, tx, "second", []string{authcontracts.PermissionRoleManage}, declared)
			return err
		},
		// user follows: standing the bootstrap administrator down. It has to read
		// the roles table to know which names can administer, and must not get an
		// answer from before the write it is queueing behind.
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := c.users.SetRoles(ctx, tx, s.ada.ID, []string{authcontracts.RoleMember})
			return err
		},
	); err != nil {
		t.Errorf("user.SetRoles once auth released the lock: %v", err)
	}

	// The same pair the other way round, from the state the first direction started
	// from rather than the one it left.
	s.restore(t)
	if err := oneHoldsTheOtherWaits(t, s, "auth after user",
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := c.users.SetRoles(ctx, tx, s.ada.ID, []string{authcontracts.RoleMember})
			return err
		},
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := c.auth.SetRole(ctx, tx, "second", nil, declared)
			return err
		},
	); err != nil {
		t.Errorf("auth.SetRole once user released the lock: %v", err)
	}
	s.restore(t)

	// Waiting is what these two directions are about, and the queue held: the tenant
	// still has both of the people this case seeded.
	if got := administeringHolders(t, s); len(got) != 2 {
		t.Errorf("%v can still administer this tenant, want both people this case seeded", got)
	}
}

// TestTheTwoFloorsStillDoNotComposeIntoOneInvariant asserts the hole, not the fix.
//
// Each floor asks a question about its own table: auth asks how many roles grant
// role:manage, user asks how many active people hold one of those names. Neither
// asks the composed one. So the sequence below is two writes whose guards are each
// entitled to say yes, and a tenant nobody inside can administer — with no
// concurrency at all, which is why the shared lock above is not the answer to it.
//
// It fails the day somebody closes the hole, and that is the day this file is
// rewritten to assert the opposite. Until then it is the evidence behind the
// sentence in CHANGELOG.md that says the two floors do not compose: run it rather
// than trust the sentence.
func TestTheTwoFloorsStillDoNotComposeIntoOneInvariant(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	s := twoAdministrators(t, cfg, compose(cfg))
	c := s.c

	// One. A role that grants role:manage, held by nobody. auth's floor is about
	// roles, and this write adds one rather than taking one away.
	err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.auth.SetRole(ctx, tx, "spare", []string{authcontracts.PermissionRoleManage}, declared)
		return err
	})
	if err != nil {
		t.Fatalf("create a second administering role: %v", err)
	}

	// Two. Empty the role both people hold. auth's floor asks whether any other role
	// still grants role:manage, finds spare, and answers that nothing is lost.
	err = db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.auth.SetRole(ctx, tx, authcontracts.RoleAdmin, nil, declared)
		return err
	})
	if err != nil {
		t.Fatalf("empty the role both administrators hold: %v", err)
	}

	// And nobody can administer the tenant: spare still grants role:manage and no
	// person holds it. Two writes, both accepted, nothing reported.
	if got := administeringHolders(t, s); len(got) != 0 {
		t.Fatalf("%v can still administer this tenant; this case's premise has gone away and"+
			" its assertions need rewriting against whatever closed it", got)
	}
}

// twoAdministrators gives the tenant a second active person holding the seeded admin
// role, so that a single removal is legitimate on its own and the only thing between
// the two writes and a locked tenant is the lock.
func twoAdministrators(t *testing.T, cfg config.Config, c composition) administrationSessions {
	t.Helper()
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	s := administrationSessions{
		c: c, conn: conn, admin: dbtest.Open(t, cfg.Database.MigrateURL),
	}

	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		var e error
		if s.tenant, e = c.tenants.ByHost(ctx, tx, acmeHost); e != nil {
			return e
		}
		_, e = c.users.Provision(ctx, tx, s.tenant.ID, "bob@acme.localhost", "Bob",
			"correct horse battery staple", []string{authcontracts.RoleAdmin})
		return e
	})
	if err != nil {
		t.Fatalf("install a second administrator: %v", err)
	}
	s.key = administrationKey(s.tenant)
	if held, waiting := administrationQueues(t, s.admin, s.key); held != 0 || waiting != 0 {
		t.Fatalf("%d held and %d waiting on %q before anybody took a lock", held, waiting, s.key)
	}

	err = db.Run(s.ctx(t), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		var e error
		if s.ada, e = c.users.ByEmail(ctx, tx, adminEmail); e != nil {
			return e
		}
		if s.bob, e = c.users.ByEmail(ctx, tx, "bob@acme.localhost"); e != nil {
			return e
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the two administrators back: %v", err)
	}
	if got := administeringHolders(t, s); len(got) != 2 {
		t.Fatalf("%v can administer this tenant, want the two this case seeded: the second one"+
			" is what makes a single removal legitimate", got)
	}
	return s
}

// restore puts the bootstrap administrator back on the administering role, so the
// next case starts from the state it describes rather than the one it left. A grant
// is never what a floor refuses, so this cannot be the write that fails.
func (s administrationSessions) restore(t *testing.T) {
	t.Helper()
	err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := s.c.users.SetRoles(ctx, tx, s.ada.ID, []string{authcontracts.RoleAdmin})
		return err
	})
	if err != nil {
		t.Fatalf("restore the bootstrap administrator: %v", err)
	}
}

// administeringHolders is the composed question, asked by hand: the active people
// holding a role that grants role:manage. Neither module's floor answers it — the
// claim of the second test is that neither of them asks it — so this reads both
// tables the way the application joined them.
func administeringHolders(t *testing.T, s administrationSessions) []string {
	t.Helper()
	var names []string
	err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		roles, err := s.c.auth.Roles(ctx, tx)
		if err != nil {
			return err
		}
		administering := make([]string, 0, len(roles))
		for _, role := range roles {
			if authcontracts.Grants(role.Grants, tenancy.Grant{Permission: authcontracts.PermissionRoleManage}) {
				administering = append(administering, role.Name)
			}
		}
		if len(administering) == 0 {
			return nil
		}
		var people []*usercontracts.User
		if err := tx.DB().WithContext(ctx).
			Where("deleted_at IS NULL AND status = ? AND roles && ?::text[]",
				usercontracts.StatusActive, pq.StringArray(administering)).
			Find(&people).Error; err != nil {
			return err
		}
		for _, person := range people {
			names = append(names, person.Email)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ask who can administer: %v", err)
	}
	return names
}
