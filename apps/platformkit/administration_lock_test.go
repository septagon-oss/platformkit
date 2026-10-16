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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"

	"github.com/septagon-oss/platformkit/kit/config"
	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/httpx"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	authcontracts "github.com/septagon-oss/platformkit/modules/auth/contracts"
	tenantcontracts "github.com/septagon-oss/platformkit/modules/tenant/contracts"
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
	// The principal is the bootstrap administrator once she has been read back,
	// and nobody before that (the seeding below only reads, and installs through
	// the system transaction, which is not a caller and is not guarded).
	var acting uuid.UUID
	if s.ada != nil {
		acting = s.ada.ID
	} else if s.bob != nil {
		acting = s.bob.ID
	}
	// The principal is the bootstrap administrator. Two of these cases put an
	// administering role back on a person, and that is a guarded act since this
	// delivery (see usercontracts.Granting): a context with no caller on it is the
	// composition acting, and the composition's answer to "may I promote somebody"
	// is no — so a case that means to act as a person says who.
	return httpx.WithConn(tenancy.WithPrincipal(tenancy.WithTenant(t.Context(), s.tenant),
		tenancy.Principal{UserID: acting}), s.conn)
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

// TestTheTwoFloorsComposeIntoOneInvariant is the brief's own sequence and the
// assertion this file used to make backwards.
//
// It used to assert the hole: create a role granting role:manage, give it to
// nobody, then empty the role both administrators hold, and watch two 200s leave
// a tenant nobody inside can administer — auth's floor asked how many roles
// still grant the permission and found the spare, user's floor asked how many
// people still hold a name and never hears about this write. Each half was true
// about its own table and the composed question was asked by nobody.
//
// It is now the second write's refusal. The two floors are one rule
// (usercontracts.CheckedAdministration), each door hands it the people and the
// grants as its own write would leave them, and this sequence answers 422 for
// the write that takes the last one away and leaves both people holding it.
func TestTheTwoFloorsComposeIntoOneInvariant(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	s := twoAdministrators(t, cfg, compose(cfg))
	c := s.c

	// One. A role that grants role:manage, held by nobody. This write adds a
	// grant rather than taking one away, so it stays allowed — it is the repair
	// the refusal recommends, and refusing it would refuse the way out too.
	err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.auth.SetRole(ctx, tx, "spare", []string{authcontracts.PermissionRoleManage}, declared)
		return err
	})
	if err != nil {
		t.Fatalf("create a second administering role: %v", err)
	}

	// Two. Empty the role both people hold. The spare still grants role:manage,
	// and now that answers for nothing, because no person holds it.
	err = db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.auth.SetRole(ctx, tx, authcontracts.RoleAdmin, nil, declared)
		return err
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("emptying the role both administrators hold = %v, want it refused", err)
	}
	if !strings.Contains(err.Error(), authcontracts.PermissionRoleManage) ||
		!strings.Contains(err.Error(), "nobody who can sign in and administer this tenant") {
		t.Errorf("the refusal names neither the permission nor what the write would leave: %v", err)
	}

	// Nothing moved: the write is not half-applied, and the people this case
	// seeded can still administer the tenant through the role they hold.
	if got := administeringHolders(t, s); len(got) != 2 {
		t.Errorf("%v can still administer this tenant, want both people this case seeded to be"+
			" untouched by a write that was refused", got)
	}
	if grants := roleGrants(t, s, authcontracts.RoleAdmin); !authcontracts.Grants(grants,
		tenancy.Grant{Permission: authcontracts.PermissionRoleManage}) {
		t.Errorf("role admin grants %v after the refused write, want the permission it had", grants)
	}
	s.restore(t)
}

// TestAnEmptyingWriteAndAStandingDownWriteRace is the pair across the two
// modules, and it is the case that used to answer 200 twice.
//
// Two active people hold two different roles that each grant role:manage. One
// session empties the second person's role; another stands the first person
// down. Each write alone is legitimate — the other half of the tenant is still
// there while it is decided — so what separates them from the old defect is the
// queue: the follower is refused once it may read again, in both orders, because
// by then the state it was relying on is gone.
func TestAnEmptyingWriteAndAStandingDownWriteRace(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	s := twoAdministrators(t, cfg, compose(cfg))
	c := s.c
	appoint(t, s, s.bob.ID, authcontracts.RoleMember, "second")
	makeGrant(t, s, "second")

	if err := oneHoldsTheOtherWaits(t, s, "standing down after an emptying",
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := c.auth.SetRole(ctx, tx, "second", nil, declared)
			return err
		},
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := c.users.SetRoles(ctx, tx, s.ada.ID, []string{authcontracts.RoleMember})
			return err
		},
	); !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("ada standing down once bob's role had been emptied = %v, want it refused", err)
	}
	if got := administeringHolders(t, s); len(got) < 1 {
		t.Error("nobody can administer this tenant after the race, which is what the pair used to reach")
	}

	// The same pair the other way round, from the state it started from.
	s.restore(t)
	makeGrant(t, s, "second")
	appoint(t, s, s.bob.ID, authcontracts.RoleMember, "second")
	if err := oneHoldsTheOtherWaits(t, s, "emptying after a standing down",
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := c.users.SetRoles(ctx, tx, s.ada.ID, []string{authcontracts.RoleMember})
			return err
		},
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := c.auth.SetRole(ctx, tx, "second", nil, declared)
			return err
		},
	); !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("emptying bob's role once ada had stood down = %v, want it refused", err)
	}
	if got := administeringHolders(t, s); len(got) < 1 {
		t.Error("nobody can administer this tenant after the race, which is what the pair used to reach")
	}
	s.restore(t)
}

