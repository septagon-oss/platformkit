package crud_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/septagon-oss/platformkit/kit/crud"
	"github.com/septagon-oss/platformkit/kit/db"
	"github.com/septagon-oss/platformkit/kit/tenancy"
)

// done is a decision: a task is done at an instant, and a task already done is
// left as it is and announced to nobody.
func done(at time.Time) func(Task) (crud.Outcome[Task], error) {
	return func(t Task) (crud.Outcome[Task], error) {
		if t.Done {
			return crud.Outcome[Task]{Next: t}, nil
		}
		t.Done, t.Status = true, "done"
		return crud.Outcome[Task]{Next: t, Event: "crud.task.done", Payload: map[string]any{"at": at}}, nil
	}
}

// TestApplyWritesWhatADecisionConcludedAtTheTransactionsInstant: the row and
// the event are written once, a repeat is silent, and every timestamp inside
// the transaction is the one instant Tx.At reports.
func TestApplyWritesWhatADecisionConcludedAtTheTransactionsInstant(t *testing.T) {
	conn := setup(t)
	err := db.Run(tenancy.WithTenant(t.Context(), acme), conn, func(ctx context.Context, tx db.Tx[db.Tenant]) error {
		at := tx.At()
		if at.IsZero() || at.Location() != time.UTC || !at.Equal(at.Truncate(time.Microsecond)) {
			t.Errorf("At() = %v; want the instant the transaction opened, in UTC, to the microsecond", at)
		}
		task := &Task{Title: "ship"}
		if err := crud.Create(ctx, tx, task); err != nil {
			return err
		}
		if !task.CreatedAt.Equal(at) || !task.UpdatedAt.Equal(at) {
			t.Errorf("created %v, updated %v; want both stamped with At() %v", task.CreatedAt, task.UpdatedAt, at)
		}
		var names []string
		for range 2 {
			got, err := crud.Apply(ctx, tx, task.ID, done(at), "done", "status", "updated_at")
			if err != nil || !got.Done || got.Status != "done" {
				return errors.Join(err, errors.New("the task is not done"))
			}
			if err := tx.DB().Raw(`SELECT name FROM platformkit_outbox WHERE tenant_id = ?`, acme.ID).Scan(&names).Error; err != nil {
				return err
			}
		}
		if len(names) != 1 || names[0] != "crud.task.done" {
			t.Errorf("the outbox holds %v; want the one event the first Apply published and silence from the second", names)
		}
		return db.Run(ctx, conn, func(_ context.Context, inner db.Tx[db.Tenant]) error {
			if !inner.At().Equal(at) {
				t.Errorf("a nested Run reports %v, want the enclosing transaction's %v", inner.At(), at)
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
}
