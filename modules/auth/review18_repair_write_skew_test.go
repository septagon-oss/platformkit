package auth_test

// Review 18's pin. internal.RepairSeededRoles reads a tenant's roles, works out
// which dead grants its own seeder wrote, and hands SetRoleChanged the rest. The
// tenant's advisory lock is taken inside that write — after the read that
// decided the list — so a role write committing in between never reaches the
// derivation: the repair writes its stale list, which both revokes a grant it
// was never asked about and restores one somebody just took away. Round 16's
// case races one repair against another, whose lists are equal by construction —
// the only race a stale read survives. This races it against an ordinary role
// write by a different operator: the schedule is forced through the port the
// repair is handed, the write is an ordinary committed transaction, and rows
// decide. billing:catalog leaving the row is the reachability probe, so nothing
// here passes because the repair did nothing.

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/auth"
	"github.com/septagon-oss/platformkit/modules/auth/contracts"
	"github.com/septagon-oss/platformkit/modules/notification"
	"github.com/septagon-oss/platformkit/modules/user"
)

// racing reads roles, commits another operator's role write, returns what it read.
type racing struct {
	contracts.Service
	t        *testing.T
	conn     *db.Conn
	tenant   tenancy.Tenant
	declared []tenancy.Grant
	write    []string
	fired    bool
	locked   bool
}

func (r *racing) Roles(ctx context.Context, tx db.Tx[db.Tenant]) ([]*contracts.Role, error) {
	out, err := r.Service.Roles(ctx, tx)
	if err != nil || r.fired {
		return out, err
	}
	r.fired = true
	e := db.Run(tenancy.WithTenant(context.Background(), r.tenant), r.conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			if e := tx.DB().Exec("SET LOCAL lock_timeout = '3s'").Error; e != nil {
				return e
			}
			_, e := r.Service.SetRole(ctx, tx, contracts.RoleAdmin, r.write, r.declared)
			return e
		})
	if e != nil && !strings.Contains(e.Error(), "lock timeout") {
		r.t.Fatalf("the other operator's role write: %v", e)
	}
	// A lock timeout means the repair already holds the tenant's role lock
	// across its own read, which closes the window from the other side.
	r.locked = e != nil
	return out, nil
}

func TestARepairDoesNotOverwriteAConcurrentRoleWrite(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	dead := []tenancy.Grant{{Permission: "role:manage"}, {Permission: "file:read"},
		{Permission: "task:read"}, {Permission: "billing:catalog", Operator: true}}
	now := []tenancy.Grant{{Permission: "role:manage"}, {Permission: "file:read"}, {Permission: "task:read"}}
	fails := func(what string, err error) {
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	for _, step := range []struct{ other, want []string }{
		{[]string{"*", "file:read", "task:read"}, []string{"*", "file:read", "task:read"}},
		{[]string{"*"}, []string{"*"}},
	} {
		tenant := tenancy.Tenant{ID: uuid.New(), Slug: "operator", Operator: true}
		in := func(fn func(context.Context, db.Tx[db.Tenant]) error) error {
			return db.Run(tenancy.WithTenant(t.Context(), tenant), conn, fn)
		}
		fails("seed the installation before the billing module left",
			dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
				return auth.SeedRoles(ctx, tx, tenant, dead, nil)
			}))
		fails("add a live grant to the row the seeder wrote", in(
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, e := svc.SetRole(ctx, tx, contracts.RoleAdmin,
					[]string{"*", "billing:catalog", "file:read"}, dead)
				return e
			}))
		given := &racing{Service: svc, t: t, conn: conn, tenant: tenant, declared: now, write: step.other}
		fails("repair it", in(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, e := auth.RepairSeededRoles(ctx, tx, given, now, nil, true)
			return e
		}))
		var got []string
		fails("read the row back", in(func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			held, e := svc.Roles(ctx, tx)
			for _, r := range held {
				if r.Name == contracts.RoleAdmin {
					got = []string(r.Grants)
				}
			}
			return e
		}))
		if given.locked {
			if slices.Contains(got, "billing:catalog") {
				t.Errorf("the write could not interleave, yet the dead grant is still held: %v", got)
			}
			continue
		}
		if !slices.Equal(got, step.want) {
			t.Errorf("a repair raced against %v leaves the administrator holding %v, want %v",
				step.other, got, step.want)
		}
	}
}