// TestTwoAdministratorsStandingDownAtOnce is the brief's racing pair inside one
// module: two transactions, the last two administrators, one advisory key.
//
// Both writes are the ordinary thing a person does in two tabs. The first is
// allowed, because the second administrator is still holding the role while it
// is decided; the second is refused, because by the time it is decided the first
// has committed and there would be nobody left. One refusal, one success, one
// administrator left — and the queue is proved with pg_locks, not with a clock.
func TestTwoAdministratorsStandingDownAtOnce(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	s := twoAdministrators(t, cfg, compose(cfg))
	c := s.c

	if err := oneHoldsTheOtherWaits(t, s, "the second administrator standing down",
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := c.users.SetRoles(ctx, tx, s.ada.ID, []string{authcontracts.RoleMember})
			return err
		},
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := c.users.SetRoles(ctx, tx, s.bob.ID, []string{authcontracts.RoleMember})
			return err
		},
	); !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("the second of two administrators standing down = %v, want it refused", err)
	}
	if got := administeringHolders(t, s); len(got) != 1 || got[0] != "bob@acme.localhost" {
		t.Errorf("%v can still administer this tenant, want exactly bob, who is the one left", got)
	}
	s.restore(t)
}

// TestTwoAdministeringRolesEmptiedAtOnce is the same race on the roles side: two
// roles that each grant role:manage, each held by one active person, emptied by
// two sessions at once.
func TestTwoAdministeringRolesEmptiedAtOnce(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	s := twoAdministrators(t, cfg, compose(cfg))
	c := s.c
	makeGrant(t, s, "second")
	appoint(t, s, s.bob.ID, authcontracts.RoleMember, "second")

	if err := oneHoldsTheOtherWaits(t, s, "the second administering role emptied",
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := c.auth.SetRole(ctx, tx, authcontracts.RoleAdmin, nil, declared)
			return err
		},
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := c.auth.SetRole(ctx, tx, "second", nil, declared)
			return err
		},
	); !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("emptying the second administering role = %v, want it refused", err)
	}
	// The role that survived grants role:manage and is held by an active person,
	// which is the invariant read from the side of the survivor.
	if got := administeringHolders(t, s); len(got) != 1 || got[0] != "bob@acme.localhost" {
		t.Errorf("%v hold a role that still administers this tenant, want exactly bob", got)
	}
	s.restore(t)
}

// TestTheComposedCheckJudgesOneTenant is the tenant boundary of the composed
// check: it is asked in one tenant's transaction, under one tenant's key, and
// neither the people nor the lock it sees cross over.
//
// Two tenants carry the same role names. Globex has an active holder of admin;
// Acme has exactly one, and she is about to stand down. If the holder read were
// ever to widen past the transaction — a missing RLS policy, a conn instead of a
// tx, a Holders that forgot its tenant — Acme's write would find Globex's person
// and answer 200. That is the failure this case names, and it is why the answer
// is asked here through the composition's own join rather than through a
// WHERE tenant_id written in the test.
func TestTheComposedCheckJudgesOneTenant(t *testing.T) {
	path, cfg := configure(t)
	install(t, path)
	s := twoAdministrators(t, cfg, compose(cfg))
	c := s.c
	// One administrator is the point: with two, the write would be legitimate.
	// Deactivating the second of two is allowed and is what this case needs: one
	// administrator left, so the write below is the one that takes the last one.
	err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.users.Deactivate(ctx, tx, s.bob.ID)
		return err
	})
	if err != nil {
		t.Fatalf("deactivating bob: %v", err)
	}
	globex := secondTenant(t, cfg, "globex", "root@globex.localhost")

	// Acme stands down while Globex has somebody who could administer it.
	err = db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := c.users.SetRoles(ctx, tx, s.ada.ID, []string{authcontracts.RoleMember})
		return err
	})
	if !errors.Is(err, crud.ErrInvalid) {
		t.Fatalf("the last administrator of Acme standing down = %v, want it refused: another"+
			" tenant's holders must not answer for this one", err)
	}

	// And the key is per tenant: while Acme's write holds its own lock, Globex's
	// answers 0 held and 0 waiting, so one customer does not queue behind
	// another's and the boundary holds for the lock as well as for the read.
	acmeKey, globexKey := s.key, administrationKey(globex)
	hold, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if err := tx.DB().WithContext(ctx).
				Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, acmeKey).Error; err != nil {
				return err
			}
			close(hold)
			<-release
			return nil
		})
	}()
	<-hold
	if held, waiting := administrationQueues(t, s.admin, acmeKey); held != 1 || waiting != 0 {
		t.Errorf("%d held and %d waiting on Acme's key while its own write is open", held, waiting)
	}
	if held, waiting := administrationQueues(t, s.admin, globexKey); held != 0 || waiting != 0 {
		t.Errorf("%d held and %d waiting on %q while a different tenant held its own key", held, waiting, globexKey)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("holding Acme's key: %v", err)
	}

	// Globex's own people are the ones its tenant answers for.
	err = db.Run(tenancy.WithTenant(s.ctx(t), globex), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		ids, err := c.access.Recipients(ctx, tx)
		if err != nil {
			return err
		}
		if len(ids) != 1 {
			t.Errorf("%v can administer Globex, want the one person provisioned there", ids)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("asking who administers Globex: %v", err)
	}
	if got := administeringHolders(t, s); len(got) != 1 {
		t.Errorf("%v can still administer Acme after the refused write, want ada, who was refused", got)
	}
}

