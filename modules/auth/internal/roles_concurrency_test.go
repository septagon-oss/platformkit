package internal_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"

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
)

// declared is the catalogue these cases hand SetRole: the one permission the
// floor is about.
var declared = []tenancy.Grant{{Permission: contracts.PermissionRoleManage}}

// roleService is the real service over a schema of this test's own, seeded the
// way tenant creation seeds one.
func roleService(t *testing.T) (*internal.Service, *db.Conn, context.Context) {
	t.Helper()
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc := internal.NewService(realUsers(), &authtest.Notices{}, delivery(&authtest.Mailbox{}))
	seed(t, conn, acme)
	return svc, conn, httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)
}

// administers reports how many of this tenant's roles grant role:manage. It is
// read in a transaction of its own, after the others have committed, because
// what the floor protects is the committed state and not anybody's snapshot.
func administers(t *testing.T, ctx context.Context, conn *db.Conn, svc *internal.Service) []string {
	t.Helper()
	var names []string
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		roles, err := svc.Roles(ctx, tx)
		if err != nil {
			return err
		}
		for _, role := range roles {
			if contracts.Grants(role.Grants, tenancy.Grant{Permission: contracts.PermissionRoleManage}) {
				names = append(names, role.Name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read the roles back: %v", err)
	}
	return names
}

// TestTwoAdministeringRolesCannotBothStandDown is the floor read at the one
// moment it is easiest to walk through: two of them at once.
//
// The guard reads the other roles and then writes, and kit/db sets no isolation
// level, so both transactions run at read committed and see each other's rows
// as they were before. Two administrators pressing save in two tabs — or one
// person double-submitting — each read a tenant that still had somebody else
// who could administer it, and both were allowed to stop. The tenant ended with
// nobody, which is the state the floor exists to prevent, reached by the floor
// being right twice about a fact that stopped being true in between.
func TestTwoAdministeringRolesCannotBothStandDown(t *testing.T) {
	svc, conn, ctx := roleService(t)

	// A second role that can administer, so the first stand-down is legitimate.
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.SetRole(ctx, tx, "second", []string{contracts.PermissionRoleManage}, declared)
		return err
	})
	if err != nil {
		t.Fatalf("grant a second role: %v", err)
	}
	if got := administers(t, ctx, conn, svc); len(got) != 2 {
		t.Fatalf("%v can administer, want the two this case is about", got)
	}

	// The first transaction writes and then waits, holding its transaction open.
	written, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if _, err := svc.SetRole(ctx, tx, contracts.RoleAdmin, nil, declared); err != nil {
				return err
			}
			close(written)
			<-release
			return nil
		})
	}()
	<-written

	// The second starts while the first is still uncommitted, and asks the same
	// question about the same tenant.
	second := make(chan error, 1)
	go func() {
		second <- db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, err := svc.SetRole(ctx, tx, "second", nil, declared)
			return err
		})
	}()

	// It must not be able to answer yet: the fact it needs belongs to a
	// transaction that has not committed. Waiting is the whole fix.
	select {
	case err := <-second:
		close(release)
		<-first
		t.Fatalf("the second stand-down answered %v while the first was still open", err)
	case <-time.After(time.Second):
	}
	close(release)

	if err := <-first; err != nil {
		t.Fatalf("the first stand-down = %v, want it allowed", err)
	}
	if err := <-second; !errors.Is(err, crud.ErrInvalid) {
		t.Errorf("the second stand-down = %v, want ErrInvalid", err)
	}
	if got := administers(t, ctx, conn, svc); len(got) == 0 {
		t.Error("no role can administer this tenant any more; both stand-downs were allowed")
	}
}

