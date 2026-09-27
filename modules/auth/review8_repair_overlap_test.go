package auth_test

import (
	"context"
	"encoding/json"
	"slices"
	"sync"
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

// The three cases below ask one question about RepairSeededRoles that no case in
// this package asks: what does it write into a role it did not read?
//
// internal.RepairSeededRoles reads the tenant's roles once, at the top, and
// derives each role's new list from that one read — the grants it saw, minus the
// dead ones — and hands the derived list to SetRole, which re-reads the row under
// the tenant's advisory lock only to decide whether anything changed at all. So
// what is written is the row as the repair saw it, with the dead grants deleted.
// Round 1's TestTwoRepairsOfOneRoleAreOneWrite shows that is safe against the
// other case in the manual — a second repair deriving the same list — and these
// cases change the other writer into something else: a row the tenant edited
// through SetRole after the read (the first two cases), and the same person's
// write landing inside the read-to-write window (the third).
//
// The overlap is one case and it is written as one: ovWas is the catalogue of
// the installation as it was, ovNow the same installation after the module
// owning billing:catalog left, and finance an initial role the application's
// literal still names.

var (
	// ovWas defined billing:catalog and intranet:read; ovNow defines only
	// intranet:read, so billing:catalog is the dead grant and intranet:read is
	// one a composed module goes on defining.
	// ovWas defined billing:catalog and intranet:read; ovNow the same
	// installation after the module owning billing:catalog left, so
	// billing:catalog is the dead grant and intranet:read one a composed module
	// goes on defining.
	ovWas = []tenancy.Grant{{Permission: "role:manage"}, {Permission: "intranet:read"},
		{Permission: "billing:catalog"}}
	// task:read is here so that the third case has a permission a composed
	// module defines and the fixture's row does not already hold.
	ovNow = []tenancy.Grant{{Permission: "role:manage"}, {Permission: "intranet:read"},
		{Permission: "task:read"}}
	// ovDefaults is the application's initial-role literal. It names both, so
	// SeededGrants reads billing:catalog back as the seeder's own grant, and
	// intranet:read is in the row for a reason the literal does not record.
	ovDefaults = []contracts.Role{{Name: "finance",
		Grants: contracts.Permissions{"role:manage", "intranet:read", "billing:catalog"}}}
)

// TestTheRepairSubtractsFromTheRowRatherThanRebuildingItFromTheLiteral is the
// floor. finance holds three grants: two a composed module still defines and one
// that left with its module. The repair's own literal names two of the three, so
// a repair that wrote what the literal says rather than what the row holds fails
// here — it would put billing:catalog back and take intranet:read away.
func TestTheRepairSubtractsFromTheRowRatherThanRebuildingItFromTheLiteral(t *testing.T) {
	_, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	tenant := ovSeed(t, conn, svc)

	found, err := ovRepair(t, conn, svc, tenant)
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !slices.Contains(found["finance"], "billing:catalog") {
		t.Fatalf("the repair reported nothing to take from finance, so it reached no row: %v", found)
	}
	grants := ovRoleGrants(t, conn, svc, tenant, "finance")
	if slices.Contains(grants, "billing:catalog") {
		t.Errorf("the repair left the dead grant in finance: %v", grants)
	}
	for _, live := range []string{"role:manage", "intranet:read"} {
		if !slices.Contains(grants, live) {
			t.Errorf("the repair took %q from finance, which a composed module defines: %v", live, grants)
		}
	}
}

// TestTheRepairOfARoleSomebodyEmptiedOfItsDeadGrantReportsNothing is the case the
// route forces. CheckedPermissions refuses a list naming a permission no module
// defines, so while finance holds billing:catalog the only write that row can
// accept is one that takes the dead grant out too — the administrator tidying up
// through the roles screen, which is the way back repair.go names. When that
// write commits first, the repair has nothing left to do: it reports nothing,
// writes nothing and publishes nothing. A repair that rebuilt the row from its
// literal would fail here by taking intranet:read away.
func TestTheRepairOfARoleSomebodyEmptiedOfItsDeadGrantReportsNothing(t *testing.T) {
	adminDB, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	tenant := ovSeed(t, conn, svc)

	// The tenant's own write, through the module's ordinary role write, against
	// the catalogue that is in force now.
	if err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, e := svc.SetRole(ctx, tx, "finance", []string{"role:manage", "intranet:read"}, ovNow)
			return e
		}); err != nil {
		t.Fatalf("the tenant's own edit of finance: %v", err)
	}
	edited := ovRoleGrants(t, conn, svc, tenant, "finance")

	found, err := ovRepair(t, conn, svc, tenant)
	if err != nil {
		t.Fatalf("repair after the tenant's edit: %v", err)
	}
	if len(found) != 0 {
		t.Errorf("the repair reported %v after the tenant had taken the dead grant out itself", found)
	}
	if grants := ovRoleGrants(t, conn, svc, tenant, "finance"); !slices.Equal(grants, edited) {
		t.Errorf("a repair with nothing to take changed finance: was %v, now %v", edited, grants)
	}
	var events int
	if err := adminDB.QueryRowContext(t.Context(),
		"SELECT count(*) FROM platformkit_outbox WHERE name = $1 AND tenant_id = $2",
		contracts.EventRoleSet, tenant.ID).Scan(&events); err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	if events != 1 {
		t.Errorf("the tenant's edit published %d %s events, want the one it made", events, contracts.EventRoleSet)
	}
}

