package internal_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

// The core review of 2026-09-29 (P1) recorded an event through the real audit
// service and then rewrote it through an ordinary tenant transaction. These cases
// hold the boundary that refuses it, asked of the database rather than of the Go
// code: what the application role holds, what happens anyway when a role tries,
// and what the one role that may expire history may and may not do.
//
// The two halves refuse different things, so each is asked separately. The REVOKE
// in migrations/00041 is what refuses the application today, in PostgreSQL's own
// vocabulary; the triggers are what refuses it after an operator hands the
// privilege back, and what refuses the table's owner, which no REVOKE reaches.

// appURL is the DSN the application role connects as, which is the role the review
// used and the one has_table_privilege has to be asked about.
func appURL(t *testing.T) string {
	t.Helper()
	u := os.Getenv("PLATFORMKIT_TEST_DATABASE_URL")
	if u == "" {
		t.Fatal("PLATFORMKIT_TEST_DATABASE_URL is unset; start the stack with `make up`")
	}
	return u
}

// priv is one answer from the catalog, beside the answer this delivery gives for it.
type priv struct {
	name string
	want bool
	why  string
}

// TestTrailPrivilegesRefuseARewrite asks pg_has_role's neighbour,
// has_table_privilege, what the application role holds on the trail. It is the
// cheapest statement of the boundary and the one an operator audits with psql.
func TestTrailPrivilegesRefuseARewrite(t *testing.T) {
	admin, _ := dbtest.Schema(t, audit.Migrations)
	role := dbtest.RoleOf(t, appURL(t))
	// UPDATE and TRUNCATE gone, SELECT and INSERT kept, DELETE kept and fenced by the
	// trigger: the residual this delivery states rather than hides. The application
	// may still hold DELETE; it cannot use it, because a role that may append to the
	// trail may never expire it — TestExpiryRoleIsTheOnlyDoor is the behaviour behind
	// that row, and it is the reason the privilege is not the hole it looks like.
	for _, p := range []priv{
		{"SELECT", true, "the trail is readable"},
		{"INSERT", true, "the trail is appendable"},
		{"UPDATE", false, "history is never rewritten"},
		{"TRUNCATE", false, "no row trigger can see a TRUNCATE, so this half must"},
		{"DELETE", true, "held, and refused by the trigger: see TestExpiryRoleIsTheOnlyDoor"},
		{"REFERENCES", false, "a view over the trail is a rewritable copy of it"},
		{"TRIGGER", false, "a trigger on the trail is a rule about who may rewrite it"},
	} {
		var got bool
		q := "SELECT has_table_privilege($1, 'audit_events', $2)"
		if err := admin.QueryRowContext(t.Context(), q, role, p.name).Scan(&got); err != nil {
			t.Fatalf("ask about %s: %v", p.name, err)
		}
		if got != p.want {
			t.Errorf("the application role holds %s on audit_events: %v, want %v (%s)", p.name, got, p.want, p.why)
		}
	}
}

