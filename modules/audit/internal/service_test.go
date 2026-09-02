package internal_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/db/dbtest"
	"github.com/septagon-oss/platformkit/kit/events"
	"github.com/septagon-oss/platformkit/kit/tenancy"
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
		_, conn := dbtest.Schema(t)
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
	_, conn := dbtest.Schema(t)
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
func TestRetentionForgetsOnlyWhatIsOldEnough(t *testing.T) {
	admin, conn := dbtest.Schema(t)
	svc := internal.NewService()

	old := db.Now().AddDate(0, 0, -40)
	recent := db.Now().AddDate(0, 0, -2)
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		for _, ev := range []events.Event{
			{ID: uuid.New(), Name: "task.task.created", At: old, Payload: []byte(`{}`)},
			{ID: uuid.New(), Name: "task.task.created", At: recent, Payload: []byte(`{}`)},
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

	job := internal.Retention(lister{acme}, 30)
	if err := job.Run(t.Context(), conn); err != nil {
		t.Fatalf("the retention job: %v", err)
	}

	var kept int
	if err := admin.QueryRowContext(t.Context(), `SELECT count(*) FROM audit_events`).Scan(&kept); err != nil {
		t.Fatalf("count what is left: %v", err)
	}
	if kept != 1 {
		t.Errorf("the trail kept %d rows, want the one inside the retention period", kept)
	}
}

// lister is the tenants the sweep walks.
type lister []tenancy.Tenant

func (l lister) List(context.Context, db.Tx[db.System]) ([]tenancy.Tenant, error) { return l, nil }