// TestTheAuditNamesEveryGrantTheRepairTookWhenTheRowMovedUnderneathIt is the
// overlap, and what it holds the repair to is the one claim repair.go makes about
// writing: that the removal goes through SetRole, so state and outbox event
// commit together.
//
// A second transaction grants finance a permission and commits in the window
// between the repair's read of the roles and its write of the derived list — the
// window the tenant's advisory lock does not cover, because the lock is taken
// after the read. SetRole's last-write-wins whole-row write then puts the row
// back as the repair saw it, and intranet:read leaves with the stale snapshot.
// Whether that lost update is SetRole's to prevent is not this case's question —
// two people PUTting the same role already lose one of them, on origin/main and
// by design. What this case holds the repair to, and what it must keep holding,
// is that the grant the repair took away is named in the auth.role_set row it
// publishes: the loss is in the audit, with the repair as its author, and never
// invisible because the removal came out of a stale read.
func TestTheAuditNamesEveryGrantTheRepairTookWhenTheRowMovedUnderneathIt(t *testing.T) {
	adminDB, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	tenant := ovSeed(t, conn, svc)

	read, commit := make(chan struct{}), make(chan struct{})
	gate := &ovGate{Service: svc, read: read, wait: commit}
	go func() {
		defer close(commit)
		<-read
		// A transaction of its own, the way a second request has one: the repair
		// is not its own reader's transaction, and it must not be this one's
		// either.
		err := db.Run(tenancy.WithTenant(context.Background(), tenant), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, e := svc.SetRole(ctx, tx, "finance",
					[]string{"role:manage", "intranet:read", "task:read"}, ovNow)
				return e
			})
		if err != nil {
			t.Errorf("the concurrent grant of task:read: %v", err)
		}
	}()

	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			_, e := auth.RepairSeededRoles(ctx, tx, gate, ovNow, ovDefaults, true)
			return e
		})
	if err != nil {
		t.Fatalf("repair overlapping a grant: %v", err)
	}
	<-commit

	var rawWas, rawNow []byte
	if err := adminDB.QueryRowContext(t.Context(),
		`SELECT payload->'was', payload->'now' FROM platformkit_outbox
		 WHERE name = $1 AND tenant_id = $2 ORDER BY created_at DESC, id DESC LIMIT 1`,
		contracts.EventRoleSet, tenant.ID).Scan(&rawWas, &rawNow); err != nil {
		t.Fatalf("read the last %s event: %v", contracts.EventRoleSet, err)
	}
	var was, now []string
	if err := json.Unmarshal(rawWas, &was); err != nil {
		t.Fatalf("the last event's was is %s: %v", rawWas, err)
	}
	if err := json.Unmarshal(rawNow, &now); err != nil {
		t.Fatalf("the last event's now is %s: %v", rawNow, err)
	}
	// The repair did write: the row it left holds no dead grant. Read back by
	// name, from the row itself, so this case reaching its assertion does not
	// depend on anything the repair printed or published.
	if grants := ovRoleGrants(t, conn, svc, tenant, "finance"); slices.Contains(grants, "billing:catalog") {
		t.Fatalf("the repair reached no row: finance still holds billing:catalog: %v", grants)
	}
	// And the permission that left with the stale snapshot left named.
	if !slices.Contains(was, "task:read") {
		t.Fatalf("the row the repair wrote from was %v, which does not name task:read: the concurrent grant never landed and this case proved nothing", was)
	}
	if slices.Contains(now, "task:read") {
		t.Errorf("task:read is still in the row, so nothing left with the stale read: now %v", now)
	}
}

