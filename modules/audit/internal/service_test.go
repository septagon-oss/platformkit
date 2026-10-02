package internal_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
	"github.com/septagon-oss/platformkit/modules/audit"
	"github.com/septagon-oss/platformkit/modules/audit/contracts"
	"github.com/septagon-oss/platformkit/modules/audit/contracts/audittest"
	"github.com/septagon-oss/platformkit/modules/audit/internal"
)

var (
	acme        = tenancy.Tenant{ID: uuid.New(), Slug: "acme", Name: "Acme"}
	globex      = tenancy.Tenant{ID: uuid.New(), Slug: "globex", Name: "Globex"}
	errRollback = errors.New("rolled back on purpose")
)

// TestServiceConforms runs the same suite the fake runs, against the real
// service, a real Postgres and a real tenant transaction.
func TestServiceConforms(t *testing.T) {
	audittest.RunService(t, func(t *testing.T, run func(audittest.Fixture)) {
		_, conn := dbtest.Schema(t, audit.Migrations)
		svc := internal.NewService()
		err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			run(audittest.Fixture{
				Ctx: ctx, Tx: tx, Service: svc,
				Published: func() []string { return outbox(t, tx) },
			})
			return errRollback
		})
		if !errors.Is(err, errRollback) {
			t.Fatalf("the case's transaction: %v", err)
		}
	})
}

// outbox is what has been published in this transaction, in order. For this
// module it is always empty, which is the claim the suite's silent() makes.
func outbox(t *testing.T, tx db.Tx[db.Tenant]) []string {
	t.Helper()
	var names []string
	err := tx.DB().Table("platformkit_outbox").Order("created_at, id").Pluck("name", &names).Error
	if err != nil {
		t.Fatalf("read the outbox: %v", err)
	}
	return names
}

// TestTheTrailIsTenantOwned: the trail is tenant-owned like every other
// table, so a row recorded in one customer's transaction is not merely filtered
// out of another's — it is invisible to it, by the policy in
// migrations/000010 and not by anything this module wrote.
func TestTheTrailIsTenantOwned(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()

	ev := events.Event{ID: uuid.New(), Name: "task.task.created", At: db.Now(),
		Payload: []byte(`{"title":"chiller-2"}`)}
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		return svc.Record(ctx, tx, ev)
	})
	if err != nil {
		t.Fatalf("record in acme: %v", err)
	}

	var acmeRow *contracts.Event
	err = db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		rows, total, err := svc.List(ctx, tx, contracts.Query{})
		if err != nil || total != 1 {
			t.Errorf("acme sees %d rows (%v), want its own", total, err)
			return err
		}
		acmeRow = rows[0]
		return nil
	})
	if err != nil {
		t.Fatalf("list in acme: %v", err)
	}

	err = db.Run(tenancy.WithTenant(t.Context(), globex), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		if _, total, err := svc.List(ctx, tx, contracts.Query{}); err != nil || total != 0 {
			t.Errorf("globex sees %d rows of acme's trail (%v)", total, err)
		}
		if _, err := svc.Get(ctx, tx, acmeRow.ID); !errors.Is(err, crud.ErrNotFound) {
			t.Errorf("globex read acme's row by id: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("list in globex: %v", err)
	}
}

// TestRetentionForgetsOnlyWhatIsOldEnough, a batch at a time. The batch is a
// thousand and the fixture is smaller, so what this proves is the boundary and
// the loop's exit, not the batching itself.
//
// The ages are past and inside the floor the trigger in migrations/00041 refuses to
// cross, and the job runs on the expiry connection rather than the application's,
// because the role that appends may not remove. What it removed is read back from
// audit_retention_marks: a sweep that forgot silently would pass a count of what is
// left and fail only this second assertion.
func TestRetentionForgetsOnlyWhatIsOldEnough(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()

	old := db.Now().AddDate(0, 0, -400)
	recent := db.Now().AddDate(0, 0, -2)
	recentID := uuid.New()
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, ev := range []events.Event{
			{ID: uuid.New(), Name: "task.task.created", At: old, Payload: []byte(`{}`)},
			{ID: recentID, Name: "task.task.created", At: recent, Payload: []byte(`{}`)},
		} {
			if err := svc.Record(ctx, tx, ev); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seed the trail: %v", err)
	}

	_, retainURL := dbtest.Role(t, admin,
		"SELECT, DELETE ON TABLE audit_events",
		"SELECT, INSERT ON TABLE audit_retention_marks")
	job := internal.Retention(lister{acme}, 365, retainURL)
	if err := job.Run(t.Context(), conn); err != nil {
		t.Fatalf("the retention job: %v", err)
	}

	var kept int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events`).Scan(&kept); err != nil {
		t.Fatalf("count what is left: %v", err)
	}
	if kept != 1 {
		t.Fatalf("the trail kept %d rows, want the one inside the retention period", kept)
	}
	var keptID uuid.UUID
	var keptAt time.Time
	if err := admin.QueryRowContext(t.Context(), `SELECT event_id, occurred_at FROM audit_events`).Scan(&keptID, &keptAt); err != nil {
		t.Fatalf("read the retained event: %v", err)
	}
	if keptID != recentID || !keptAt.Equal(recent) {
		t.Errorf("retained %s at %s, want the recent event %s at %s", keptID, keptAt, recentID, recent)
	}

	// The expiry wrote its own record, in the transaction of the delete it describes.
	var marks int
	var cutoff time.Time
	var removed int64
	var mine bool
	if err := admin.QueryRowContext(t.Context(),
		`SELECT count(*), max(cutoff), sum(removed), bool_and(tenant_id = $1) FROM audit_retention_marks`, acme.ID).
		Scan(&marks, &cutoff, &removed, &mine); err != nil {
		t.Fatalf("read the retention marks: %v", err)
	}
	if marks != 1 || removed != 1 || !mine {
		t.Fatalf("the sweep left %d marks naming %d rows of this tenant: %v, want one mark naming one row of %s",
			marks, removed, mine, acme.ID)
	}
	if !cutoff.Before(recent) {
		t.Errorf("the mark's cutoff %s is not before the retained row at %s", cutoff, recent)
	}

	// A second pass has nothing to remove, and a record of a nothing is not a record.
	if err := job.Run(t.Context(), conn); err != nil {
		t.Fatalf("the retention job, second pass: %v", err)
	}
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_retention_marks`).Scan(&marks); err != nil {
		t.Fatal(err)
	}
	if marks != 1 {
		t.Errorf("an empty sweep wrote %d marks, want the one that already described the removal", marks)
	}
}

