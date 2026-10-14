package auth_test

// RepairSeededRoles reads a tenant's roles twice when it is going to write: once
// unlocked, to find out whether this tenant holds a dead grant at all, and once
// under the tenant's role lock, whose answer is the one it subtracts from. This
// races an ordinary role write into the gap between those two reads and then reads
// the two rows back, so what the repair decided is read from a run that still had
// something to take.
//
// One role is not enough to say anything here: while a row holds a dead grant,
// CheckedPermissions refuses every list that keeps it, so any whole-row write to it
// takes the dead grant out itself and a repair that re-reads has nothing left to do
// — a run that writes nothing proves only that it wrote nothing. Two roles make the
// repair write one row while the other is being moved underneath it, which is the
// state in which its report, its audit row and its neighbour's grants can all be
// read together. A repair that derived its lists from the first read would rebuild
// the row the other operator had just written: it would report taking a grant that
// row no longer held, publish an event naming permissions nobody chose, and revoke
// the permission that write had just handed out without saying so.

import (
	"context"
	"database/sql"
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

// twoRolesWere defined billing:catalog, which left with its module, and
// intranet:read, which a composed module goes on defining; twoRolesNow is the same
// installation with task:read in place of the departed one.
var (
	twoRolesWere = []tenancy.Grant{{Permission: "role:manage"}, {Permission: "intranet:read"},
		{Permission: "billing:catalog"}}
	twoRolesNow = []tenancy.Grant{{Permission: "role:manage"}, {Permission: "intranet:read"},
		{Permission: "task:read"}}
	// Both roles are the seeder's own and both hold the dead grant, so the repair
	// arrives intending to take it from each. intern holds one fewer live permission
	// than finance so that the two rows' audit entries can never be mistaken for one
	// another.
	twoRolesDefaults = []contracts.Role{
		{Name: "finance", Grants: contracts.Permissions{"role:manage", "intranet:read", "billing:catalog"}},
		{Name: "intern", Grants: contracts.Permissions{"role:manage", "billing:catalog"}},
	}
)

// betweenWrites answers with the roles as they were, commits another
// transaction's write of one role, and only then returns: the read the repair acts
// on is the one taken before that write existed.
type betweenWrites struct {
	contracts.Service
	t        *testing.T
	conn     *db.Conn
	tenant   tenancy.Tenant
	role     string
	grants   []string
	declared []tenancy.Grant
	done     chan struct{}
	once     sync.Once
}

func (b *betweenWrites) Roles(ctx context.Context, tx db.Tx[db.Tenant]) ([]*contracts.Role, error) {
	held, err := b.Service.Roles(ctx, tx)
	if err != nil {
		return nil, err
	}
	b.once.Do(func() {
		defer close(b.done)
		if err := db.Run(tenancy.WithTenant(context.Background(), b.tenant), b.conn,
			func(ctx context.Context, tx db.Tx[db.Tenant]) error {
				_, e := b.Service.SetRole(ctx, tx, b.role, b.grants, b.declared)
				return e
			}); err != nil {
			b.t.Errorf("the other operator's write of role %q: %v", b.role, err)
		}
	})
	return held, nil
}

func TestARepairThatWritesLeavesWhatAnotherOperatorWroteAndNamesWhatItTook(t *testing.T) {
	adminDB, conn := dbtest.Schema(t, user.Migrations, notification.Migrations, auth.Migrations)
	svc, _ := auth.Module(auth.Deps{})
	tenant := tenancy.Tenant{ID: uuid.New(), Slug: "two-roles"}
	fails := func(what string, err error) {
		if err != nil {
			t.Fatalf("%s: %v", what, err)
		}
	}
	fails("seed the installation as the older seeder left it",
		dbtest.System(t.Context(), conn, func(ctx context.Context, tx db.Tx[db.System]) error {
			return auth.SeedRoles(ctx, tx, tenant, twoRolesWere, twoRolesDefaults)
		}))

	// The write that races the repair is an ordinary one: the tenant's own role
	// write, its own transaction, against the catalogue in force. It cannot name
	// billing:catalog, because that permission no module defines.
	gate := &betweenWrites{Service: svc, t: t, conn: conn, tenant: tenant,
		role: "intern", grants: []string{"role:manage", "task:read"}, declared: twoRolesNow,
		done: make(chan struct{})}
	var found map[string][]string
	fails("repair it", db.Run(tenancy.WithTenant(t.Context(), tenant), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			var e error
			found, e = auth.RepairSeededRoles(ctx, tx, gate, twoRolesNow, twoRolesDefaults, true)
			return e
		}))
	<-gate.done

	// What the run says it took. finance still held its dead grant when the repair
	// took the lock, so that one is its own; intern's had already left in the write
	// this run raced, and a line naming it would be a removal no write of this run
	// made.
	if got := found["finance"]; !slices.Equal(got, []string{"billing:catalog"}) {
		t.Errorf("the repair reported %v for finance, want the dead grant it took: %v", found["finance"], found)
	}
	if _, ok := found["intern"]; ok {
		t.Errorf("the repair reported %v for intern, whose dead grant the other write had already taken", found["intern"])
	}

	// What the two rows hold, read back by name from the rows themselves.
	if got := twoRolesGrants(t, conn, svc, tenant, "finance"); !slices.Equal(got, []string{"intranet:read", "role:manage"}) {
		t.Errorf("finance holds %v after the repair, want its two live grants", got)
	}
	if got := twoRolesGrants(t, conn, svc, tenant, "intern"); !slices.Equal(got, []string{"role:manage", "task:read"}) {
		t.Errorf("intern holds %v after the repair, want the write that raced it to survive whole", got)
	}

	// And what an audit would see: one auth.role_set row per role this installation
	// moved, so the repair's own is the one for finance, and it names the grant the
	// repair took and nothing it did not take.
	events := twoRolesRoleSet(t, adminDB, tenant)
	if len(events) != 2 {
		t.Fatalf("the tenant holds %d %s rows, want one for the other operator's write and one for the repair",
			len(events), contracts.EventRoleSet)
	}
	var repair *twoRolesEvent
	for i, e := range events {
		if e.Role == "finance" {
			repair = &events[i]
		}
	}
	if repair == nil {
		t.Fatalf("no %s row names finance: %v", contracts.EventRoleSet, events)
	}
	var was, now []string
	fails("read the repair's was", json.Unmarshal(repair.Was, &was))
	fails("read the repair's now", json.Unmarshal(repair.Now, &now))
	if !slices.Contains(was, "billing:catalog") {
		t.Errorf("the repair's row was %v, which does not name the grant it took", was)
	}
	if slices.Contains(now, "billing:catalog") {
		t.Errorf("the repair's row now is %v, which still names the grant it did not take away", now)
	}
	if slices.Contains(now, "task:read") || slices.Contains(was, "task:read") {
		t.Errorf("the repair's row mentions task:read (%v -> %v), which is not this run's to take", was, now)
	}
}