// TestARepairThatWroteNoRowReportsNoRemoval is the same stale read seen from the
// report instead of the row, and it is the claim apps/platformkit/roles.go makes
// about its own output: "a line saying removed is a claim about a row, and a
// commit that fails takes the removal back with it", which is why f97b2e2 moved
// the printing after the commit.
//
// There is a second run that makes the same false line, and no commit failure is
// involved. Two operators run repair-roles --remove at once; both read the roles
// before either writes them, which the advisory lock cannot prevent because
// SetRole takes it after the read. The loser then derives the list the winner
// already wrote, SetRole changes nothing and publishes nothing — and the loser
// still returns the grant it took away from nobody, which the command prints as
// removed. Two lines, one removal, one tenant told twice that a person made a
// change nobody made.
func TestARepairThatWroteNoRowReportsNoRemoval(t *testing.T) {
	adminDB, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	tenant := ovSeed(t, conn, svc)

	first, both := make(chan struct{}), make(chan struct{})
	errs := make(chan int, 2)
	run := func(gate *ovGate) {
		n := 0
		err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				found, e := auth.RepairSeededRoles(ctx, tx, gate, ovNow, ovDefaults, true)
				if e == nil && len(found["finance"]) > 0 {
					n = len(found["finance"])
				}
				return e
			})
		if err != nil {
			t.Errorf("a concurrent repair: %v", err)
		}
		errs <- n
	}
	go run(&ovGate{Service: svc, read: first, wait: both})
	go run(&ovGate{Service: svc, readAfter: first, both: both})
	total := 0
	for range 2 {
		total += <-errs
	}

	if grants := ovRoleGrants(t, conn, svc, tenant, "finance"); slices.Contains(grants, "billing:catalog") {
		t.Fatalf("two repairs left the dead grant in finance: %v", grants)
	}
	var events int
	if err := adminDB.QueryRowContext(t.Context(),
		"SELECT count(*) FROM platformkit_outbox WHERE name = $1 AND tenant_id = $2",
		contracts.EventRoleSet, tenant.ID).Scan(&events); err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	if events != 1 {
		t.Errorf("two overlapping repairs published %d %s events, want 1", events, contracts.EventRoleSet)
	}
	if total != 1 {
		t.Errorf("the two overlapping repairs together reported removing billing:catalog %d times, want once: a run that changed no row printed a removal it did not make", total)
	}
}

// --- fixture and helpers ---

// ovSeed creates the installation the repair exists for: one customer tenant,
// seeded by the catalogue that still defined billing:catalog, with finance
// holding two live grants and the dead one beside them.
func ovSeed(t *testing.T, conn *db.Conn, svc contracts.Service) tenancy.Tenant {
	t.Helper()
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "services-law"}
	err := dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
		return auth.SeedRoles(ctx, tx, tenant, ovWas, ovDefaults)
	})
	if err != nil {
		t.Fatalf("seed the installation as the older seeder left it: %v", err)
	}
	before := ovRoleGrants(t, conn, svc, tenant, "finance")
	if !slices.Contains(before, "billing:catalog") || !slices.Contains(before, "intranet:read") {
		t.Fatalf("the fixture seeded finance %v, want the dead grant and a live one beside it", before)
	}
	return tenant
}

func ovRepair(t *testing.T, conn *db.Conn, svc contracts.Service,
	tenant tenancy.Tenant) (map[string][]string, error) {
	t.Helper()
	var found map[string][]string
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var e error
			found, e = auth.RepairSeededRoles(ctx, tx, svc, ovNow, ovDefaults, true)
			return e
		})
	return found, err
}

func ovRoleGrants(t *testing.T, conn *db.Conn, svc contracts.Service,
	tenant tenancy.Tenant, name string) []string {
	t.Helper()
	var out []string
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		roles, e := svc.Roles(ctx, tx)
		if e != nil {
			return e
		}
		for _, r := range roles {
			if r.Name == name {
				out = []string(r.Grants)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("read %s's role %q: %v", tenant.Slug, name, err)
	}
	return out
}

// ovGate is the rendezvous: it lets the repair read the roles, holds it there
// while another transaction writes, and releases it to write its own list. Both
// shapes are here because one case needs the other writer to be a repair, and
// then the two reads have to happen in either order with both complete before
// either write.
type ovGate struct {
	contracts.Service
	read      chan struct{}
	wait      chan struct{}
	readAfter chan struct{}
	both      chan struct{}
	once      sync.Once
}

func (g *ovGate) Roles(ctx context.Context, tx db.Tx[db.Tenant]) ([]*contracts.Role, error) {
	roles, err := g.Service.Roles(ctx, tx)
	if err != nil {
		return nil, err
	}
	g.once.Do(func() {
		if g.readAfter != nil {
			<-g.readAfter
			close(g.both)
			return
		}
		close(g.read)
		<-g.wait
	})
	return roles, nil
}