// TestTrailRefusesEveryWriteTheApplicationCanAttempt is the review's own acceptance,
// run as the application role inside an ordinary tenant transaction — the door it
// walked through. Every statement is refused and the row stays where it was.
func TestTrailRefusesEveryWriteTheApplicationCanAttempt(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	ctx := tenancy.WithTenant(t.Context(), acme)
	svc := internal.NewService()
	id := uuid.New()
	seed(t, ctx, conn, svc, id, db.Now())

	for _, w := range []struct {
		label string
		stmt  func() string
	}{
		{"UPDATE", func() string {
			return "UPDATE audit_events SET name = 'task.forged' WHERE event_id = '" + id.String() + "'"
		}},
		{"DELETE", func() string { return "DELETE FROM audit_events WHERE event_id = '" + id.String() + "'" }},
		{"TRUNCATE", func() string { return "TRUNCATE audit_events" }},
	} {
		err := db.Run(ctx, conn, func(_ context.Context, tx db.Tx[db.Tenant]) error {
			return tx.DB().Exec(w.stmt()).Error
		})
		if err == nil {
			t.Errorf("%s of audit history committed through an ordinary tenant transaction", w.label)
		}
		var kept int
		if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events`).Scan(&kept); err != nil {
			t.Fatalf("count the trail: %v", err)
		}
		if kept != 1 {
			t.Fatalf("%s left %d rows in the trail, want the one that was there", w.label, kept)
		}
	}
}

// TestOwnerCannotRewriteOrExpireEither is the half the REVOKE cannot reach: the
// table's owner holds every privilege by definition, so only the triggers say
// anything about it — including about an expiry, because the owner may append and
// so may not expire, aged or not.
func TestOwnerCannotRewriteOrExpireEither(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	ctx := tenancy.WithTenant(t.Context(), acme)
	svc := internal.NewService()
	fresh, aged := uuid.New(), uuid.New()
	seed(t, ctx, conn, svc, fresh, db.Now())
	seed(t, ctx, conn, svc, aged, db.Now().AddDate(-2, 0, 0))

	if _, err := admin.ExecContext(t.Context(), `UPDATE audit_events SET name = 'forged'`); err == nil {
		t.Error("the table's owner rewrote the trail")
	}
	if _, err := admin.ExecContext(t.Context(), `DELETE FROM audit_events`); err == nil {
		t.Error("the table's owner deleted the trail")
	}
	// Aged past the floor, and still refused: the owner holds INSERT, and the append
	// door and the expiry door are different doors.
	if _, err := admin.ExecContext(t.Context(), `DELETE FROM audit_events WHERE occurred_at < now() - interval '365 days'`); err == nil {
		t.Error("the table's owner expired a row older than the floor")
	}
}

// TestExpiryRoleIsTheOnlyDoor is the shape the fence admits: DELETE and not INSERT,
// and only past the floor. This is the role database.retain_url names, created here
// the way a deployment creates it.
func TestExpiryRoleIsTheOnlyDoor(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	ctx := tenancy.WithTenant(t.Context(), acme)
	svc := internal.NewService()
	fresh, aged := uuid.New(), uuid.New()
	seed(t, ctx, conn, svc, fresh, db.Now())
	seed(t, ctx, conn, svc, aged, db.Now().AddDate(-2, 0, 0))

	_, retainURL := dbtest.Role(t, admin,
		"SELECT, DELETE ON TABLE audit_events",
		"SELECT, INSERT ON TABLE audit_retention_marks")
	retain, err := db.Open(t.Context(), retainURL)
	if err != nil {
		t.Fatalf("open the expiry connection: %v", err)
	}
	defer retain.Close()

	// Inside the floor: the row is not the expiry's to touch yet.
	if err := db.Run(ctx, retain, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("DELETE FROM audit_events WHERE event_id = ?", fresh).Error
	}); err == nil {
		t.Error("the expiry role removed a row inside the retention floor")
	}
	// Past it: this is the one write the trail accepts from anyone.
	var removed int64
	err = db.Run(ctx, retain, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		res := tx.DB().Exec("DELETE FROM audit_events WHERE event_id = ?", aged)
		removed = res.RowsAffected
		return res.Error
	})
	if err != nil || removed != 1 {
		t.Fatalf("expire an aged row: %v (rows %d)", err, removed)
	}
	// And the two rights the door does not carry: append, and rewrite.
	if err := db.Run(ctx, retain, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("INSERT INTO audit_events (tenant_id, occurred_at, name, event_id, payload) VALUES (?, now(), 'x', ?, '{}')",
			acme.ID, uuid.New()).Error
	}); err == nil {
		t.Error("the expiry role could append to the trail, which is the privilege that disqualifies it")
	}
	if err := db.Run(ctx, retain, func(_ context.Context, tx db.Tx[db.Tenant]) error {
		return tx.DB().Exec("UPDATE audit_events SET name = 'x'").Error
	}); err == nil {
		t.Error("the expiry role could rewrite the trail")
	}
}

// TestRecordingTwiceWritesOneRowEvenWithTheTriggersInPlace is the case the triggers
// could have broken: ON CONFLICT DO NOTHING is a statement, and a statement trigger
// or a row trigger reading the row it conflicts with would turn a redelivery into a
// refused write. Recording is idempotent after 00041 exactly as it was before it.
func TestRecordingTwiceWritesOneRowEvenWithTheTriggersInPlace(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	ctx := tenancy.WithTenant(t.Context(), acme)
	svc := internal.NewService()
	ev := events.Event{ID: uuid.New(), Name: "task.task.created", At: db.Now(), Payload: []byte(`{}`)}
	for i := 0; i < 2; i++ {
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return svc.Record(ctx, tx, ev)
		}); err != nil {
			t.Fatalf("record the event twice: %v", err)
		}
	}
	var rows int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Errorf("a redelivery wrote %d trail rows, want one", rows)
	}
}

// seed writes one event through the real service, at the instant given.
func seed(t *testing.T, ctx context.Context, conn *db.Conn, svc *internal.Service, id uuid.UUID, at time.Time) {
	t.Helper()
	err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Record(ctx, tx, events.Event{ID: id, Name: "task.task.created", At: at, Payload: []byte(`{}`)})
	})
	if err != nil {
		t.Fatalf("seed the trail: %v", err)
	}
}