type twoRolesEvent struct {
	Role string          `json:"role"`
	Was  json.RawMessage `json:"was"`
	Now  json.RawMessage `json:"now"`
}

func twoRolesRoleSet(t *testing.T, adminDB *sql.DB, tenant tenancy.Tenant) []twoRolesEvent {
	t.Helper()
	rows, err := adminDB.QueryContext(t.Context(),
		`SELECT payload FROM platformkit_outbox WHERE name = $1 AND tenant_id = $2`,
		contracts.EventRoleSet, tenant.ID)
	if err != nil {
		t.Fatalf("read the %s rows: %v", contracts.EventRoleSet, err)
	}
	defer rows.Close()
	var out []twoRolesEvent
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan an event: %v", err)
		}
		var e twoRolesEvent
		if err := json.Unmarshal(raw, &e); err != nil {
			t.Fatalf("the event payload is %s: %v", raw, err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read the events: %v", err)
	}
	return out
}

func twoRolesGrants(t *testing.T, conn *db.Conn, svc contracts.Service,
	tenant tenancy.Tenant, name string) []string {
	t.Helper()
	var out []string
	err := db.Run(tenancy.WithTenant(t.Context(), tenant), conn,
		func(ctx context.Context, tx db.Tx[db.Tenant]) error {
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