// TestTheAdministrationLockIsTheAgreedKey pins the one thing two modules have
// to agree about without importing each other.
//
// modules/user takes the same advisory lock over the same property from the
// other side — it counts the people holding a role that can administer, this
// counts the roles — and two floors on different keys do not serialize against
// each other at all: emptying a second administering role and standing the last
// administrator down would both pass, each having verified what the other was
// falsifying. The agreement is a string and a hash function, and both halves
// matter: the same key through hashtext rather than hashtextextended is a
// different lock.
//
// So this reads the lock the service actually took out of pg_locks, and
// compares it against the key computed in SQL from the literal spelled out
// here. An edit to either side of the agreement fails this, which is the point
// of writing the literal twice rather than importing it once.
func TestTheAdministrationLockIsTheAgreedKey(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc := internal.NewService(realUsers(), &authtest.Notices{}, delivery(&authtest.Mailbox{}))
	seed(t, conn, acme)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)

	// The agreement, written out rather than built from the code under test.
	key := "administration/" + acme.ID.String()
	// pg_locks splits a bigint advisory key across two oid columns, high half
	// in classid and low half in objid, and the hash is signed — so both halves
	// are masked into oid range before they are compared. kit/jobs reads its
	// own locks the same way.
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
	// held() asks a connection of its own, so it can see the lock while the
	// transaction that took it is still open.
	var taken int
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, err := svc.SetRole(ctx, tx, "editor", []string{contracts.PermissionRoleManage}, declared); err != nil {
			return err
		}
		taken = held()
		return nil
	})
	if err != nil {
		t.Fatalf("SetRole: %v", err)
	}
	if n := taken; n != 1 {
		t.Errorf("%d locks on %q while a role was being written, want the one this module takes"+
			" — the key or the hash no longer matches what modules/user takes", n, key)
	}
	// And it is a transaction lock, so it is gone without anybody releasing it.
	if n := held(); n != 0 {
		t.Errorf("%d locks on the agreed key after the transaction ended", n)
	}
}

// TestAFailedReadIsNotAFreePass is the other way a floor gives way: not by
// deciding wrongly, but by deciding on nothing.
//
// SetRole reads what the role grants today in order to know whether this write
// is taking role:manage away. When that read failed, the error used to be
// dropped on the floor: the guard then saw a role that granted nothing, decided
// nothing was leaving, and let the INSERT ... ON CONFLICT DO UPDATE overwrite
// the row anyway. A check whose precondition is a read nobody looked at is a
// check that passes by accident.
//
// The read is broken on its own, and that is the whole design of this case. An
// aborted transaction breaks everything after it, so the advisory lock fails
// first and the read branch is never reached — the case this replaces asserted
// nothing once the lock moved ahead of the read. Revoking SELECT on one table
// leaves a transaction that can take its lock and can write, and cannot read
// the row it is about to replace, which is exactly the state the guard has to
// survive.
func TestAFailedReadIsNotAFreePass(t *testing.T) {
	admin, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc := internal.NewService(realUsers(), &authtest.Notices{}, delivery(&authtest.Mailbox{}))
	seed(t, conn, acme)
	ctx := httpx.WithConn(tenancy.WithTenant(t.Context(), acme), conn)

	// The role the application connects as, asked of the connection itself
	// rather than of the environment: dbtest.URLs drops and recreates the
	// schema, so a test cannot ask it a second question.
	var appRole string
	err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Raw("SELECT current_user").Scan(&appRole).Error
	})
	if err != nil || appRole == "" {
		t.Fatalf("who the application connects as = %q, %v", appRole, err)
	}
	if _, err := admin.ExecContext(t.Context(),
		"REVOKE SELECT ON roles FROM "+pq.QuoteIdentifier(appRole)); err != nil {
		t.Fatalf("revoke the read: %v", err)
	}

	err = db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		_, err := svc.SetRole(ctx, tx, contracts.RoleAdmin, nil, declared)
		return err
	})
	if err == nil {
		t.Fatal("SetRole answered nil although it could not read what the role grants")
	}
	if strings.Contains(err.Error(), "write the role") {
		t.Errorf("the write was attempted after the guard could not read: %v", err)
	}
	if !strings.Contains(err.Error(), "read the role") {
		t.Errorf("the failure is not reported where it happened: %v", err)
	}
	// And the row is what it was. Read as the owner, because the application
	// role can no longer see it.
	var granted pq.StringArray
	const q = "SELECT permissions FROM roles WHERE tenant_id = $1 AND name = $2"
	if err := admin.QueryRowContext(t.Context(), q, acme.ID, contracts.RoleAdmin).Scan(&granted); err != nil {
		t.Fatalf("read the role back as the owner: %v", err)
	}
	if !slices.Equal([]string(granted), []string{contracts.Wildcard}) {
		t.Errorf("admin grants %v; the guard did not read and the write went ahead anyway", granted)
	}
}