// appoint writes a person's roles the way the seeding in twoAdministrators does:
// through a system transaction, which is the composition restoring a row and not
// a caller granting a role. A case that meant to act as a person says who by
// going through the service.
func appoint(t *testing.T, s administrationSessions, id uuid.UUID, roles ...string) {
	t.Helper()
	err := dbtest.System(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec("UPDATE users SET roles = ?::text[] WHERE id = ?",
			pq.StringArray(roles), id).Error
	})
	if err != nil {
		t.Fatalf("appointing a person to %v: %v", roles, err)
	}
}

// makeGrant writes a role that grants role:manage through the auth module's own
// door, in its own transaction, so a case starts from a state a route could
// reach rather than one only SQL could.
func makeGrant(t *testing.T, s administrationSessions, name string) {
	t.Helper()
	err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := s.c.auth.SetRole(ctx, tx, name, []string{authcontracts.PermissionRoleManage}, declared)
		return err
	})
	if err != nil {
		t.Fatalf("granting role:manage to %q: %v", name, err)
	}
}

// roleGrants is what one role grants, read back.
func roleGrants(t *testing.T, s administrationSessions, name string) authcontracts.Permissions {
	t.Helper()
	var out authcontracts.Permissions
	err := db.Run(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		roles, err := s.c.auth.Roles(ctx, tx)
		if err != nil {
			return err
		}
		for _, role := range roles {
			if role.Name == name {
				out = role.Grants
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("reading the grants of %q: %v", name, err)
	}
	return out
}

// secondTenant creates another tenant with the composition's own door and gives
// it an administrator, and answers with the tenant. It is the second customer:
// the same role names, provisioned the same way, in a transaction of its own.
func secondTenant(t *testing.T, cfg config.Config, slug, email string) tenancy.Tenant {
	t.Helper()
	conn, err := db.Open(t.Context(), cfg.Database.URL)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer conn.Close()
	c := compose(cfg)
	var out tenancy.Tenant
	err = dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		if _, err := c.tenants.Create(ctx, tx, tenantcontracts.NewTenant{
			Slug: slug, Name: strings.ToUpper(slug), Host: slug + ".localhost",
		}); err != nil {
			return err
		}
		if out, err = c.tenants.ByHost(ctx, tx, slug+".localhost"); err != nil {
			return err
		}
		_, err = c.users.Provision(ctx, tx, out.ID, email, "", adminPass,
			[]string{authcontracts.RoleAdmin})
		return err
	})
	if err != nil {
		t.Fatalf("creating a second tenant: %v", err)
	}
	if held, waiting := administrationQueues(t, dbtest.Open(t, cfg.Database.MigrateURL), administrationKey(out)); held != 0 || waiting != 0 {
		t.Errorf("%d held and %d waiting on a key nobody has taken", held, waiting)
	}
	return out
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
	// The state a case starts from, written the way the seeding above writes it: a
	// system transaction, the composition restoring a row and not a caller
	// granting a role. SetRoles is now guarded (usercontracts.Granting), and after
	// the first direction nobody in this tenant could grant anything — which is
	// precisely what the guard is for, and nothing to do with the lock this case
	// is about.
	err := dbtest.System(s.ctx(t), s.conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return tx.DB().Exec("UPDATE users SET roles = ?::text[] WHERE id = ?",
			"{admin}", s.ada.ID).Error
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
