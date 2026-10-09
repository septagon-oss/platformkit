package internal_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
)

// A view over the trail is a second append door, and it is one the deployment built:
// PostgreSQL resolves an INSERT through a plain view as the view's owner unless the view
// says security_invoker, so a view over the trail owned by the trail's owner writes
// history for a role the trail itself never granted INSERT to — at either level, which is
// what TestColumnAppenderCannotExpireHistory's role had to be given in order to append at
// all. The fence asked "may this role append?" of the trail's own grants, and the trail's
// own grants say nothing about a door beside them, so the role appended through the view
// and DELETEd its own row as itself, unmarked. What a role may append to the trail is a
// fact about the deployment's objects, not about which catalog row its grant was written
// against, so the fence asks it of every view that draws from the trail and of every view
// that draws from those.
//
// Two cases, because a view over a view is the same door with one more hinge in it. The
// second role is handed the far end of the chain and nothing else, so what refuses it can
// only be the hinge rather than a grant sitting next to it.

func TestViewAppenderCannotExpireHistory(t *testing.T) {
	admin, _ := dbtest.Schema(t, audit.Migrations)
	appenderBehindTheDoorCannotExpire(t, admin, "audit_events_append_door")
}

func TestViewOverViewAppenderCannotExpireHistory(t *testing.T) {
	admin, _ := dbtest.Schema(t, audit.Migrations)
	appenderBehindTheDoorCannotExpire(t, admin, "audit_events_append_door", "audit_events_append_door_deep")
}

// appenderBehindTheDoorCannotExpire builds one view over the trail, then one view over
// that, as far as doors reaches. It grants SELECT and DELETE on the trail and SELECT and
// INSERT on the last view only — so the role appends through a chain of the deployment's
// own objects while holding no INSERT on the trail at the table level or on any column of
// it — and then asks the trail what it does about an expiry by that role.
func appenderBehindTheDoorCannotExpire(t *testing.T, admin *sql.DB, doors ...string) {
	t.Helper()
	source := "audit_events"
	for _, door := range doors {
		if _, err := admin.ExecContext(t.Context(),
			"CREATE VIEW "+door+" AS SELECT * FROM "+source); err != nil {
			t.Fatalf("build the deployment's view %s over %s: %v", door, source, err)
		}
		source = door
	}
	appendDoor := doors[len(doors)-1]
	_, appDSN := dbtest.Role(t, admin,
		"SELECT, DELETE ON TABLE audit_events",
		"SELECT, INSERT ON TABLE "+appendDoor)
	conn, err := db.Open(t.Context(), appDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	ctx := tenancy.WithTenant(t.Context(), acme)
	id := uuid.New()
	// The premise, proved rather than assumed: this role appends to the trail.
	err = db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("INSERT INTO "+appendDoor+
			" (tenant_id, occurred_at, name, event_id, payload) VALUES (?, ?, ?, ?, ?)",
			acme.ID, db.Now().AddDate(-2, 0, 0), "task.task.created", id, "{}").Error
	})
	if err != nil {
		t.Fatalf("append through the deployment's view %s: %v", appendDoor, err)
	}
	err = db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("DELETE FROM audit_events WHERE event_id = ?", id).Error
	})
	if err == nil {
		t.Errorf("%s: a role that appended through the view committed DELETE of its row", appendDoor)
	} else if pg, ok := errors.AsType[*pgconn.PgError](err); !ok || pg.Code != "42501" {
		t.Errorf("%s: expiry refusal = %v, want insufficient_privilege (42501)", appendDoor, err)
	}
	var kept, marks int
	if err := admin.QueryRowContext(t.Context(),
		"SELECT (SELECT count(*) FROM audit_events WHERE event_id=$1), (SELECT count(*) FROM audit_retention_marks)", id).
		Scan(&kept, &marks); err != nil {
		t.Fatal(err)
	}
	if kept != 1 || marks != 0 {
		t.Errorf("%s: view appender left %d history rows and %d retention marks; want the unchanged row and no marks",
			appendDoor, kept, marks)
	}
}