// TestRetentionRefusesWithoutItsOwnRole is the refusal a deployment sees first: the
// application role cannot expire the trail, so a job with no retain_url has no door
// and says so rather than deleting nothing and reporting success.
func TestRetentionRefusesWithoutItsOwnRole(t *testing.T) {
	_, conn := dbtest.Schema(t, audit.Migrations)
	err := internal.Retention(lister{acme}, 365, "").Run(t.Context(), conn)
	if err == nil {
		t.Fatal("the retention job ran with no expiry role and reported success")
	}
}

// TestRetentionLeavesAnotherTrailsOwnerAlone is the pillar-1 case, and the one a
// misconfigured retain_url would pass: a retain role's connection is not a
// superuser's, so the first tenant's pass deletes the first tenant's rows. A
// BYPASSRLS DSN could not even be opened (kit/db refuses it); this case is what
// proves the policy, not the role attribute, is doing the work.
func TestRetentionLeavesAnotherTenantTrailAlone(t *testing.T) {
	admin, conn := dbtest.Schema(t, audit.Migrations)
	svc := internal.NewService()
	globex := tenancy.Tenant{ID: uuid.New(), Slug: "globex"}
	for _, tc := range []tenancy.Tenant{acme, globex} {
		ctx := tenancy.WithTenant(t.Context(), tc)
		if err := db.Run(ctx, conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
			return svc.Record(ctx, tx, events.Event{ID: uuid.New(), Name: "task.task.created", At: db.Now().AddDate(0, 0, -400), Payload: []byte(`{}`)})
		}); err != nil {
			t.Fatalf("seed %s: %v", tc.Slug, err)
		}
	}
	_, retainURL := dbtest.Role(t, admin,
		"SELECT, DELETE ON TABLE audit_events",
		"SELECT, INSERT ON TABLE audit_retention_marks")
	if err := internal.Retention(lister{acme}, 365, retainURL).Run(t.Context(), conn); err != nil {
		t.Fatalf("the retention job: %v", err)
	}
	for tenant, want := range map[uuid.UUID]int{acme.ID: 0, globex.ID: 1} {
		var kept int
		if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events WHERE tenant_id = $1`, tenant).Scan(&kept); err != nil {
			t.Fatal(err)
		}
		if kept != want {
			t.Errorf("trimming %s left %d of its rows, want %d", tenant, kept, want)
		}
	}
}

// lister is the tenants the sweep walks.
type lister []tenancy.Tenant

func (l lister) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) { return l, nil }
